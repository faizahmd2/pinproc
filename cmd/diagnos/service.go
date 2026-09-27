package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

const (
	serviceUnitPath = "/usr/lib/systemd/system/pinproc.service"
	serviceConfigPath = "/etc/pinproc/config.yaml"
	serviceBinaryPath = "/usr/bin/pinproc"
	serviceDataPath = "/var/lib/pinproc"
	serviceUser = "pinproc"
	serviceGroup = "pinproc"
)

func newServiceCmd() *cobra.Command {
	run := newServeCmd()
	run.Use = "run"
	run.Short = "run the persistent inspection service"
	cmd := &cobra.Command{
		Use: "service",
		Short: "run the persistent pinproc service",
		RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	cmd.AddCommand(run)
	return cmd
}

func renderServiceUnit(exe, serviceUser, serviceGroup, dataDir string) string {
	return fmt.Sprintf(`[Unit]
Description=pinproc Linux inspection service
After=local-fs.target network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s service run
WorkingDirectory=/
User=%s
Group=%s
Restart=on-failure
RestartSec=2s
TimeoutStopSec=30s
KillSignal=SIGTERM

CapabilityBoundingSet=CAP_DAC_READ_SEARCH CAP_SYS_PTRACE CAP_SYSLOG
AmbientCapabilities=CAP_DAC_READ_SEARCH CAP_SYS_PTRACE CAP_SYSLOG

ProtectSystem=strict
ProtectHome=true
ProtectKernelModules=true
ProtectKernelTunables=true
ProtectControlGroups=true
PrivateTmp=true
RestrictNamespaces=true
RestrictRealtime=true
LockPersonality=true
NoNewPrivileges=false
ReadWritePaths=%s

[Install]
WantedBy=multi-user.target
`, exe, serviceUser, serviceGroup, dataDir)
}