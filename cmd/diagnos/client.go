package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/faizahmd2/pinproc/internal/config"
	"github.com/faizahmd2/pinproc/internal/contract"
	"github.com/faizahmd2/pinproc/internal/deliver"
	"github.com/faizahmd2/pinproc/internal/doc"
	"github.com/faizahmd2/pinproc/internal/engine"
	"github.com/faizahmd2/pinproc/internal/inventory"
	"github.com/faizahmd2/pinproc/internal/report"
	"github.com/spf13/cobra"
)

// newReportCmd builds the detailed machine report: the full inventory (where every
// byte of RAM/disk and every core goes) plus the engine's findings. It writes
// report.json + report.html, prints their paths, and delivers the page to the
// configured channels.
func newReportCmd() *cobra.Command {
	var last, legacyCallback, noSend, toEmail bool
	var hint, dimension, out string
	var to []string
	cmd := &cobra.Command{
		Use:   "report",
		Short: "build the detailed machine report (HTML + JSON), save it and send it to your callback",
		Example: `  sudo pinproc report                 # write report.html/json, deliver to the callback
  sudo pinproc report --to email      # deliver by email instead
  pinproc report --no-send -o ./out   # just write the files
  pinproc report --last               # print the last captured alert as text`,
		RunE: func(cmd *cobra.Command, args []string) error {
			w := cmd.OutOrStdout()
			if last {
				return printLast(w)
			}
			cfg, err := loadConfigForCLI()
			if err != nil {
				return err
			}
			dir, err := outputDir(out)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.ErrOrStderr(), "collecting (≈2–5s: two samples plus a bounded directory scan)…")
			ctx := context.Background()
			inv, snap, err := collectBoth(ctx, cfg, engine.Request{
				ID: fmt.Sprintf("report-%d", time.Now().UnixNano()), Host: localHostName(), Trigger: "manual",
				Hint: hint, Dimension: contract.Dimension(dimension),
			}, inventory.ReportOptions(), nil)
			if err != nil {
				return err
			}
			d := buildDocument(cfg, doc.KindReport, inv, snap)
			htmlPath, jsonPath, err := writeOutputs(cfg, dir, "report", d)
			if err != nil {
				return fmt.Errorf("write report: %w", err)
			}
			fmt.Fprintf(w, "%s\n\n  html: %s\n  json: %s\n", d.Headline, htmlPath, jsonPath)
			if noSend {
				return nil
			}
			if toEmail {
				to = append(to, "email")
			}
			if legacyCallback {
				to = append(to, "callback")
			}
			return send(cmd, cfg, d, to, true)
		},
	}
	cmd.Flags().BoolVar(&last, "last", false, "print the last captured alert (plain text) and exit")
	cmd.Flags().BoolVar(&noSend, "no-send", false, "only write the files; do not deliver")
	cmd.Flags().StringSliceVar(&to, "to", nil, "deliver to these channels only: callback, email (default: every enabled channel)")
	cmd.Flags().BoolVar(&toEmail, "email", false, "shorthand for --to email")
	cmd.Flags().BoolVar(&legacyCallback, "callback", false, "shorthand for --to callback")
	cmd.Flags().StringVarP(&out, "output", "o", "", "directory to write report.html/report.json (default "+config.DataDirectory+" or ~/.local/state/pinproc)")
	cmd.Flags().StringVar(&hint, "hint", "", "operator context, e.g. \"checkout latency\"")
	cmd.Flags().StringVar(&dimension, "dimension", "", "focus the findings: cpu|memory|io|network|filesystem|limits")
	_ = cmd.Flags().MarkHidden("callback")
	return cmd
}

// newAlertCmd sends a pressure alert on demand. The service sends these by itself
// when it captures an incident; this command is for testing a channel or forcing
// an alert now. It ignores the cooldown.
func newAlertCmd() *cobra.Command {
	var last bool
	var out string
	cmd := &cobra.Command{
		Use:       "alert [callback|email]",
		Short:     "send a pressure alert now (the service does this automatically on incidents)",
		ValidArgs: []string{"callback", "email"},
		Args:      cobra.MatchAll(cobra.MaximumNArgs(1), cobra.OnlyValidArgs),
		Example: `  sudo pinproc alert callback   # check the machine now and POST the alert page
  sudo pinproc alert email      # same, by email
  sudo pinproc alert --last     # re-send the last captured incident to every channel`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfigForCLI()
			if err != nil {
				return err
			}
			var d *doc.Document
			if last {
				d, err = doc.Load(filepath.Join(config.DataDirectory, "alert.json"))
				if err != nil {
					if os.IsNotExist(err) {
						return fmt.Errorf("no captured alert yet at %s", config.DataDirectory)
					}
					return err
				}
			} else {
				dir, err := outputDir(out)
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.ErrOrStderr(), "checking the machine now…")
				inv, snap, err := collectBoth(context.Background(), cfg, engine.Request{
					ID: fmt.Sprintf("manual-%d", time.Now().UnixNano()), Host: localHostName(), Trigger: "manual",
				}, inventory.AlertOptions(), nil)
				if err != nil {
					return err
				}
				d = buildDocument(cfg, doc.KindAlert, inv, snap)
				d.TriggerDetail = "manual check (pinproc alert) — no threshold was crossed to send this"
				htmlPath, _, err := writeOutputs(cfg, dir, "alert-manual", d)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s\n  html: %s\n", d.Headline, htmlPath)
			}
			return send(cmd, cfg, d, args, true)
		},
	}
	cmd.Flags().BoolVar(&last, "last", false, "re-send the last captured incident instead of checking now")
	cmd.Flags().StringVarP(&out, "output", "o", "", "directory for the alert page")
	return cmd
}

// send delivers d to the named channels (or every enabled one) and prints results.
func send(cmd *cobra.Command, cfg *config.Config, d *doc.Document, names []string, force bool) error {
	var chans []deliver.Channel
	if len(names) == 0 {
		chans = deliver.Enabled(cfg)
		if len(chans) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "\nno delivery channel enabled — set one with: sudo pinproc config callback | sudo pinproc config email")
			return nil
		}
	}
	for _, n := range names {
		chans = append(chans, deliver.Channel(strings.ToLower(strings.TrimSpace(n))))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	failed := false
	fmt.Fprintln(cmd.OutOrStdout())
	for _, r := range (&deliver.Dispatcher{Cfg: cfg}).Deliver(ctx, d, chans, force) {
		fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", r)
		failed = failed || r.Err != nil
	}
	if failed {
		return fmt.Errorf("delivery failed")
	}
	return nil
}

func printLast(w io.Writer) error {
	if d, err := doc.Load(filepath.Join(config.DataDirectory, "alert.json")); err == nil && d.Text != "" {
		fmt.Fprint(w, d.Text)
		return nil
	}
	text, err := report.ReadText(config.DataDirectory)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no captured report yet at %s", config.DataDirectory)
		}
		if os.IsPermission(err) {
			return fmt.Errorf("cannot read %s (permission denied) — re-run with sudo", config.DataDirectory)
		}
		return err
	}
	fmt.Fprint(w, text)
	return nil
}

// loadConfigForCLI loads the config, turning a permission error on the managed
// file into a clear "use sudo" message.
func loadConfigForCLI() (*config.Config, error) {
	if err := ensureManagedConfigReadable(); err != nil {
		return nil, err
	}
	cfg, err := config.Load(cfgPath)
	if err != nil && os.IsPermission(err) {
		return nil, managedUnreadableErr()
	}
	return cfg, err
}
