package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"text/template"

	"github.com/faizahmd2/diagnos/internal/resources"
	"github.com/spf13/cobra"
)

type installData struct{ User, Binary, ReportDir, Interval string }

// newInstallCmd installs the bounded systemd health check and read-access policy.
func newInstallCmd() *cobra.Command {
	var username, interval, dir string
	var dryRun bool
	cmd := &cobra.Command{Use: "install", Short: "install the Diagnos health check and permissions", RunE: func(cmd *cobra.Command, _ []string) error {
		binary, err := os.Executable()
		if err != nil {
			return err
		}
		data := installData{username, binary, dir, interval}
		service, err := renderInstallTemplate("systemd/diagnos-check.service.tmpl", data)
		if err != nil {
			return err
		}
		timer, err := renderInstallTemplate("systemd/diagnos-check.timer.tmpl", data)
		if err != nil {
			return err
		}
		sudoers, err := renderInstallTemplate("sudoers.tmpl", data)
		if err != nil {
			return err
		}
		if dryRun {
			fmt.Fprintln(cmd.OutOrStdout(), service)
			fmt.Fprintln(cmd.OutOrStdout(), timer)
			fmt.Fprintln(cmd.OutOrStdout(), sudoers)
			return nil
		}
		if os.Geteuid() != 0 {
			return fmt.Errorf("install requires root; use --dry-run to preview")
		}
		if err := ensureInstallUser(username); err != nil {
			return err
		}
		if err := os.MkdirAll(dir, 0750); err != nil {
			return err
		}
		files := map[string][]byte{"/etc/systemd/system/diagnos-check.service": []byte(service), "/etc/systemd/system/diagnos-check.timer": []byte(timer), "/etc/sudoers.d/diagnos-read": []byte(sudoers)}
		for path, data := range files {
			mode := os.FileMode(0644)
			if strings.HasSuffix(path, "diagnos-read") {
				mode = 0440
			}
			if err := os.WriteFile(path, data, mode); err != nil {
				return err
			}
		}
		if err := runInstallCommand("systemctl", "daemon-reload"); err != nil {
			return err
		}
		return runInstallCommand("systemctl", "enable", "--now", "diagnos-check.timer")
	}}
	cmd.Flags().StringVar(&username, "user", "diagnos", "dedicated OS user")
	cmd.Flags().StringVar(&interval, "interval", "10m", "health-check interval")
	cmd.Flags().StringVar(&dir, "dir", "/var/lib/diagnos/reports", "report directory")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print generated files without changing the host")
	return cmd
}

func renderInstallTemplate(name string, data installData) (string, error) {
	raw, err := resources.Files.ReadFile(name)
	if err != nil {
		return "", err
	}
	t, err := template.New(name).Parse(string(raw))
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	if err := t.Execute(&out, data); err != nil {
		return "", err
	}
	return out.String(), nil
}
func ensureInstallUser(name string) error {
	if _, err := user.Lookup(name); err == nil {
		return nil
	}
	if _, err := exec.LookPath("useradd"); err != nil {
		return err
	}
	return runInstallCommand("useradd", "--system", "--no-create-home", "--shell", "/usr/sbin/nologin", name)
}
func runInstallCommand(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}
