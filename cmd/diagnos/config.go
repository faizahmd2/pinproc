package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/faizahmd2/pinproc/internal/config"
	dprovider "github.com/faizahmd2/pinproc/internal/provider"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func newConfigCommand() *cobra.Command { cmd := &cobra.Command{Use: "config", Short: "inspect managed configuration"}; cmd.AddCommand(newConfigShowCmd(), newConfigValidateCmd()); return cmd }

func newConfigShowCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{Use: "show", RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(""); if err != nil { return err }; redactConfig(cfg)
		if asJSON { data, err := json.MarshalIndent(cfg, "", "  "); if err != nil { return err }; fmt.Fprintln(cmd.OutOrStdout(), string(data)); return nil }
		data, err := yaml.Marshal(cfg); if err != nil { return err }; fmt.Fprint(cmd.OutOrStdout(), string(data)); return nil
	}}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON"); return cmd
}

func newConfigValidateCmd() *cobra.Command { return &cobra.Command{Use: "validate", RunE: func(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(""); if err != nil { return err }
	if cfg.AI.Provider != "" { if _, err := dprovider.LoadInstalled(cfg.AI.Provider); err != nil { fmt.Fprintf(cmd.OutOrStdout(), "warning: configured AI provider %q is not installed\n", cfg.AI.Provider) } }
	fmt.Fprintln(cmd.OutOrStdout(), "configuration is valid"); return nil
}}}

func redactConfig(cfg *config.Config) {
	for k, v := range cfg.AI.Config { l := strings.ToLower(k); if strings.Contains(l, "key") || strings.Contains(l, "token") || strings.Contains(l, "secret") || strings.Contains(l, "password") || strings.Contains(l, "credential") { if v != "" { cfg.AI.Config[k] = "configured" } } }
	if cfg.Server.APIKey != "" { cfg.Server.APIKey = "configured" }
}