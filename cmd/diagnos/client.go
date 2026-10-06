package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/faizahmd2/pinproc/internal/capability"
	"github.com/faizahmd2/pinproc/internal/config"
	"github.com/faizahmd2/pinproc/internal/contract"
	drules "github.com/faizahmd2/pinproc/internal/decision/rules"
	"github.com/faizahmd2/pinproc/internal/engine"
	"github.com/faizahmd2/pinproc/internal/identity"
	"github.com/faizahmd2/pinproc/internal/report"
	"github.com/faizahmd2/pinproc/internal/rules"
	"github.com/faizahmd2/pinproc/internal/source"
	"github.com/spf13/cobra"
)

// newReportCmd prints a report. By default it runs a fresh investigation in-process
// (no daemon, no socket) and prints it; --last prints the daemon's last capture.
func newReportCmd() *cobra.Command {
	var last bool
	var hint, dimension string
	cmd := &cobra.Command{
		Use:   "report",
		Short: "print an investigation report (fresh by default, or the last capture)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if last {
				text, err := report.ReadText(config.DataDirectory)
				if err != nil {
					if os.IsNotExist(err) {
						return fmt.Errorf("no captured report yet at %s", config.DataDirectory)
					}
					return err
				}
				fmt.Fprint(cmd.OutOrStdout(), text)
				return nil
			}
			text, err := investigateNow(hint, dimension)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), text)
			return nil
		},
	}
	cmd.Flags().BoolVar(&last, "last", false, "print the daemon's last captured report instead of running now")
	cmd.Flags().StringVar(&hint, "hint", "", "operator context, e.g. \"checkout latency\"")
	cmd.Flags().StringVar(&dimension, "dimension", "", "focus: cpu|memory|io|network|filesystem|limits")
	return cmd
}

// investigateNow runs a one-shot investigation in-process and returns the rendered
// report text. It is fully local — no socket, no egress.
func investigateNow(hint, dimension string) (string, error) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return "", err
	}
	src := source.NewLocalWithTimeout("/proc", "/sys", 8<<20, cfg.Source.ReadTimeout)
	defer src.Close()
	if err := src.StartupCheck(); err != nil {
		return "", err
	}
	reg, err := capability.BuildBuiltin()
	if err != nil {
		return "", err
	}
	eng := engine.New(engine.Options{
		Source: src, Registry: reg, Rules: rules.Default(), Decision: drules.New(),
		Identity: identity.New(src), Budget: contract.BudgetNormal(), ParallelWidth: 3,
		MaxFindings: cfg.Report.MaxFindings, Logger: logger,
	})
	inv, err := eng.Run(context.Background(), engine.Request{
		ID:        fmt.Sprintf("manual-%d", time.Now().UnixNano()),
		Host:      localHostName(),
		Trigger:   "manual",
		Hint:      hint,
		Dimension: contract.Dimension(dimension),
	})
	if err != nil {
		return "", err
	}
	return report.RenderMarkdown(inv), nil
}
