package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/faizahmd2/pinproc/internal/config"
	"github.com/spf13/cobra"
)

const serviceUnitPath = "/etc/systemd/system/pinproc.service"
const serviceConfigPath = "/etc/pinproc/app.yaml"
const serviceBinaryPath = "/usr/local/bin/pinproc"

func newServiceCmd() *cobra.Command {
	run := newServeCmd()
	run.Use = "run"
	run.Short = "run the persistent inspection service"

	cmd := &cobra.Command{Use: "service", Short: "manage the persistent pinproc service"}
	cmd.AddCommand(run, newServiceInstallCmd(), newServiceUninstallCmd())
	return cmd
}

func newServiceInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use: "install", Short: "install and start the systemd service",
		RunE: func(cmd *cobra.Command, args []string) error {
			if runtime.GOOS != "linux" {
				return fmt.Errorf("service install is supported on Linux only")
			}
			if os.Geteuid() != 0 {
				return fmt.Errorf("run as root: sudo %s service install", os.Args[0])
			}
			if _, err := exec.LookPath("systemctl"); err != nil {
				return fmt.Errorf("systemctl not found; this installer requires systemd")
			}
			cfgFile := cfgPath
			if cfgFile == "" {
				cfgFile = config.DiscoverPath()
			}
			if cfgFile == "" {
				return fmt.Errorf("app.yaml not found; place app.yaml in the current directory or /etc/pinproc/app.yaml first")
			}
			cfg, err := config.Load(cfgFile)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			dataDir := strings.TrimSpace(cfg.Service.DataDirectory)
			if dataDir == "" {
				dataDir = "/var/lib/pinproc"
			}
			if err := os.MkdirAll(dataDir, 0750); err != nil {
				return fmt.Errorf("create data directory: %w", err)
			}

			outputDir, err := config.ResolveOutputDirectory(cfg.Output.Directory)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(outputDir, 0750); err != nil {
				return fmt.Errorf("create report directory: %w", err)
			}

			if err := installConfig(cfgFile); err != nil {
				return err
			}
			exe, err := os.Executable()
			if err != nil {
				return err
			}
			if resolved, err := filepath.EvalSymlinks(exe); err == nil {
				exe = resolved
			}
			if err := installServiceBinary(exe); err != nil {
				return err
			}
			if err := os.WriteFile(serviceUnitPath, []byte(renderServiceUnit(serviceBinaryPath, dataDir)), 0644); err != nil {
				return fmt.Errorf("write systemd unit: %w", err)
			}
			if err := runSystemctl("daemon-reload"); err != nil {
				return err
			}
			if err := runSystemctl("enable", "pinproc.service"); err != nil {
				return err
			}
			if err := runSystemctl("restart", "pinproc.service"); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "pinproc service installed and started")
			fmt.Fprintln(cmd.OutOrStdout(), "logs: sudo journalctl -u pinproc -f")
			return nil
		},
	}
}

func newServiceUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use: "uninstall", Short: "stop and remove the systemd service",
		RunE: func(cmd *cobra.Command, args []string) error {
			if runtime.GOOS != "linux" {
				return fmt.Errorf("service uninstall is supported on Linux only")
			}
			if os.Geteuid() != 0 {
				return fmt.Errorf("run as root: sudo %s service uninstall", os.Args[0])
			}
			_ = runSystemctl("disable", "--now", "pinproc.service")
			if err := os.Remove(serviceUnitPath); err != nil && !os.IsNotExist(err) {
				return err
			}
			_ = runSystemctl("daemon-reload")
			fmt.Fprintln(cmd.OutOrStdout(), "pinproc service removed; config and reports were preserved")
			return nil
		},
	}
}

func installConfig(sourcePath string) error {
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return fmt.Errorf("read config %s: %w", sourcePath, err)
	}
	if err := os.MkdirAll(filepath.Dir(serviceConfigPath), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(serviceConfigPath, data, 0640); err != nil {
		return fmt.Errorf("write %s: %w", serviceConfigPath, err)
	}
	return nil
}

func installServiceBinary(source string) error {
	info, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("service executable %q is unavailable: %w", source, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("service executable %q is not a regular file", source)
	}
	if info.Mode().Perm()&0111 == 0 {
		return fmt.Errorf("service executable %q is not executable", source)
	}
	if err := os.MkdirAll(filepath.Dir(serviceBinaryPath), 0755); err != nil {
		return fmt.Errorf("create service binary directory: %w", err)
	}

	data, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("read service executable %q: %w", source, err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(serviceBinaryPath), ".pinproc-*")
	if err != nil {
		return fmt.Errorf("stage service executable: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0755); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod staged service executable: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write staged service executable: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync staged service executable: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close staged service executable: %w", err)
	}
	if err := os.Rename(tmpName, serviceBinaryPath); err != nil {
		return fmt.Errorf("install service executable at %s: %w", serviceBinaryPath, err)
	}
	return nil
}

func renderServiceUnit(exe, dataDir string) string {
	return fmt.Sprintf("[Unit]\nDescription=pinproc Linux inspection service\nAfter=local-fs.target network-online.target\nWants=network-online.target\n\n[Service]\nType=simple\nExecStart=%s service run --config %s\nWorkingDirectory=%s\nRestart=on-failure\nRestartSec=2s\nTimeoutStopSec=30s\nKillSignal=SIGTERM\n\n[Install]\nWantedBy=multi-user.target\n", exe, serviceConfigPath, dataDir)
}

func runSystemctl(args ...string) error {
	out, err := exec.Command("systemctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s failed: %s", strings.Join(args, " "), strings.TrimSpace(string(out)))
	}
	return nil
}
