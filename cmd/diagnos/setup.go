package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/faizahmd2/pinproc/internal/config"
	dprovider "github.com/faizahmd2/pinproc/internal/provider"
	"github.com/spf13/cobra"
)

func newSetupCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "setup", Short: "configure pinproc without editing files"}
	cmd.AddCommand(newSetupAICmd(), newSetupCallbackCmd(), newSetupServerCmd())
	return cmd
}

func requireRoot() error {
	if os.Geteuid() != 0 { return errors.New("run this command as root, for example: sudo pinproc setup ai") }
	return nil
}

func newSetupAICmd() *cobra.Command {
	var selected string
	cmd := &cobra.Command{Use: "ai", Short: "configure an installed AI provider", RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireRoot(); err != nil { return err }
		providers, err := dprovider.Discover(config.ProviderManifests); if err != nil { return err }
		available := providers[:0]
		for _, p := range providers { if _, err := os.Stat(p.Executable); err == nil { available = append(available, p) } }
		if len(available) == 0 { fmt.Fprintln(cmd.OutOrStdout(), "No AI providers are installed."); fmt.Fprintln(cmd.OutOrStdout(), "Install a provider package first; pinproc will continue with deterministic rules."); return nil }
		m, err := chooseProvider(cmd, available, selected); if err != nil { return err }
		cfg, err := config.Load(""); if err != nil { return err }
		if cfg.AI.Provider != m.ID { cfg.AI.Config = map[string]string{} }
		if cfg.AI.Config == nil { cfg.AI.Config = map[string]string{} }
		cfg.AI.Provider = m.ID
		reader := bufio.NewReader(cmd.InOrStdin())
		for _, setting := range m.Settings {
			current := cfg.AI.Config[setting.Name]
			value, changed, err := promptSetting(cmd, reader, setting, current); if err != nil { return err }
			if changed { cfg.AI.Config[setting.Name] = value }
			if setting.Required && strings.TrimSpace(cfg.AI.Config[setting.Name]) == "" { return fmt.Errorf("required setting %q was not configured", setting.Name) }
		}
		if err := config.Save(config.ConfigPath, cfg); err != nil { return err }
		if err := restartService(); err != nil { return err }
		fmt.Fprintf(cmd.OutOrStdout(), "AI provider %q configured. pinproc service restarted.\n", m.Name)
		return nil
	}}
	cmd.Flags().StringVar(&selected, "provider", "", "provider id")
	return cmd
}

func newSetupCallbackCmd() *cobra.Command {
	var url string; var disable bool
	cmd := &cobra.Command{Use: "callback", Short: "configure the report callback", RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireRoot(); err != nil { return err }
		cfg, err := config.Load(""); if err != nil { return err }
		if disable { cfg.Callback.Enabled = false; cfg.Callback.URL = "" } else {
			if url == "" { fmt.Fprint(cmd.OutOrStdout(), "Callback URL: "); url, err = readLine(cmd.InOrStdin()); if err != nil { return err } }
			cfg.Callback.URL = strings.TrimSpace(url); cfg.Callback.Enabled = cfg.Callback.URL != ""
		}
		if err := config.Save(config.ConfigPath, cfg); err != nil { return err }
		if err := restartService(); err != nil { return err }
		if cfg.Callback.Enabled { fmt.Fprintln(cmd.OutOrStdout(), "Callback configured. pinproc service restarted.") } else { fmt.Fprintln(cmd.OutOrStdout(), "Callback disabled. pinproc service restarted.") }
		return nil
	}}
	cmd.Flags().StringVar(&url, "url", "", "callback URL"); cmd.Flags().BoolVar(&disable, "disable", false, "disable callback")
	return cmd
}

func newSetupServerCmd() *cobra.Command {
	var listen string; var clearKey bool
	cmd := &cobra.Command{Use: "server", Short: "configure the local pinproc API", RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireRoot(); err != nil { return err }
		cfg, err := config.Load(""); if err != nil { return err }
		if cmd.Flags().Changed("listen") { cfg.Server.Listen = strings.TrimSpace(listen) }
		if clearKey {
			cfg.Server.APIKey = ""
		} else {
			reader := bufio.NewReader(cmd.InOrStdin())
			current := cfg.Server.APIKey
			label := "Remote API key"
			if current != "" { fmt.Fprint(cmd.OutOrStdout(), label+" [configured, press Enter to keep]: ") } else { fmt.Fprint(cmd.OutOrStdout(), label+" (press Enter to leave disabled): ") }
			value, err := readSecret(reader, cmd.OutOrStdout())
			if err != nil { return err }
			if value != "" { cfg.Server.APIKey = value }
		}
		if err := config.Save(config.ConfigPath, cfg); err != nil { return err }
		if err := restartService(); err != nil { return err }
		fmt.Fprintln(cmd.OutOrStdout(), "Server configuration updated. pinproc service restarted.")
		return nil
	}}
	cmd.Flags().StringVar(&listen, "listen", "", "listen address, default 127.0.0.1:8080")
	cmd.Flags().BoolVar(&clearKey, "clear-api-key", false, "disable remote API authentication")
	return cmd
}

func chooseProvider(cmd *cobra.Command, providers []dprovider.Manifest, selected string) (dprovider.Manifest, error) {
	if selected != "" { for _, p := range providers { if p.ID == selected { return p, nil } }; return dprovider.Manifest{}, fmt.Errorf("provider %q is not installed", selected) }
	fmt.Fprintln(cmd.OutOrStdout(), "Installed AI providers:")
	for i, p := range providers { fmt.Fprintf(cmd.OutOrStdout(), "  %d. %s (%s)\n", i+1, p.Name, p.ID) }
	fmt.Fprint(cmd.OutOrStdout(), "Choose a provider: ")
	line, err := readLine(cmd.InOrStdin()); if err != nil { return dprovider.Manifest{}, err }
	n, err := strconv.Atoi(line); if err != nil || n < 1 || n > len(providers) { return dprovider.Manifest{}, fmt.Errorf("invalid provider selection") }
	return providers[n-1], nil
}

func promptSetting(cmd *cobra.Command, reader *bufio.Reader, setting dprovider.Setting, current string) (string, bool, error) {
	label := setting.Label; if label == "" { label = setting.Name }
	if current != "" { if setting.Type == "secret" { fmt.Fprintf(cmd.OutOrStdout(), "%s [configured, press Enter to keep]: ", label) } else { fmt.Fprintf(cmd.OutOrStdout(), "%s [%s]: ", label, current) } } else { fmt.Fprintf(cmd.OutOrStdout(), "%s%s: ", label, func() string { if setting.Required { return " (required)" }; return "" }()) }
	var value string; var err error
	if setting.Type == "secret" { value, err = readSecret(reader, cmd.OutOrStdout()) } else { value, err = reader.ReadString(10); value = strings.TrimSpace(value) }
	if err != nil { return "", false, err }
	if value == "" && current != "" { return current, false, nil }
	if value == "" && setting.Default != "" { return setting.Default, true, nil }
	return value, true, nil
}

func readLine(r io.Reader) (string, error) {
	scanner := bufio.NewScanner(r); if !scanner.Scan() { if err := scanner.Err(); err != nil { return "", err }; return "", io.EOF }; return strings.TrimSpace(scanner.Text()), nil
}

func readSecret(reader *bufio.Reader, out io.Writer) (string, error) {
	interactive := false; if f, err := os.Stdin.Stat(); err == nil { interactive = f.Mode()&os.ModeCharDevice != 0 }
	if interactive { _ = exec.Command("stty", "-echo").Run(); defer exec.Command("stty", "echo").Run() }
	value, err := reader.ReadString(10); if interactive { fmt.Fprintln(out) }; return strings.TrimSpace(value), err
}

func restartService() error {
	systemctl, err := exec.LookPath("systemctl"); if err != nil { return nil }
	out, err := exec.Command(systemctl, "restart", "pinproc.service").CombinedOutput()
	if err != nil { return fmt.Errorf("restart pinproc service: %s", strings.TrimSpace(string(out))) }
	return nil
}