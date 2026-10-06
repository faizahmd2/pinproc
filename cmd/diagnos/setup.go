package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/faizahmd2/pinproc/internal/config"
	"github.com/spf13/cobra"
)

func newSetupCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "setup", Short: "configure pinproc without editing files"}
	cmd.AddCommand(newSetupCallbackCmd())
	return cmd
}

func requireRoot() error {
	if os.Geteuid() != 0 {
		return errors.New("run this command as root, for example: sudo pinproc setup callback")
	}
	return nil
}

func newSetupCallbackCmd() *cobra.Command {
	var url string
	var disable bool
	cmd := &cobra.Command{Use: "callback", Short: "set the URL that captured reports are POSTed to (as text)", RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireRoot(); err != nil {
			return err
		}
		cfg, err := config.Load("")
		if err != nil {
			return err
		}
		if disable {
			cfg.Callback.Enabled = false
			cfg.Callback.URL = ""
		} else {
			if url == "" {
				fmt.Fprint(cmd.OutOrStdout(), "Callback URL: ")
				if url, err = readLine(cmd.InOrStdin()); err != nil {
					return err
				}
			}
			cfg.Callback.URL = strings.TrimSpace(url)
			cfg.Callback.Enabled = cfg.Callback.URL != ""
		}
		if err := config.Save(config.ConfigPath, cfg); err != nil {
			return err
		}
		if err := restartService(); err != nil {
			return err
		}
		if cfg.Callback.Enabled {
			fmt.Fprintln(cmd.OutOrStdout(), "Callback configured. pinproc restarted.")
		} else {
			fmt.Fprintln(cmd.OutOrStdout(), "Callback disabled. pinproc restarted.")
		}
		return nil
	}}
	cmd.Flags().StringVar(&url, "url", "", "callback URL")
	cmd.Flags().BoolVar(&disable, "disable", false, "disable callback")
	return cmd
}

func readLine(r io.Reader) (string, error) {
	scanner := bufio.NewScanner(r)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", err
		}
		return "", io.EOF
	}
	return strings.TrimSpace(scanner.Text()), nil
}

func restartService() error {
	systemctl, err := exec.LookPath("systemctl")
	if err != nil {
		return nil
	}
	out, err := exec.Command(systemctl, "restart", "pinproc.service").CombinedOutput()
	if err != nil {
		return fmt.Errorf("restart pinproc service: %s", strings.TrimSpace(string(out)))
	}
	return nil
}
