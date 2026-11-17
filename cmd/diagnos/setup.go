package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	netmail "net/mail"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/faizahmd2/pinproc/internal/callback"
	"github.com/faizahmd2/pinproc/internal/config"
	"github.com/faizahmd2/pinproc/internal/contract"
	"github.com/faizahmd2/pinproc/internal/doc"
	"github.com/faizahmd2/pinproc/internal/inventory"
	"github.com/faizahmd2/pinproc/internal/mail"
	"github.com/faizahmd2/pinproc/internal/render"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// newSetupCmd keeps the old `pinproc setup callback` working; `pinproc config
// callback` is the documented form.
func newSetupCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "setup", Short: "configure pinproc (alias of `pinproc config`)", Hidden: true}
	cmd.AddCommand(newConfigCallbackCmd())
	return cmd
}

func requireRoot() error {
	if os.Geteuid() != 0 {
		return errors.New("this changes " + config.ConfigPath + " — run it with sudo")
	}
	return nil
}

// saveAndRestart validates, writes the managed config and restarts the service so
// it picks the change up.
func saveAndRestart(cmd *cobra.Command, cfg *config.Config, what string) error {
	if err := config.Save(config.ConfigPath, cfg); err != nil {
		return err
	}
	if err := restartService(); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s saved to %s · pinproc restarted.\n", what, config.ConfigPath)
	return nil
}

func newConfigCallbackCmd() *cobra.Command {
	var u, method, format string
	var disable, test bool
	cmd := &cobra.Command{
		Use:   "callback",
		Short: "set the URL that alert and report pages are sent to",
		Example: `  sudo pinproc config callback --url https://hooks.example.com/pinproc
  sudo pinproc config callback --format json      # html (default) | json | text
  sudo pinproc config callback --test
  sudo pinproc config callback --disable`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireRoot(); err != nil {
				return err
			}
			cfg, err := config.Load("")
			if err != nil {
				return err
			}
			changed := false
			switch {
			case disable:
				cfg.Callback.Enabled, cfg.Callback.URL, changed = false, "", true
			case u != "" || (!test && method == "" && format == ""):
				if u == "" {
					if u, err = prompt(cmd, "Callback URL", cfg.Callback.URL); err != nil {
						return err
					}
				}
				p, err := url.Parse(strings.TrimSpace(u))
				if err != nil || p.Host == "" || (p.Scheme != "http" && p.Scheme != "https") {
					return fmt.Errorf("callback URL must be an absolute http(s) URL")
				}
				cfg.Callback.URL, cfg.Callback.Enabled, changed = p.String(), true, true
			}
			if method != "" {
				cfg.Callback.Method, changed = strings.ToUpper(method), true
			}
			if format != "" {
				cfg.Callback.Format, changed = strings.ToLower(format), true
			}
			if changed {
				if err := saveAndRestart(cmd, cfg, "Callback"); err != nil {
					return err
				}
			}
			if test {
				body := fmt.Sprintf("pinproc callback test from %s at %s", localHostName(), time.Now().UTC().Format(time.RFC3339))
				if err := callback.SendBody(context.Background(), cfg.Callback.Method, cfg.Callback.URL, cfg.Callback.Timeout, "text/plain; charset=utf-8", []byte(body)); err != nil {
					return fmt.Errorf("test failed: %w", err)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Test request delivered to %s (%s). For a real page: sudo pinproc alert callback\n", cfg.Callback.URL, cfg.Callback.Method)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&u, "url", "", "callback URL (http or https)")
	cmd.Flags().StringVar(&method, "method", "", "POST (default) or GET")
	cmd.Flags().StringVar(&format, "format", "", "body format: html (default), json (the full document) or text")
	cmd.Flags().BoolVar(&disable, "disable", false, "disable the callback")
	cmd.Flags().BoolVar(&test, "test", false, "send a small test request to the configured URL")
	return cmd
}

// smtpPreset is a provider's documented submission endpoint.
type smtpPreset struct {
	host     string
	port     int
	security string
	user     string // fixed username, when the provider uses one
	note     string
}

var smtpPresets = map[string]smtpPreset{
	"gmail":     {"smtp.gmail.com", 587, "starttls", "", "needs an app password: myaccount.google.com/apppasswords"},
	"outlook":   {"smtp-mail.outlook.com", 587, "starttls", "", "needs an app password when 2-step verification is on"},
	"office365": {"smtp.office365.com", 587, "starttls", "", "SMTP AUTH must be enabled for the mailbox"},
	"ses":       {"email-smtp.us-east-1.amazonaws.com", 587, "starttls", "", "use SES SMTP credentials (not IAM keys); change the region in the host if needed"},
	"sendgrid":  {"smtp.sendgrid.net", 587, "starttls", "apikey", "password is your SendGrid API key"},
	"mailgun":   {"smtp.mailgun.org", 587, "starttls", "", "use the domain's SMTP credentials"},
	"postmark":  {"smtp.postmarkapp.com", 587, "starttls", "", "username and password are both the server API token"},
	"brevo":     {"smtp-relay.brevo.com", 587, "starttls", "", "use the SMTP key from the Brevo console"},
	"zoho":      {"smtp.zoho.com", 587, "starttls", "", ""},
	"local":     {"localhost", 25, "none", "", "a relay on this machine (postfix, msmtp); no password"},
}

func presetNames() string {
	return "gmail, outlook, office365, ses, sendgrid, mailgun, postmark, brevo, zoho, local, custom"
}

func presetFor(host string) string {
	for name, p := range smtpPresets {
		if p.host == host {
			return name
		}
	}
	if strings.HasPrefix(host, "email-smtp.") {
		return "ses"
	}
	return "custom"
}

func newConfigEmailCmd() *cobra.Command {
	var host, security, user, from, passFile, subject string
	var to []string
	var port int
	var passStdin, disable, test, noAttach bool
	cmd := &cobra.Command{
		Use:   "email",
		Short: "set up email alerts through your own SMTP account (asks for each field)",
		Long: `Set up email delivery. pinproc ships no mail service: it speaks SMTP directly
(Go's net/smtp) to the account you configure — Gmail/Workspace or Outlook with an
app password, Amazon SES, SendGrid, Mailgun, Postmark, Brevo, Zoho, or a local
relay. Alerts and reports are sent as HTML with a plain-text alternative.

Run it without flags to be asked for each field (Enter keeps the current value).
After saving it offers a dry-run test: connect, TLS, login, sender and recipients
are checked and nothing is sent. Run that test again any time with --test.

The password is never taken as a flag (it would land in shell history): it is
asked for without echo, read from --password-stdin, or read from --password-file
at send time. It is stored in ` + config.ConfigPath + ` (mode 0640, root:pinproc).`,
		Example: `  sudo pinproc config email                 # guided setup
  sudo pinproc config email --test          # dry-run the saved settings (sends nothing)
  sudo pinproc config email --host smtp.gmail.com --user me@example.com \
       --from "pinproc <me@example.com>" --to ops@example.com,dev@example.com
  echo "$SMTP_PASS" | sudo pinproc config email --password-stdin
  sudo pinproc config email --subject "[{tag}] {host}: {cause}"
  sudo pinproc config email --disable`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireRoot(); err != nil {
				return err
			}
			cfg, err := config.Load("")
			if err != nil {
				return err
			}
			e := &cfg.Email
			w := cmd.OutOrStdout()
			if disable {
				e.Enabled = false
				return saveAndRestart(cmd, cfg, "Email (disabled)")
			}
			flagsGiven := host != "" || user != "" || from != "" || len(to) > 0 || port != 0 || security != "" || passFile != "" || passStdin || noAttach || cmd.Flags().Changed("subject")
			if test && !flagsGiven {
				return verifyEmail(cmd, cfg)
			}
			interactive := !flagsGiven && term.IsTerminal(int(os.Stdin.Fd()))
			if interactive {
				if err := askEmail(cmd, e, cfg.Report.Tag); err != nil {
					return err
				}
			} else {
				if host != "" {
					e.Host = host
				}
				if port != 0 {
					e.Port = port
				}
				if security != "" {
					e.Security = security
				} else if port == 465 {
					e.Security = "tls"
				}
				if user != "" {
					e.Username = user
				}
				switch {
				case passFile != "":
					e.PasswordFile, e.Password = passFile, ""
				case passStdin:
					b, err := io.ReadAll(io.LimitReader(os.Stdin, 4096))
					if err != nil {
						return err
					}
					e.Password, e.PasswordFile = strings.TrimSpace(string(b)), ""
				}
				if from != "" {
					e.From = from
				}
				if len(to) > 0 {
					e.To = splitList(strings.Join(to, ","))
				}
				if noAttach {
					e.AttachReport = false
				}
				if cmd.Flags().Changed("subject") {
					e.Subject = strings.TrimSpace(subject)
				}
			}
			e.Enabled = true
			if e.From == "" && strings.Contains(e.Username, "@") {
				e.From = "pinproc <" + e.Username + ">"
			}
			if err := saveAndRestart(cmd, cfg, "Email configuration"); err != nil {
				return err
			}
			if test || (interactive && askYesNo(cmd, "\nTest SMTP connection and authentication now?", true)) {
				return verifyEmail(cmd, cfg)
			}
			fmt.Fprintln(w, "Dry-run it later with: sudo pinproc config email --test")
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&host, "host", "", "SMTP server host")
	f.IntVar(&port, "port", 0, "SMTP port (587 STARTTLS, 465 implicit TLS, 25 local relay)")
	f.StringVar(&security, "security", "", "starttls (default), tls, or none (local relay only)")
	f.StringVar(&user, "user", "", "SMTP username")
	f.BoolVar(&passStdin, "password-stdin", false, "read the SMTP password from stdin")
	f.StringVar(&passFile, "password-file", "", "read the SMTP password from this file at send time")
	f.StringVar(&from, "from", "", "sender, e.g. \"pinproc <alerts@example.com>\"")
	f.StringSliceVar(&to, "to", nil, "recipients, comma-separated (or repeat the flag)")
	f.StringVar(&subject, "subject", "", "alert subject template ({host} {ip} {tag} {severity} {dimension} {cause} {headline}); empty restores the default")
	f.BoolVar(&noAttach, "no-attach", false, "do not attach the full HTML page to emails")
	f.BoolVar(&disable, "disable", false, "turn email delivery off (settings are kept)")
	f.BoolVar(&test, "test", false, "dry-run the SMTP settings: connect, TLS, login, sender, recipients — sends nothing")
	return cmd
}

// askEmail walks through every field, validating as it goes.
func askEmail(cmd *cobra.Command, e *config.EmailConfig, tag string) error {
	w := cmd.OutOrStdout()
	fmt.Fprint(w, `pinproc · email alerts

Bring any SMTP account — pinproc connects to it directly, nothing goes through a
third party. Press Enter to keep the value shown in [brackets].

`)
	cur := presetFor(e.Host)
	if e.Host == "" {
		cur = "custom"
	}
	for {
		name, err := prompt(cmd, "Provider ("+presetNames()+")", cur)
		if err != nil {
			return err
		}
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "custom" || name == cur && e.Host != "" {
			break
		}
		p, ok := smtpPresets[name]
		if !ok {
			fmt.Fprintln(w, "  unknown provider — pick one from the list, or custom")
			continue
		}
		e.Host, e.Port, e.Security = p.host, p.port, p.security
		if p.user != "" {
			e.Username = p.user
		}
		if name == "local" {
			e.Username, e.Password, e.PasswordFile = "", "", ""
		}
		if p.note != "" {
			fmt.Fprintln(w, "  note: "+p.note)
		}
		break
	}
	var err error
	if e.Host, err = promptValid(cmd, "SMTP host", e.Host, func(v string) error {
		if v == "" || strings.ContainsAny(v, " /:") {
			return errors.New("a host name like smtp.example.com")
		}
		return nil
	}); err != nil {
		return err
	}
	if e.Port == 0 {
		e.Port = 587
	}
	v, err := promptValid(cmd, "Port (587 STARTTLS · 465 TLS · 25 local relay)", strconv.Itoa(e.Port), func(v string) error {
		if n, err := strconv.Atoi(v); err != nil || n < 1 || n > 65535 {
			return errors.New("a port number")
		}
		return nil
	})
	if err != nil {
		return err
	}
	e.Port, _ = strconv.Atoi(v)
	defSec := e.Security
	if e.Port == 465 {
		defSec = "tls"
	} else if defSec == "" || (defSec == "tls" && e.Port == 587) {
		defSec = "starttls"
	}
	if e.Security, err = promptValid(cmd, "Security (starttls · tls · none)", defSec, func(v string) error {
		switch strings.ToLower(v) {
		case "starttls", "tls", "none", "ssl":
			return nil
		}
		return errors.New("starttls, tls or none")
	}); err != nil {
		return err
	}
	e.Security = strings.ToLower(e.Security)
	if e.Security == "ssl" {
		e.Security = "tls"
	}
	prevUser := e.Username
	if e.Username, err = prompt(cmd, "Username (blank for none)", e.Username); err != nil {
		return err
	}
	if e.Username != "" {
		keep := ""
		if e.Password != "" || e.PasswordFile != "" {
			keep = ", Enter keeps the current one"
		}
		fmt.Fprintf(w, "Password (hidden%s): ", keep)
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(w)
		if err != nil {
			return err
		}
		if p := strings.TrimSpace(string(b)); p != "" {
			e.Password, e.PasswordFile = p, ""
		} else if keep == "" {
			fmt.Fprintln(w, "  no password set — the test below will show whether the server needs one")
		}
	} else {
		e.Password, e.PasswordFile = "", ""
	}
	defFrom := e.From
	if (defFrom == "" || e.Username != prevUser) && strings.Contains(e.Username, "@") {
		defFrom = "pinproc <" + e.Username + ">"
	}
	if e.From, err = promptValid(cmd, "From (e.g. pinproc <alerts@example.com>)", defFrom, func(v string) error {
		_, err := netmail.ParseAddress(v)
		return err
	}); err != nil {
		return err
	}
	v, err = promptValid(cmd, "Send to (comma-separated)", strings.Join(e.To, ", "), func(v string) error {
		list := splitList(v)
		if len(list) == 0 {
			return errors.New("at least one address")
		}
		for _, a := range list {
			if _, err := netmail.ParseAddress(a); err != nil {
				return fmt.Errorf("%q: %v", a, err)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	e.To = splitList(v)
	e.AttachReport = askYesNo(cmd, "Attach the full HTML page to each email?", e.AttachReport || e.Host == "")

	fmt.Fprintf(w, "\nAlert subject (optional)\n  current:      %s\n  placeholders: {host} {ip} {tag} {severity} {dimension} {cause} {headline}\n", subjectOrDefault(e.Subject))
	sub, err := prompt(cmd, "Custom subject (Enter keeps it, - restores the default)", "")
	if err != nil {
		return err
	}
	switch strings.TrimSpace(sub) {
	case "":
	case "-":
		e.Subject = ""
	default:
		e.Subject = strings.TrimSpace(sub)
	}
	fmt.Fprintf(w, "  alerts will read: %s\n\n", sampleSubject(e.Subject, tag))
	return nil
}

func subjectOrDefault(s string) string {
	if strings.TrimSpace(s) == "" {
		return render.DefaultAlertSubject + " (default)"
	}
	return s
}

func sampleSubject(tmpl, tag string) string {
	d := &doc.Document{Kind: doc.KindAlert, Severity: doc.SeverityCritical, Trigger: "auto:memory", Dimension: contract.DimensionMemory,
		Headline: "Memory pressure — java is the cause", Snapshot: &inventory.Snapshot{Host: inventory.Host{Hostname: localHostName(), PrimaryIP: "10.0.0.4"}},
		Incident: &contract.Incident{Cause: &contract.ServiceCause{Service: "java", Value: 6.1 * (1 << 30), Unit: "bytes"}}, Tag: tag}
	return render.Subject(d, tmpl)
}

// verifyEmail dry-runs the saved settings and prints each stage.
func verifyEmail(cmd *cobra.Command, cfg *config.Config) error {
	w := cmd.OutOrStdout()
	e := cfg.Email
	if strings.TrimSpace(e.Host) == "" || len(e.To) == 0 {
		return errors.New("email is not set up yet — run: sudo pinproc config email")
	}
	pass, err := e.EmailPassword()
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "\nTesting %s:%d (%s) — dry run, nothing will be sent\n", e.Host, e.Port, e.Security)
	steps, err := mail.Verify(context.Background(), mail.Server{Host: e.Host, Port: e.Port, Security: e.Security, Username: e.Username, Password: pass, Timeout: e.Timeout}, e.From, e.To)
	for _, s := range steps {
		fmt.Fprintf(w, "  ✓ %-10s %s\n", s.Name, s.Detail)
	}
	if err != nil {
		fmt.Fprintf(w, "  ✗ %v\n", err)
		if h := mail.Hint(err); h != "" {
			fmt.Fprintf(w, "    → %s\n", h)
		}
		fmt.Fprintln(w, "\nSettings are saved. Fix them with: sudo pinproc config email   (Enter keeps what is right)")
		return errors.New("SMTP test failed")
	}
	fmt.Fprintln(w, "\nSMTP settings work. Nothing was sent (dry run).")
	fmt.Fprintln(w, "Alerts arrive by email from now on. Send one now: sudo pinproc alert email")
	return nil
}

func promptValid(cmd *cobra.Command, label, def string, check func(string) error) (string, error) {
	for i := 0; ; i++ {
		v, err := prompt(cmd, label, def)
		if err != nil {
			return "", err
		}
		v = strings.TrimSpace(v)
		cerr := check(v)
		if cerr == nil {
			return v, nil
		}
		if i == 2 {
			return "", fmt.Errorf("%s: %v", strings.ToLower(label), cerr)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "  expected %v — try again\n", cerr)
	}
}

func askYesNo(cmd *cobra.Command, q string, def bool) bool {
	hint := "[Y/n]"
	if !def {
		hint = "[y/N]"
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s %s ", q, hint)
	v, err := readLine(cmd.InOrStdin())
	if err != nil {
		fmt.Fprintln(cmd.OutOrStdout())
		return def
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "":
		return def
	case "y", "yes":
		return true
	}
	return false
}

func newConfigTimezoneCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "timezone <UTC|IST|Area/City|+05:30>",
		Short:   "set the timezone used in reports, alerts and the terminal view (default UTC)",
		Example: "  sudo pinproc config timezone IST\n  sudo pinproc config timezone UTC\n  sudo pinproc config timezone Europe/Berlin",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireRoot(); err != nil {
				return err
			}
			_, label, err := render.ParseZone(args[0])
			if err != nil {
				return err
			}
			cfg, err := config.Load("")
			if err != nil {
				return err
			}
			cfg.Report.Timezone = args[0]
			return saveAndRestart(cmd, cfg, "Timezone "+label)
		},
	}
}

func newConfigCooldownCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "cooldown <duration>",
		Short:   "minimum gap between automatic alerts for the same resource (default 30m)",
		Example: "  sudo pinproc config cooldown 30m\n  sudo pinproc config cooldown 2h",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireRoot(); err != nil {
				return err
			}
			d, err := time.ParseDuration(args[0])
			if err != nil || d < time.Minute {
				return fmt.Errorf("cooldown must be a duration of at least 1m, e.g. 30m or 2h")
			}
			cfg, err := config.Load("")
			if err != nil {
				return err
			}
			cfg.Alerts.Cooldown = d
			return saveAndRestart(cmd, cfg, "Alert cooldown "+d.String())
		},
	}
}

func newConfigTagCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "tag <label>",
		Short:   "label shown in report headers, e.g. production or eu-west-api",
		Example: "  sudo pinproc config tag production\n  sudo pinproc config tag \"\"   # clear",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireRoot(); err != nil {
				return err
			}
			cfg, err := config.Load("")
			if err != nil {
				return err
			}
			cfg.Report.Tag = strings.TrimSpace(args[0])
			return saveAndRestart(cmd, cfg, "Report tag")
		},
	}
}

func prompt(cmd *cobra.Command, label, def string) (string, error) {
	if def != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "%s [%s]: ", label, def)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "%s: ", label)
	}
	v, err := readLine(cmd.InOrStdin())
	if err != nil && err != io.EOF {
		return "", err
	}
	if v == "" {
		return def, nil
	}
	return v, nil
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func readLine(r io.Reader) (string, error) {
	scanner := bufio.NewScanner(r)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", err
		}
		return "", io.EOF
	}
	return strings.TrimSpace(scanner.Text()), nil
}

func restartService() error {
	systemctl, err := exec.LookPath("systemctl")
	if err != nil {
		return nil
	}
	if exec.Command(systemctl, "is-enabled", "--quiet", "pinproc.service").Run() != nil {
		return nil // not installed as a service (dev build): nothing to restart
	}
	// Several settings changed in a row restart the service several times; clear
	// systemd's start-rate counter so the watcher is never left failed by it.
	_ = exec.Command(systemctl, "reset-failed", "pinproc.service").Run()
	out, err := exec.Command(systemctl, "restart", "pinproc.service").CombinedOutput()
	if err != nil {
		return fmt.Errorf("restart pinproc service: %s", strings.TrimSpace(string(out)))
	}
	return nil
}
