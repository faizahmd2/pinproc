package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/faizahmd2/pinproc/internal/capability"
	"github.com/faizahmd2/pinproc/internal/config"
	"github.com/faizahmd2/pinproc/internal/contract"
	"github.com/faizahmd2/pinproc/internal/engine"
	"github.com/faizahmd2/pinproc/internal/identity"
	"github.com/faizahmd2/pinproc/internal/narrator"
	"github.com/faizahmd2/pinproc/internal/report"
	"github.com/faizahmd2/pinproc/internal/rules"
	"github.com/spf13/cobra"
)

// newInvestigateCmd is retained only as a hidden local-development CLI.
func newInvestigateCmd() *cobra.Command {
	var hint, dim, out, trigger string
	var asJSON bool
	cmd := &cobra.Command{
		Use: "investigate [host]",
		Short: "local development investigation command",
		Hidden: true,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			host := "localhost"
			if len(args) > 0 {
				host = args[0]
			}
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return err
			}
			if out == "" {
				out = config.StateDir
			} else {
				out, err = config.ResolveOutputDirectory(out)
				if err != nil {
					return err
				}
			}
			if err := report.EnsureWritable(out); err != nil {
				return fmt.Errorf("output directory unavailable: %w", err)
			}

			src, closeFn, err := targetSource(context.Background(), host, cfg.Source.ReadTimeout)
			if err != nil {
				return err
			}
			defer closeFn()

			reg, err := capability.BuildBuiltin()
			if err != nil {
				return err
			}
			dec, err := makeDecisionProvider(cfg)
			if err != nil {
				return err
			}

			invID := fmt.Sprintf("inv-%d", time.Now().UnixNano())
			eng := engine.New(engine.Options{
				Source: src, Registry: reg, Rules: rules.Default(), Decision: dec,
				Identity: identity.New(src), Budget: contract.BudgetNormal(), ParallelWidth: 3,
				MaxFindings: cfg.Report.MaxFindings, DecisionNotice: decisionNotice(cfg), Logger: logger,
			})
			inv, err := eng.Run(context.Background(), engine.Request{
				ID: invID, Host: host, Trigger: trigger, Hint: hint, Dimension: contract.Dimension(dim),
			})
			if err != nil {
				fail := report.Failure(time.Now(), hint, err.Error(), contract.StopError)
				_ = report.Write(fail, out)
				return err
			}
			if cfg.Narrator.Enabled {
				if text, ne := narrator.NewRules().Narrate(context.Background(), inv); ne == nil && narrator.Validate(inv, text) == nil {
					inv.Narrative = text
				}
			}
			if err := report.Write(inv, filepath.Clean(out)); err != nil {
				return err
			}
			if asJSON {
				data, _ := json.MarshalIndent(inv, "", "  ")
				fmt.Fprintln(cmd.OutOrStdout(), string(data))
				return nil
			}
			return report.Terminal(cmd.OutOrStdout(), inv)
		},
	}
	cmd.Flags().StringVar(&hint, "hint", "", "operator context")
	cmd.Flags().StringVar(&trigger, "trigger", "manual", "incident trigger/source")
	cmd.Flags().StringVar(&dim, "dimension", "", "cpu|memory|io|network|scheduling|filesystem|limits")
	cmd.Flags().StringVar(&out, "out", "", "report directory")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON")
	return cmd
}
