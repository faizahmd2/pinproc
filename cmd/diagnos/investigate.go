package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/faizahmd2/diagnos/internal/capability"
	"github.com/faizahmd2/diagnos/internal/config"
	"github.com/faizahmd2/diagnos/internal/contract"
	"github.com/faizahmd2/diagnos/internal/engine"
	"github.com/faizahmd2/diagnos/internal/identity"
	"github.com/faizahmd2/diagnos/internal/narrator"
	"github.com/faizahmd2/diagnos/internal/report"
	"github.com/faizahmd2/diagnos/internal/rules"
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
		src, closeFn, loadedCfg, err := targetSource(context.Background(), host)
		if err != nil {
			return err
		}
		defer closeFn()
		if loadedCfg == nil && cfgPath != "" {
			loadedCfg, err = config.Load(cfgPath)
			if err != nil {
				return err
			}
		}
		reg, err := capability.BuildBuiltin()
		if err != nil {
			return err
		}
		b, err := budget(budgetName)
		if err != nil {
			return err
		}
		if loadedCfg != nil && budgetName == "normal" {
			b, _ = budget(loadedCfg.Engine.Budget)
		}
		dec, err := makeDecisionProvider(loadedCfg, noAI)
		if err != nil {
			return err
		}
		eng := engine.New(engine.Options{Source: src, Registry: reg, Rules: rules.Default(), Decision: dec, Identity: identity.New(src), Budget: b, Logger: logger})
		inv, err := eng.Run(context.Background(), engine.Request{Host: host, Trigger: trigger, Hint: hint, Dimension: contract.Dimension(dim)})
		if err != nil {
			return err
		}
		if loadedCfg == nil || loadedCfg.Narrator.Enabled {
			if text, ne := narrator.NewRules().Narrate(context.Background(), inv); ne == nil {
				if narrator.Validate(inv, text) == nil {
					inv.Narrative = text
				}
			}
		}
		if !asJSON {
			_ = report.Terminal(cmd.OutOrStdout(), inv)
		}
		if out == "" {
			if loadedCfg != nil && loadedCfg.Output.Directory != "" {
				out, _ = config.ResolveOutputDirectory(loadedCfg.Output.Directory)
			} else {
				out = "."
			}
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
