package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/faizahmd2/pinproc/internal/config"
	dprovider "github.com/faizahmd2/pinproc/internal/provider"
	"github.com/spf13/cobra"
)

func newAICommand() *cobra.Command {
	cmd := &cobra.Command{Use: "ai", Short: "inspect AI provider configuration"}
	cmd.AddCommand(newAIListCmd(), newAIStatusCmd(), newAIRemoveCmd())
	return cmd
}

func newAIListCmd() *cobra.Command { return &cobra.Command{Use: "list", RunE: func(cmd *cobra.Command, args []string) error {
	providers, err := dprovider.Discover(config.ProviderManifests); if err != nil { return err }
	if len(providers) == 0 { fmt.Fprintln(cmd.OutOrStdout(), "No AI providers installed."); return nil }
	for _, p := range providers { state := "installed"; if _, err := os.Stat(p.Executable); err != nil { state = "manifest-only" }; fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", p.ID, p.Name, state) }
	return nil
}}}

func newAIStatusCmd() *cobra.Command { return &cobra.Command{Use: "status", RunE: func(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(""); if err != nil { return err }
	if strings.TrimSpace(cfg.AI.Provider) == "" { fmt.Fprintln(cmd.OutOrStdout(), "AI provider: not configured"); fmt.Fprintln(cmd.OutOrStdout(), "Decision mode: deterministic rules"); return nil }
	m, err := dprovider.LoadInstalled(cfg.AI.Provider); if err != nil { fmt.Fprintf(cmd.OutOrStdout(), "AI provider: %s (unavailable)\n", cfg.AI.Provider); fmt.Fprintln(cmd.OutOrStdout(), "Decision mode: deterministic rules"); return nil }
	fmt.Fprintf(cmd.OutOrStdout(), "AI provider: %s (%s)\n", m.Name, m.ID); fmt.Fprintln(cmd.OutOrStdout(), "Decision mode: AI provider with deterministic fallback")
	for _, s := range m.Settings { value := cfg.AI.Config[s.Name]; if s.Type == "secret" { if value != "" { value = "configured" } else { value = "not configured" } }; if value == "" { value = "not configured" }; fmt.Fprintf(cmd.OutOrStdout(), "  %s: %s\n", s.Name, value) }
	return nil
}}}

func newAIRemoveCmd() *cobra.Command { return &cobra.Command{Use: "remove", Short: "disable AI reasoning", RunE: func(cmd *cobra.Command, args []string) error {
	if err := requireRoot(); err != nil { return err }; cfg, err := config.Load(""); if err != nil { return err }; cfg.AI.Provider = ""; cfg.AI.Config = map[string]string{}
	if err := config.Save(config.ConfigPath, cfg); err != nil { return err }; if err := restartService(); err != nil { return err }; fmt.Fprintln(cmd.OutOrStdout(), "AI reasoning disabled. pinproc service restarted."); return nil
}}}