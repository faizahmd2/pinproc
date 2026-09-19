package main

import (
	"fmt"
	"strings"

	"github.com/faizahmd2/vm-native-diagnos/internal/capability"
	"github.com/spf13/cobra"
)

// newCapabilitiesCmd lists the legal investigation graph.
func newCapabilitiesCmd() *cobra.Command {
	var markdown, asJSON bool
	cmd := &cobra.Command{Use: "capabilities", Short: "list registered V2 investigation capabilities", RunE: func(cmd *cobra.Command, _ []string) error {
		reg, err := capability.BuildBuiltin()
		if err != nil {
			return err
		}
		caps := reg.All()
		if asJSON {
			type row struct {
				ID        string   `json:"id"`
				Dimension string   `json:"dimension"`
				Level     int      `json:"level"`
				Cost      string   `json:"cost"`
				Accepts   string   `json:"accepts"`
				LeadsTo   []string `json:"leads_to"`
				Summary   string   `json:"summary"`
			}
			out := make([]row, 0, len(caps))
			for _, c := range caps {
				out = append(out, row{c.ID, string(c.Dimension), int(c.Level), string(c.Cost), string(c.Accepts), c.LeadsTo, c.Summary})
			}
			return writeJSON(cmd.OutOrStdout(), out)
		}
		if markdown {
			fmt.Fprintln(cmd.OutOrStdout(), "# Diagnos V2 Capabilities")
			for _, c := range caps {
				fmt.Fprintf(cmd.OutOrStdout(), "## `%s`\n\n- dimension: `%s`\n- level: `%d`\n- cost: `%s`\n- accepts: `%s`\n- leads to: `%s`\n\n%s\n\n", c.ID, c.Dimension, c.Level, c.Cost, c.Accepts, strings.Join(c.LeadsTo, ", "), c.Summary)
			}
			return nil
		}
		for _, c := range caps {
			fmt.Fprintf(cmd.OutOrStdout(), "%-24s L%d %-10s -> %s\n", c.ID, c.Level, c.Cost, strings.Join(c.LeadsTo, ", "))
		}
		return nil
	}}
	cmd.Flags().BoolVar(&markdown, "markdown", false, "render Markdown")
	cmd.Flags().BoolVar(&asJSON, "json", false, "render JSON")
	return cmd
}
