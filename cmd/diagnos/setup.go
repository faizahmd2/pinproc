package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/faizahmd2/pinproc/internal/callback"
	"github.com/faizahmd2/pinproc/internal/config"
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

func newConfigEmailCmd() *cobra.Command {
	var host, security, user, from, passFile string
	var to []string
	var port int
	var passStdin, disable, test, noAttach bool
	cmd := &cobra.Command{
		Use:   "email",
		Short: "configure SMTP email for alerts and reports (your own mail server/provider)",
		Long: `Configure email delivery. pinproc ships no mail provider: it speaks plain SMTP
(Go's net/smtp) to the server you configure — Gmail/Workspace (app password),
Amazon SES, Postmark, Mailgun, SendGrid, or a local relay.

The password is never accepted as a flag (it would land in shell history). It is
prompted for without echo, read from --password-stdin, or read from
--password-file at send time. It is stored in ` + config.ConfigPath + `
(mode 0640, root:pinproc).`,
		Example: `  sudo pinproc config email          # interactive
  sudo pinproc config email --host smtp.gmail.com --port 587 --user me@example.com \
       --from "pinproc <me@example.com>" --to ops@example.com,dev@example.com
  echo "$SMTP_PASS" | sudo pinproc config email --password-stdin
  sudo pinproc config email --test
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
			if disable {
				e.Enabled = false
				return saveAndRestart(cmd, cfg, "Email (disabled)")
			}
			onlyTest := test && host == "" && user == "" && from == "" && len(to) == 0 && port == 0 && security == "" && passFile == "" && !passStdin && !noAttach
			if !onlyTest {
				interactive := host == "" && term.IsTerminal(int(os.Stdin.Fd()))
				if host != "" {
					e.Host = host
				} else if interactive {
					if e.Host, err = prompt(cmd, "SMTP host", e.Host); err != nil {
						return err
					}
				}
				if port != 0 {
					e.Port = port
				} else if interactive {
					v, err := prompt(cmd, "SMTP port (587 STARTTLS, 465 TLS)", strconv.Itoa(e.Port))
					if err != nil {
						return err
					}
					if e.Port, err = strconv.Atoi(v); err != nil {
						return fmt.Errorf("port: %w", err)
					}
				}
				if security != "" {
					e.Security = security
				} else if e.Port == 465 {
					e.Security = "tls"
				}
				if user != "" {
					e.Username = user
				} else if interactive {
					if e.Username, err = prompt(cmd, "SMTP username (blank for none)", e.Username); err != nil {
						return err
					}
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
				case interactive && e.Username != "":
					fmt.Fprint(cmd.OutOrStdout(), "SMTP password (hidden, Enter keeps the current one): ")
					b, err := term.ReadPassword(int(os.Stdin.Fd()))
					fmt.Fprintln(cmd.OutOrStdout())
					if err != nil {
						return err
					}
					if p := strings.TrimSpace(string(b)); p != "" {
						e.Password, e.PasswordFile = p, ""
					}
				}
				if from != "" {
					e.From = from
				} else if interactive {
					def := e.From
					if def == "" && strings.Contains(e.Username, "@") {
						def = "pinproc <" + e.Username + ">"
					}
					if e.From, err = prompt(cmd, "From (e.g. pinproc <alerts@example.com>)", def); err != nil {
						return err
					}
				}
				if len(to) > 0 {
					e.To = to
				} else if interactive {
					v, err := prompt(cmd, "To (comma-separated)", strings.Join(e.To, ","))
					if err != nil {
						return err
					}
					e.To = splitList(v)
				}
				if noAttach {
					e.AttachReport = false
				}
				e.Enabled = true
				if err := saveAndRestart(cmd, cfg, "Email"); err != nil {
					return err
				}
			}
			if test {
				return testEmail(cmd, cfg)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Send a test with: sudo pinproc config email --test")
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&host, "host", "", "SMTP server host")
	f.IntVar(&port, "port", 0, "SMTP port (587 STARTTLS, 465 implicit TLS, 25 relay)")
	f.StringVar(&security, "security", "", "starttls (default), tls, or none (local relay only)")
	f.StringVar(&user, "user", "", "SMTP username")
	f.BoolVar(&passStdin, "password-stdin", false, "read the SMTP password from stdin")
	f.StringVar(&passFile, "password-file", "", "read the SMTP password from this file at send time")
	f.StringVar(&from, "from", "", "sender, e.g. \"pinproc <alerts@example.com>\"")
	f.StringSliceVar(&to, "to", nil, "recipients (comma-separated or repeated)")
	f.BoolVar(&noAttach, "no-attach", false, "do not attach the full HTML page to emails")
	f.BoolVar(&disable, "disable", false, "disable email delivery")
	f.BoolVar(&test, "test", false, "send a test email with the current settings")
	return cmd
}

func testEmail(cmd *cobra.Command, cfg *config.Config) error {
	e := cfg.Email
	pass, err := e.EmailPassword()
	if err != nil {
		return err
	}
	host := localHostName()
	msg := mail.Message{
		From: e.From, To: e.To, Subject: "[pinproc] test email from " + host,
		Text: "This is a test from pinproc on " + host + ". Email delivery works.\n\nSend a real alert with: sudo pinproc alert email\n",
		HTML: "<p style=\"font-family:sans-serif\">This is a test from <b>pinproc</b> on <code>" + host + "</code>. Email delivery works.</p><p style=\"font-family:sans-serif;color:#64748b\">Send a real alert with <code>sudo pinproc alert email</code>.</p>",
	}
	fmt.Fprintf(cmd.OutOrStdout(), "sending test via %s:%d (%s)…\n", e.Host, e.Port, e.Security)
	if err := mail.Send(context.Background(), mail.Server{Host: e.Host, Port: e.Port, Security: e.Security, Username: e.Username, Password: pass, Timeout: e.Timeout}, msg); err != nil {
		return fmt.Errorf("test email failed: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Test email sent to %s.\n", strings.Join(e.To, ", "))
	return nil
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
	out, err := exec.Command(systemctl, "restart", "pinproc.service").CombinedOutput()
	if err != nil {
		return fmt.Errorf("restart pinproc service: %s", strings.TrimSpace(string(out)))
	}
	return nil
}
