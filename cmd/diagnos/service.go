package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/faizahmd2/pinproc/internal/config"
	"github.com/faizahmd2/pinproc/internal/report"
	"github.com/faizahmd2/pinproc/internal/source"
	"github.com/spf13/cobra"
)

// newServiceCmd exposes the persistent daemon. Installation, account creation,
// enablement and removal are owned by the Debian package.
func newServiceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service",
		Short: "run the persistent pinproc watcher",
		RunE:  func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	run := &cobra.Command{
		Use:   "run",
		Short: "watch the host and capture incidents (no socket, no egress by default)",
		RunE:  func(cmd *cobra.Command, args []string) error { return runService() },
	}
	cmd.AddCommand(run)
	return cmd
}

// runService prepares storage, confirms the host is readable, then runs the
// read-only self-trigger until the process is asked to stop.
func runService() error {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	dir, err := config.ResolveOutputDirectory(config.DataDirectory)
	if err != nil {
		return err
	}
	if err := report.EnsureWritable(dir); err != nil {
		return serviceStartHint(fmt.Errorf("data directory unavailable: %w", err))
	}
	probe := source.NewLocalWithTimeout("/proc", "/sys", 8<<20, cfg.Source.ReadTimeout)
	defer probe.Close()
	if err := probe.StartupCheck(); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	logger.Info("pinproc started", "report_dir", dir)
	err = runMonitor(ctx, cfg, filepath.Clean(dir), &sync.Mutex{})
	if err == context.Canceled {
		return nil
	}
	return err
}

// serviceStartHint turns a bare permission error from a hand-run `pinproc` into
// guidance: the daemon is systemd-managed, and humans read reports with `report`.
func serviceStartHint(err error) error {
	if os.IsPermission(err) && os.Geteuid() != 0 {
		return fmt.Errorf("%w\n\npinproc runs as a managed system service. You usually do not start it by hand.\n  start:        sudo systemctl start pinproc\n  get a report: pinproc report\n  self-check:   sudo pinproc doctor", err)
	}
	return err
}
