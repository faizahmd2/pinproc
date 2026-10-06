package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/faizahmd2/pinproc/internal/config"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func newConfigCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "config", Short: "inspect managed configuration"}
	cmd.AddCommand(newConfigShowCmd(), newConfigValidateCmd())
	return cmd
}

// ensureManagedConfigReadable refuses to fall back to built-in defaults when the
// managed config exists but the current user cannot read it — printing defaults as
// if they were the live config would be a lie. Only enforced without --config.
func ensureManagedConfigReadable() error {
	if cfgPath != "" {
		return nil
	}
	if _, err := os.Stat(config.ConfigPath); err != nil {
		if os.IsPermission(err) {
			return managedUnreadableErr()
		}
	} else {
		return nil
	}
	if _, derr := os.Stat(filepath.Dir(config.ConfigPath)); os.IsPermission(derr) {
		return managedUnreadableErr()
	}
	return nil
}

func managedUnreadableErr() error {
	return fmt.Errorf("cannot read managed configuration at %s (permission denied) — re-run with sudo", config.ConfigPath)
}

func newConfigShowCmd() *cobra.Command {
	return &cobra.Command{Use: "show", RunE: func(cmd *cobra.Command, args []string) error {
		if err := ensureManagedConfigReadable(); err != nil {
			return err
		}
		cfg, err := config.Load(cfgPath)
		if err != nil {
			return err
		}
		data, err := yaml.Marshal(cfg)
		if err != nil {
			return err
		}
		fmt.Fprint(cmd.OutOrStdout(), string(data))
		return nil
	}}
}

func newConfigValidateCmd() *cobra.Command {
	return &cobra.Command{Use: "validate", RunE: func(cmd *cobra.Command, args []string) error {
		if err := ensureManagedConfigReadable(); err != nil {
			return err
		}
		if _, err := config.Load(cfgPath); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "configuration is valid")
		return nil
	}}
}
