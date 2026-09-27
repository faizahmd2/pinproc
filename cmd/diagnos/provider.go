package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/faizahmd2/pinproc/internal/config"
	"github.com/faizahmd2/pinproc/internal/provider"
	"github.com/spf13/cobra"
)

func newProviderCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "provider",
		Short: "inspect installed AI providers",
	}
	cmd.AddCommand(newProviderListCmd(), newProviderShowCmd())
	return cmd
}

func newProviderListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "list installed provider adapters",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			manifests, err := provider.List(provider.ManifestDir)
			if err != nil {
				return err
			}
			if len(manifests) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No provider adapters are installed.")
				return nil
			}
			for _, m := range manifests {
				status := "unavailable"
				if provider.BinaryInstalled(m, provider.BinaryDir) {
					status = "ready"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%-20s %-24s %s\n", m.ID, m.Name, status)
			}
			return nil
		},
	}
}

func newProviderShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <provider>",
		Short: "show provider metadata and settings",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := provider.Find(strings.TrimSpace(args[0]))
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "ID: %s\nName: %s\nProtocol: %d\nExecutable: %s\n", m.ID, m.Name, m.ProtocolVersion, m.Executable)
			if m.Description != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "Description: %s\n", m.Description)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Binary: %s\n", m.BinaryPath(provider.BinaryDir))
			if _, err := os.Stat(m.BinaryPath(provider.BinaryDir)); err != nil {
				fmt.Fprintln(cmd.OutOrStdout(), "Status: unavailable")
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "Status: ready")
			}
			if len(m.Settings) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "Settings: none")
				return nil
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Settings:")
			for _, s := range m.Settings {
				required := "optional"
				if s.Required {
					required = "required"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "  %-20s %-8s %s\n", s.Key, s.Type, required)
			}
			return nil
		},
	}
}

func newAIStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ai-status",
		Short: "show configured AI provider status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireRoot(); err != nil {
				return err
			}
			cfg, err := configForCLI()
			if err != nil {
				return err
			}
			if !cfg.AI.Enabled || cfg.AI.Provider == "" {
				fmt.Fprintln(cmd.OutOrStdout(), "AI: not configured")
				fmt.Fprintln(cmd.OutOrStdout(), "Decision mode: deterministic rules")
				return nil
			}
			m, err := provider.Find(cfg.AI.Provider)
			if err != nil || !provider.BinaryInstalled(m, provider.BinaryDir) {
				fmt.Fprintf(cmd.OutOrStdout(), "AI: configured (%s), provider unavailable\n", cfg.AI.Provider)
				fmt.Fprintln(cmd.OutOrStdout(), "Decision mode: deterministic rules")
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "AI: configured (%s)\n", m.Name)
			fmt.Fprintln(cmd.OutOrStdout(), "Decision mode: configured provider")
			return nil
		},
	}
}
