package main

import (
	"context"
	"encoding/json"
	"fmt"
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
	"path/filepath"
)

// newInvestigateCmd runs a bounded investigation.
func newInvestigateCmd() *cobra.Command {
	var hint, dim, budgetName, out, trigger string
	var asJSON, noAI bool
	cmd := &cobra.Command{Use: "investigate [host]", Short: "investigate a Linux resource anomaly", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		host := "localhost"
		if len(args) > 0 {
			host = args[0]
		}
		loadedCfg, err := config.Load(cfgPath)
		if err != nil { return err }
		if out == "" {
			out, err = config.ResolveOutputDirectory(loadedCfg.Output.Directory)
		} else {
			out, err = config.ResolveOutputDirectory(out)
		}
		if err != nil { return err }
		if err := report.EnsureWritable(out); err != nil {
			return fmt.Errorf("output directory unavailable: %w", err)
		}
		b, err := budget(budgetName)
		if err != nil { return err }
		if budgetName == "normal" { b, _ = budget(loadedCfg.Engine.Budget) }
		invID := fmt.Sprintf("inv-%d", time.Now().UnixNano())
		src, closeFn, err := targetSource(context.Background(), host, loadedCfg.Source.ReadTimeout)
		if err != nil {
			_ = report.WriteFailure(out, invID, host, trigger, hint, b, err.Error(), contract.StopError)
			return err
		}
		defer closeFn()
		reg, err := capability.BuildBuiltin()
		if err != nil { return err }
		dec, err := makeDecisionProvider(loadedCfg, noAI)
		if err != nil {
			return err
		}
		eng := engine.New(engine.Options{Source: src, Registry: reg, Rules: rules.Default(), Decision: dec, Identity: identity.New(src), Budget: b, Logger: logger, ParallelWidth: loadedCfg.Engine.ParallelWidth, MaxFindings: loadedCfg.Report.MaxFindings, DecisionNotice: decisionNotice(loadedCfg, noAI)})
		runCtx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			t := time.NewTicker(5 * time.Second)
			defer t.Stop()
			for {
				select {
				case <-t.C:
					logger.Info("investigation still running", "id", invID)
				case <-done:
					return
				}
			}
		}()
		inv, err := eng.Run(runCtx, engine.Request{ID: invID, Host: host, Trigger: trigger, Hint: hint, Dimension: contract.Dimension(dim)})
		close(done)
		cancel()
		if err != nil {
			_ = report.WriteFailure(out, invID, host, trigger, hint, b, err.Error(), contract.StopError)
			return fmt.Errorf("investigation failed completely: %w (failure report: %s)", err, filepath.Join(out, invID, "report.md"))
		}
		if loadedCfg.Narrator.Enabled {
			if text, ne := narrator.NewRules().Narrate(context.Background(), inv); ne == nil {
				if narrator.Validate(inv, text) == nil {
					inv.Narrative = text
				}
			}
		}
		if !asJSON {
			_ = report.Terminal(cmd.OutOrStdout(), inv)
		}
		if err := report.Write(inv, filepath.Clean(out)); err != nil {
			return err
		}
		if asJSON {
			data, _ := json.MarshalIndent(inv, "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(data))
		}
		return nil
	}}
	cmd.Flags().StringVar(&hint, "hint", "", "operator context")
	cmd.Flags().StringVar(&trigger, "trigger", "manual", "incident trigger/source")
	cmd.Flags().StringVar(&dim, "dimension", "", "cpu|memory|io|network|scheduling|filesystem|limits")
	cmd.Flags().StringVar(&budgetName, "budget", "normal", "fast|normal|deep")
	cmd.Flags().StringVar(&out, "out", "", "report directory")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON")
	cmd.Flags().BoolVar(&noAI, "no-ai", false, "disable AI decisions")
	return cmd
}
