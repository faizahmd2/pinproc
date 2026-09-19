package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/faizahmd2/vm-native-diagnos/internal/capability"
	"github.com/faizahmd2/vm-native-diagnos/internal/contract"
	drules "github.com/faizahmd2/vm-native-diagnos/internal/decision/rules"
	"github.com/faizahmd2/vm-native-diagnos/internal/engine"
	"github.com/faizahmd2/vm-native-diagnos/internal/rules"
	"github.com/faizahmd2/vm-native-diagnos/internal/source"
	"github.com/spf13/cobra"
)

type commandError int

func (e commandError) Error() string { return fmt.Sprintf("command exited %d", e) }

// newCheckCmd runs the cheap no-AI machine sweep.
func newCheckCmd() *cobra.Command {
	var quiet, asJSON bool
	cmd := &cobra.Command{Use: "check [host]", Short: "run a cheap no-AI health sweep", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		host := "localhost"
		if len(args) > 0 {
			host = args[0]
		}
		if host != "localhost" && host != "127.0.0.1" {
			return fmt.Errorf("M2 check supports local host only")
		}
		src := source.NewLocal("/proc", "/sys", 1<<20)
		defer src.Close()
		reg, e := capability.BuildBuiltin()
		if e != nil {
			return e
		}
		eng := engine.New(engine.Options{Source: src, Registry: reg, Rules: rules.Default(), Decision: drules.New(), Budget: contract.BudgetFast(), Logger: logger})
		inv, e := eng.Run(context.Background(), engine.Request{Host: host, Trigger: "cron"})
		if e != nil {
			return e
		}
		if asJSON {
			b, _ := json.MarshalIndent(inv, "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(b))
		} else if !quiet {
			fmt.Fprintf(cmd.OutOrStdout(), "status: %s\nstop: %s\nevidence: %d\nhypotheses: %d\n", status(inv), inv.StopReason, len(inv.Evidence), len(inv.Hypotheses))
		}
		if inv.StopReason != contract.StopNoAnomaly {
			return commandError(1)
		}
		return nil
	}}
	cmd.Flags().BoolVar(&quiet, "quiet", false, "suppress normal output")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit investigation JSON")
	return cmd
}
func status(inv *contract.Investigation) string {
	if inv == nil || inv.StopReason == contract.StopNoAnomaly {
		return "healthy"
	}
	return "anomaly"
}
