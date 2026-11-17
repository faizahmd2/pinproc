package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/faizahmd2/pinproc/internal/config"
	"github.com/faizahmd2/pinproc/internal/render"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func newConfigCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "see and change settings (" + config.ConfigPath + ")",
		Long: `pinproc works with its built-in defaults and no configuration at all. Settings
only add delivery (email, callback) and change presentation or thresholds.`,
		Example: `  pinproc config list                         # what is in effect, in plain words
  sudo pinproc config email                   # set up email alerts (guided)
  sudo pinproc config callback --url https://hooks.example.com/pinproc
  sudo pinproc config set report.timezone IST
  sudo pinproc config set monitor.mem.cap_util 92
  pinproc config show                         # the full YAML`,
	}
	cmd.AddCommand(newConfigListCmd(), newConfigShowCmd(), newConfigSetCmd(), newConfigValidateCmd(), newConfigEmailCmd(),
		newConfigCallbackCmd(), newConfigTimezoneCmd(), newConfigCooldownCmd(), newConfigTagCmd())
	return cmd
}

// ensureManagedConfigReadable refuses to fall back to built-in defaults when the
// managed config exists but the current user cannot read it — printing defaults as
// if they were the live config would be a lie. Only enforced without --config.
func ensureManagedConfigReadable() error {
	if cfgPath != "" {
		return nil
	}
	if _, err := os.Stat(config.ConfigPath); err != nil {
		if os.IsPermission(err) {
			return managedUnreadableErr()
		}
	} else {
		return nil
	}
	if _, derr := os.Stat(filepath.Dir(config.ConfigPath)); os.IsPermission(derr) {
		return managedUnreadableErr()
	}
	return nil
}

func managedUnreadableErr() error {
	return fmt.Errorf("cannot read managed configuration at %s (permission denied) — re-run with sudo", config.ConfigPath)
}

func newConfigShowCmd() *cobra.Command {
	return &cobra.Command{Use: "show", Short: "print the effective configuration as YAML (secrets masked)", RunE: func(cmd *cobra.Command, args []string) error {
		if err := ensureManagedConfigReadable(); err != nil {
			return err
		}
		cfg, err := config.Load(cfgPath)
		if err != nil {
			return err
		}
		if cfg.Email.Password != "" {
			cfg.Email.Password = "******** (hidden)"
		}
		data, err := yaml.Marshal(cfg)
		if err != nil {
			return err
		}
		fmt.Fprint(cmd.OutOrStdout(), string(data))
		return nil
	}}
}

func newConfigValidateCmd() *cobra.Command {
	return &cobra.Command{Use: "validate", Short: "check the configuration file", RunE: func(cmd *cobra.Command, args []string) error {
		if err := ensureManagedConfigReadable(); err != nil {
			return err
		}
		if _, err := config.Load(cfgPath); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "configuration is valid")
		return nil
	}}
}

func newConfigListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "print the settings in effect, grouped and in plain words (secrets masked)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := ensureManagedConfigReadable(); err != nil {
				return err
			}
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return err
			}
			printConfigList(cmd.OutOrStdout(), cfg)
			return nil
		},
	}
}

func onOff(b bool) string {
	if b {
		return "on "
	}
	return "off"
}

func printConfigList(w io.Writer, cfg *config.Config) {
	src := config.DiscoverPath()
	if cfgPath != "" {
		src = cfgPath
	}
	if _, err := os.Stat(src); src == "" || err != nil {
		src = "none — built-in defaults"
	}
	fmt.Fprintf(w, "config file   %s\n\n", src)

	e := cfg.Email
	fmt.Fprintln(w, "ALERT DELIVERY  (in order: email, then callback)")
	if e.Host == "" {
		fmt.Fprintln(w, "  email     off   not set up        → sudo pinproc config email")
	} else {
		auth := "no login"
		if e.Username != "" {
			auth = "as " + e.Username
			switch {
			case e.PasswordFile != "":
				auth += " · password from " + e.PasswordFile
			case e.Password != "":
				auth += " · password set"
			default:
				auth += " · no password"
			}
		}
		fmt.Fprintf(w, "  email     %s   %s:%d %s · %s\n", onOff(e.Enabled), e.Host, e.Port, e.Security, auth)
		fmt.Fprintf(w, "                  from %s → %s\n", e.From, strings.Join(e.To, ", "))
		attach := "full HTML page attached"
		if !e.AttachReport {
			attach = "no attachment"
		}
		fmt.Fprintf(w, "                  subject %s · %s\n", subjectOrDefault(e.Subject), attach)
	}
	c := cfg.Callback
	if c.URL == "" {
		fmt.Fprintln(w, "  callback  off   not set up        → sudo pinproc config callback --url <https://...>")
	} else {
		fmt.Fprintf(w, "  callback  %s   %s %s · body %s · timeout %s\n", onOff(c.Enabled), c.Method, c.URL, c.Format, c.Timeout)
	}
	fmt.Fprintf(w, "  cooldown        %s between automatic alerts for the same resource (alerts.cooldown)\n\n", cfg.Alerts.Cooldown)

	m := cfg.Monitor
	fmt.Fprintln(w, "WATCHER  (arm → watch → capture; 0 disables a bound)")
	fmt.Fprintf(w, "  cadence         every %s while calm, every %s while watching, up to %s\n", m.CalmCadence, m.ArmedCadence, m.WatchWindow)
	fmt.Fprintf(w, "  cpu             arm %g%% busy / psi %g · capture %g%% / psi %g · or sustained %s\n", m.CPU.ArmUtil, m.CPU.ArmPSI, m.CPU.CapUtil, m.CPU.CapPSI, m.CPU.Sustain)
	fmt.Fprintf(w, "  memory          arm %g%% used / psi %g · capture %g%% / psi %g · or sustained %s\n", m.Mem.ArmUtil, m.Mem.ArmPSI, m.Mem.CapUtil, m.Mem.CapPSI, m.Mem.Sustain)
	fmt.Fprintf(w, "  disk i/o        arm %g%% busy / psi %g · capture %g%% / psi %g · or sustained %s\n", m.IO.ArmUtil, m.IO.ArmPSI, m.IO.CapUtil, m.IO.CapPSI, m.IO.Sustain)
	fmt.Fprintf(w, "  network         retransmits %g→%g/s · conntrack %g→%g%% · time-wait %d→%d\n\n", m.Net.RetransArm, m.Net.RetransCap, m.Net.ConntrackArm, m.Net.ConntrackCap, m.Net.TimeWaitArm, m.Net.TimeWaitCap)

	_, zone, _ := render.ParseZone(cfg.Report.Timezone)
	fmt.Fprintln(w, "REPORTS")
	fmt.Fprintf(w, "  timezone        %s (report.timezone)\n", zone)
	tag := cfg.Report.Tag
	if tag == "" {
		tag = "—"
	}
	fmt.Fprintf(w, "  tag             %s (report.tag)\n", tag)
	fmt.Fprintf(w, "  files           %s\n\n", config.DataDirectory)
	fmt.Fprintln(w, "Change one value: sudo pinproc config set <key> <value>   ·   full YAML: pinproc config show")
}

// newConfigSetCmd changes one key by its YAML path, validates the result and
// restarts the service — any setting, without hand-editing the file.
func newConfigSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "change one setting by its YAML path, e.g. report.timezone IST",
		Example: `  sudo pinproc config set report.timezone IST
  sudo pinproc config set alerts.cooldown 1h
  sudo pinproc config set email.to ops@example.com,dev@example.com
  sudo pinproc config set monitor.cpu.cap_util 95
  sudo pinproc config set callback.enabled false`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireRoot(); err != nil {
				return err
			}
			key, val := strings.TrimSpace(args[0]), args[1]
			if key == "email.password" {
				return errors.New("set the password with: sudo pinproc config email (asked without echo) or --password-stdin")
			}
			cfg, err := config.Load("")
			if err != nil {
				return err
			}
			next, err := setKey(cfg, key, val)
			if err != nil {
				return err
			}
			return saveAndRestart(cmd, next, key+" = "+val+" ·")
		},
	}
}

// optionalKeys are settings left out of the YAML while empty.
var optionalKeys = map[string]bool{"email.subject": true, "email.password_file": true}

// setKey round-trips the config through a YAML tree, replaces one node and
// decodes it back, so types (durations, numbers, lists) are checked by the same
// decoder that reads the file.
func setKey(cfg *config.Config, key, val string) (*config.Config, error) {
	b, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	var root yaml.Node
	if err := yaml.Unmarshal(b, &root); err != nil {
		return nil, err
	}
	node := root.Content[0]
	path := strings.Split(key, ".")
	for i, part := range path {
		found := false
		for j := 0; j+1 < len(node.Content); j += 2 {
			if node.Content[j].Value == part {
				found = true
				if i == len(path)-1 {
					target := node.Content[j+1]
					switch target.Kind {
					case yaml.SequenceNode:
						target.Content = nil
						for _, item := range splitList(val) {
							target.Content = append(target.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: item})
						}
					case yaml.MappingNode:
						return nil, fmt.Errorf("%s is a section; set one of its keys, e.g. %s.<key>", key, key)
					default:
						target.Kind, target.Tag, target.Value, target.Style = yaml.ScalarNode, "", val, 0
					}
				} else {
					node = node.Content[j+1]
				}
				break
			}
		}
		if !found && i == len(path)-1 && optionalKeys[key] && node.Kind == yaml.MappingNode {
			// optional keys are omitted from the YAML while empty
			node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: part}, &yaml.Node{Kind: yaml.ScalarNode, Value: val})
			found = true
		}
		if !found {
			return nil, fmt.Errorf("unknown setting %q (see: pinproc config show)", key)
		}
	}
	out, err := yaml.Marshal(&root)
	if err != nil {
		return nil, err
	}
	var next config.Config
	if err := yaml.Unmarshal(out, &next); err != nil {
		return nil, fmt.Errorf("%s: %v", key, err)
	}
	if err := config.Validate(&next); err != nil {
		return nil, fmt.Errorf("%s: %v", key, err)
	}
	return &next, nil
}
