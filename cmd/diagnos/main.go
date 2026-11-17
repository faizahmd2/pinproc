package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/faizahmd2/pinproc/internal/config"
	"github.com/faizahmd2/pinproc/internal/console"
	"github.com/faizahmd2/pinproc/internal/deliver"
	"github.com/faizahmd2/pinproc/internal/inventory"
	"github.com/faizahmd2/pinproc/internal/render"
	druntime "github.com/faizahmd2/pinproc/internal/runtime"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var cfgPath string
var logger *slog.Logger

// logLevel is WARN for interactive commands (no engine chatter on the terminal);
// the daemon raises it to INFO for the journal.
var logLevel = new(slog.LevelVar)
var version = "dev"
var commit = "none"
var date = "unknown"

// main runs the one-look terminal view (bare `pinproc`), the daemon (`pinproc service run`,
// started by systemd) or a CLI command.
func main() {
	logLevel.Set(slog.LevelWarn)
	logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel}))
	druntime.Local(logger)
	var upgrade, asJSON bool
	root := &cobra.Command{
		Use:   "pinproc",
		Short: "pinproc — read-only Linux incident watcher: pressure alerts, machine reports, one-look terminal view",
		Long: `pinproc watches a Linux host read-only. The systemd service samples the kernel's
pressure signals, captures an incident the moment a threshold is crossed and sends
the alert to your email and callback. ` + "`pinproc report`" + ` describes the whole machine on
demand. Run it bare for one structured look at the machine right now.`,
		Example: `  pinproc                       # one look at the machine now
  sudo pinproc config email     # get alerts by email (your own SMTP)
  sudo pinproc report           # full machine description → report.html/json + delivery
  sudo pinproc alert email      # send a pressure alert now
  pinproc config list           # current settings
  sudo pinproc --upgrade        # update to the latest release`,
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if upgrade {
				return runUpgrade(cmd.OutOrStdout())
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			snap, err := inventory.New(inventory.TerminalOptions()).Collect(ctx)
			if err != nil {
				return err
			}
			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(snap)
			}
			fd := int(os.Stdout.Fd())
			width := 0
			if w, _, err := term.GetSize(fd); err == nil {
				width = w
			}
			console.Render(cmd.OutOrStdout(), snap, renderOptionsBestEffort(), console.AutoStyle(term.IsTerminal(fd), width))
			return nil
		},
	}
	root.PersistentFlags().StringVar(&cfgPath, "config", "", "config path")
	root.Flags().BoolVar(&upgrade, "upgrade", false, "upgrade pinproc to the latest release (APT installs)")
	root.Flags().BoolVar(&asJSON, "json", false, "print the machine snapshot as JSON instead")
	root.AddCommand(newServiceCmd(), newSetupCmd(), newConfigCommand(), newDoctorCmd(), newReportCmd(), newAlertCmd(), newRenderCmd())
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "pinproc:", err)
		os.Exit(2)
	}
}

// renderOptionsBestEffort reads the timezone from config when it is readable; the
// terminal view must work for unprivileged users, so it falls back to UTC silently.
func renderOptionsBestEffort() render.Options {
	if err := ensureManagedConfigReadable(); err == nil {
		if cfg, err := config.Load(cfgPath); err == nil {
			return deliver.Options(cfg)
		}
	}
	return render.Options{}
}
