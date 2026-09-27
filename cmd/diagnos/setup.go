package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/faizahmd2/pinproc/internal/config"
	"github.com/faizahmd2/pinproc/internal/provider"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newSetupCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "setup", Short: "configure pinproc without editing configuration files"}
	cmd.AddCommand(newSetupAICmd(), newSetupCallbackCmd(), newSetupServerCmd())
	return cmd
}

func configForCLI() (*config.Config, error) {
	return config.Load(config.ManagedPath)
}

func requireRoot() error {
	if os.Geteuid() != 0 {
		return errors.New("run this command as root, for example: sudo pinproc setup ai")
	}
	return nil
}

func newSetupAICmd() *cobra.Command {
	var providerID string
	var disable bool
	cmd := &cobra.Command{
		Use:   "ai",
		Short: "configure or disable an AI provider",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireRoot(); err != nil {
				return err
			}
			cfg, err := configForCLI()
			if err != nil {
				return err
			}
			if disable {
				cfg.AI.Enabled = false
				cfg.AI.Provider = ""
				cfg.AI.Config = map[string]any{}
				if err := config.SaveManaged(cfg); err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), "AI disabled. Configuration saved.")
				return restartService(cmd.OutOrStdout())
			}

			manifests, err := provider.List(provider.ManifestDir)
			if err != nil {
				return err
			}
			ready := make([]provider.Manifest, 0, len(manifests))
			for _, m := range manifests {
				if provider.BinaryInstalled(m, provider.BinaryDir) {
					ready = append(ready, m)
				}
			}
			if len(ready) == 0 {
				return errors.New("no usable AI provider is installed; install a provider package first")
			}

			selected, err := selectProvider(os.Stdin, cmd.OutOrStdout(), ready, providerID, cfg.AI.Provider)
			if err != nil {
				return err
			}

			values := map[string]any{}
			var current map[string]any
			if cfg.AI.Enabled && cfg.AI.Provider == selected.ID {
				current = cfg.AI.Config
			}
			for _, setting := range selected.Settings {
				value, keep, err := promptSetting(os.Stdin, cmd.OutOrStdout(), setting, current)
				if err != nil {
					return err
				}
				if keep {
					value = current[setting.Key]
				}
				if value != nil || keep {
					values[setting.Key] = value
				}
			}

			cfg.AI.Enabled = true
			cfg.AI.Provider = selected.ID
			cfg.AI.Config = values
			if err := config.SaveManaged(cfg); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "AI provider configured: %s
", selected.Name)
			return restartService(cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&providerID, "provider", "", "provider ID")
	cmd.Flags().BoolVar(&disable, "disable", false, "disable AI and clear provider configuration")
	return cmd
}

func newSetupCallbackCmd() *cobra.Command {
	var disable bool
	cmd := &cobra.Command{
		Use:   "callback",
		Short: "configure or disable the report callback",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireRoot(); err != nil {
				return err
			}
			cfg, err := configForCLI()
			if err != nil {
				return err
			}
			if disable {
				cfg.Callback.Enabled = false
				cfg.Callback.URL = ""
				if err := config.SaveManaged(cfg); err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), "Callback disabled. Configuration saved.")
				return restartService(cmd.OutOrStdout())
			}

			reader := bufio.NewReader(os.Stdin)
			value, err := promptLine(reader, cmd.OutOrStdout(), "Callback URL", cfg.Callback.URL)
			if err != nil {
				return err
			}
			value = strings.TrimSpace(value)
			if err := validateHTTPURL(value); err != nil {
				return err
			}
			cfg.Callback.Enabled = true
			cfg.Callback.URL = value
			if err := config.SaveManaged(cfg); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Callback configured. Configuration saved.")
			return restartService(cmd.OutOrStdout())
		},
	}
	cmd.Flags().BoolVar(&disable, "disable", false, "disable callback and clear its URL")
	return cmd
}

func newSetupServerCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "server",
		Short: "configure the API listener and optional API key",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireRoot(); err != nil {
				return err
			}
			cfg, err := configForCLI()
			if err != nil {
				return err
			}

			reader := bufio.NewReader(os.Stdin)
			listen, err := promptLine(reader, cmd.OutOrStdout(), "Listen address", cfg.Server.Listen)
			if err != nil {
				return err
			}
			listen = strings.TrimSpace(listen)
			host, _, err := net.SplitHostPort(listen)
			if err != nil {
				return fmt.Errorf("listen address must be host:port: %w", err)
			}

			fmt.Fprint(cmd.OutOrStdout(), "API key (leave blank to keep current, '-' to clear): ")
			secret, err := readSecret(os.Stdin, cfg.Server.APIKey != "")
			if err != nil {
				return err
			}
			switch strings.TrimSpace(secret) {
			case "-":
				cfg.Server.APIKey = ""
			case "":
				// Keep the current key.
			default:
				cfg.Server.APIKey = secret
			}

			if !isLoopbackListenHost(host) && cfg.Server.APIKey == "" {
				return errors.New("a non-loopback listener requires an API key")
			}
			cfg.Server.Listen = listen
			if err := config.SaveManaged(cfg); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Server configuration saved.")
			return restartService(cmd.OutOrStdout())
		},
	}
}

func selectProvider(in *os.File, out io.Writer, manifests []provider.Manifest, requested, current string) (provider.Manifest, error) {
	if requested != "" {
		for _, m := range manifests {
			if m.ID == requested {
				return m, nil
			}
		}
		return provider.Manifest{}, fmt.Errorf("provider %q is not installed and ready", requested)
	}
	if current != "" {
		for _, m := range manifests {
			if m.ID == current {
				return m, nil
			}
		}
	}
	if len(manifests) == 1 {
		return manifests[0], nil
	}
	reader := bufio.NewReader(in)
	for i, m := range manifests {
		fmt.Fprintf(out, "%d) %s (%s)
", i+1, m.Name, m.ID)
	}
	value, err := promptLine(reader, out, "Select provider", "1")
	if err != nil {
		return provider.Manifest{}, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n < 1 || n > len(manifests) {
		return provider.Manifest{}, errors.New("invalid provider selection")
	}
	return manifests[n-1], nil
}

func promptSetting(in *os.File, out io.Writer, setting provider.Setting, current map[string]any) (any, bool, error) {
	currentValue, hasCurrent := current[setting.Key]
	required := ""
	if setting.Required {
		required = " required"
	}
	label := setting.Label
	if label == "" {
		label = setting.Key
	}

	if setting.Type == "secret" {
		fmt.Fprintf(out, "%s%s: ", label, required)
		value, err := readSecret(in, hasCurrent)
		if err != nil {
			return nil, false, err
		}
		if value == "" && hasCurrent {
			return nil, true, nil
		}
		if value == "" && setting.Required {
			return nil, false, fmt.Errorf("%s is required", setting.Key)
		}
		return value, false, nil
	}

	def := ""
	if hasCurrent {
		def = fmt.Sprint(currentValue)
	} else if setting.Default != nil {
		def = fmt.Sprint(setting.Default)
	}
	reader := bufio.NewReader(in)
	value, err := promptLine(reader, out, label+required, def)
	if err != nil {
		return nil, false, err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		if hasCurrent {
			return nil, true, nil
		}
		if setting.Default != nil {
			return setting.Default, false, nil
		}
		if setting.Required {
			return nil, false, fmt.Errorf("%s is required", setting.Key)
		}
		return nil, false, nil
	}
	switch setting.Type {
	case "url":
		if err := validateHTTPURL(value); err != nil {
			return nil, false, fmt.Errorf("%s: %w", setting.Key, err)
		}
		return value, false, nil
	case "boolean":
		switch strings.ToLower(value) {
		case "y", "yes", "true", "1":
			return true, false, nil
		case "n", "no", "false", "0":
			return false, false, nil
		default:
			return nil, false, fmt.Errorf("%s must be yes or no", setting.Key)
		}
	default:
		return value, false, nil
	}
}

func promptLine(reader *bufio.Reader, out io.Writer, label, def string) (string, error) {
	if def != "" {
		fmt.Fprintf(out, "%s [%s]: ", label, def)
	} else {
		fmt.Fprintf(out, "%s: ", label)
	}
	value, err := reader.ReadString('
')
	if err != nil && len(value) == 0 {
		return "", err
	}
	value = strings.TrimRight(value, "
")
	if value == "" {
		return def, nil
	}
	return value, nil
}

func readSecret(in *os.File, hasCurrent bool) (string, error) {
	if !term.IsTerminal(int(in.Fd())) {
		return "", errors.New("secret setup requires an interactive terminal")
	}
	value, err := term.ReadPassword(int(in.Fd()))
	fmt.Fprintln(os.Stdout)
	if err != nil {
		return "", err
	}
	if len(value) == 0 && hasCurrent {
		return "", nil
	}
	return string(value), nil
}

func validateHTTPURL(value string) error {
	u, err := url.Parse(value)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return errors.New("URL must be an absolute http or https URL")
	}
	return nil
}

func isLoopbackListenHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func restartService(out io.Writer) error {
	if _, err := os.Stat("/usr/lib/systemd/system/pinproc.service"); err != nil {
		fmt.Fprintln(out, "Systemd unit not installed; restart skipped.")
		return nil
	}
	cmd := exec.Command("systemctl", "restart", "pinproc.service")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("configuration saved but service restart failed: %s", strings.TrimSpace(string(output)))
	}
	fmt.Fprintln(out, "pinproc service restarted.")
	return nil
}
