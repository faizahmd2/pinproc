package main

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/faizahmd2/pinproc/internal/config"
	"github.com/faizahmd2/pinproc/internal/deliver"
	"github.com/faizahmd2/pinproc/internal/render"
	druntime "github.com/faizahmd2/pinproc/internal/runtime"
	"github.com/faizahmd2/pinproc/internal/tui"
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

// main runs the live view (bare `pinproc`), the daemon (`pinproc service run`,
// started by systemd) or a CLI command.
func main() {
	logLevel.Set(slog.LevelWarn)
	logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel}))
	druntime.Local(logger)
	var upgrade, once bool
	var interval time.Duration
	root := &cobra.Command{
		Use:   "pinproc",
		Short: "pinproc — read-only Linux machine watcher: live view, incident alerts, detailed reports",
		Long: `pinproc watches a Linux host read-only. Run it bare for a live, top-like view;
the systemd service captures incidents by itself and sends alerts to your callback
and email; ` + "`pinproc report`" + ` builds the detailed machine report on demand.`,
		Example: `  pinproc                       # live view (q to quit)
  sudo pinproc report           # detailed report → report.html/json + callback
  sudo pinproc alert email      # send a pressure alert now
  sudo pinproc config email     # set up SMTP
  sudo pinproc --upgrade        # update to the latest release`,
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if upgrade {
				return runUpgrade(cmd.OutOrStdout())
			}
			opt := renderOptionsBestEffort()
			fd := int(os.Stdout.Fd())
			if once || !term.IsTerminal(fd) {
				width := 120
				if w, _, err := term.GetSize(fd); err == nil && w > 0 {
					width = w
				}
				return tui.Once(cmd.OutOrStdout(), opt, width)
			}
			return tui.Run(opt, interval)
		},
	}
	root.PersistentFlags().StringVar(&cfgPath, "config", "", "config path")
	root.Flags().BoolVar(&upgrade, "upgrade", false, "upgrade pinproc to the latest release (APT installs)")
	root.Flags().BoolVar(&once, "once", false, "print one snapshot and exit (like top -b -n 1)")
	root.Flags().DurationVarP(&interval, "interval", "d", 2*time.Second, "live view refresh interval")
	root.AddCommand(newServiceCmd(), newSetupCmd(), newConfigCommand(), newDoctorCmd(), newReportCmd(), newAlertCmd(), newRenderCmd())
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "pinproc:", err)
		os.Exit(2)
	}
}

// renderOptionsBestEffort reads the timezone from config when it is readable; the
// live view must work for unprivileged users, so it falls back to UTC silently.
func renderOptionsBestEffort() render.Options {
	if err := ensureManagedConfigReadable(); err == nil {
		if cfg, err := config.Load(cfgPath); err == nil {
			return deliver.Options(cfg)
		}
	}
	return render.Options{}
}
