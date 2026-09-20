package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/faizahmd2/vm-native-diagnos/internal/config"
	"github.com/spf13/cobra"
)

const serviceUnitPath = "/etc/systemd/system/vm-native-diagnos.service"
const serviceConfigPath = "/etc/vm-native-diagnos/app.yaml"
const serviceBinaryPath = "/usr/local/bin/vm-native-diagnos"

func newServiceCmd() *cobra.Command {
	run := newServeCmd()
	run.Use = "run"
	run.Short = "run the persistent inspection service"

	cmd := &cobra.Command{Use:"service", Short:"manage the persistent vm-native-diagnos service"}
	cmd.AddCommand(run, newServiceInstallCmd(), newServiceUninstallCmd())
	return cmd
}

func newServiceInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:"install", Short:"install and start the systemd service",
		RunE: func(cmd *cobra.Command, args []string) error {
			if runtime.GOOS != "linux" { return fmt.Errorf("service install is supported on Linux only") }
			if os.Geteuid() != 0 { return fmt.Errorf("run as root: sudo %s service install", os.Args[0]) }
			if _, err := exec.LookPath("systemctl"); err != nil { return fmt.Errorf("systemctl not found; this installer requires systemd") }
			cfgFile := cfgPath
			if cfgFile == "" { cfgFile = config.DiscoverPath() }
			if cfgFile == "" { return fmt.Errorf("app.yaml not found; place app.yaml in the current directory or /etc/vm-native-diagnos/app.yaml first") }
			cfg, err := config.Load(cfgFile); if err != nil { return fmt.Errorf("load config: %w", err) }

			serviceUser, serviceGroup, err := resolveServiceAccount(cfg.Service.User, cfg.Service.Group)
			if err != nil { return err }
			uid, gid, err := lookupIDs(serviceUser, serviceGroup)
			if err != nil { return err }

			dataDir := strings.TrimSpace(cfg.Service.DataDirectory)
			if dataDir == "" { dataDir = "/var/lib/vm-native-diagnos" }
			if err := os.MkdirAll(dataDir, 0750); err != nil { return fmt.Errorf("create data directory: %w", err) }
			if err := os.Chown(dataDir, uid, gid); err != nil { return fmt.Errorf("own data directory: %w", err) }

			outputDir, err := resolveServiceOutput(cfg.Output.Directory, serviceUser)
			if err != nil { return err }
			if err := os.MkdirAll(outputDir, 0750); err != nil { return fmt.Errorf("create report directory: %w", err) }
			if err := os.Chown(outputDir, uid, gid); err != nil { return fmt.Errorf("own report directory: %w", err) }

			if err := installConfig(cfgFile, serviceUser, serviceGroup); err != nil { return err }
			exe, err := os.Executable(); if err != nil { return err }
			if resolved, err := filepath.EvalSymlinks(exe); err == nil { exe = resolved }
			if err := installServiceBinary(exe); err != nil { return err }
			if err := os.WriteFile(serviceUnitPath, []byte(renderServiceUnit(serviceBinaryPath, serviceUser, serviceGroup, dataDir)), 0644); err != nil { return fmt.Errorf("write systemd unit: %w", err) }
			if err := runSystemctl("daemon-reload"); err != nil { return err }
			if err := runSystemctl("enable", "vm-native-diagnos.service"); err != nil { return err }
			if err := runSystemctl("restart", "vm-native-diagnos.service"); err != nil { return err }
			fmt.Fprintf(cmd.OutOrStdout(), "vm-native-diagnos service installed and started as %s:%s\n", serviceUser, serviceGroup)
			fmt.Fprintln(cmd.OutOrStdout(), "logs: sudo journalctl -u vm-native-diagnos -f")
			return nil
		},
	}
}

func newServiceUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:"uninstall", Short:"stop and remove the systemd service",
		RunE: func(cmd *cobra.Command, args []string) error {
			if runtime.GOOS != "linux" { return fmt.Errorf("service uninstall is supported on Linux only") }
			if os.Geteuid() != 0 { return fmt.Errorf("run as root: sudo %s service uninstall", os.Args[0]) }
			_ = runSystemctl("disable", "--now", "vm-native-diagnos.service")
			if err := os.Remove(serviceUnitPath); err != nil && !os.IsNotExist(err) { return err }
			_ = runSystemctl("daemon-reload")
			fmt.Fprintln(cmd.OutOrStdout(), "vm-native-diagnos service removed; config and reports were preserved")
			return nil
		},
	}
}

func resolveServiceAccount(configUser, configGroup string) (string, string, error) {
	u := strings.TrimSpace(configUser)
	if u == "" { u = strings.TrimSpace(os.Getenv("SUDO_USER")) }
	if u == "" {
		cur, err := user.Current(); if err != nil { return "","",err }
		u = cur.Username
	}
	if u == "" { return "","",fmt.Errorf("could not determine service user") }

	if _, err := user.Lookup(u); err != nil {
		if strings.TrimSpace(configUser) == "" { return "","",fmt.Errorf("service user %q does not exist",u) }
		if err := ensureServiceUser(u, configGroup); err != nil { return "","",err }
	}
	g := strings.TrimSpace(configGroup)
	if g == "" {
		usr, err := user.Lookup(u); if err != nil { return "","",err }
		grp, err := user.LookupGroupId(usr.Gid); if err != nil { return "","",err }
		g = grp.Name
	}
	if _, err := user.LookupGroup(g); err != nil { return "","",fmt.Errorf("service group %q does not exist",g) }
	return u,g,nil
}

func ensureServiceUser(name, group string) error {
	if group != "" {
		if _, err := user.LookupGroup(group); err != nil {
			if err := exec.Command("groupadd","--system",group).Run(); err != nil { return fmt.Errorf("create service group %q: %w",group,err) }
		}
	}
	args := []string{"--system","--home-dir","/var/lib/vm-native-diagnos","--no-create-home","--shell","/usr/sbin/nologin"}
	if group != "" { args = append(args,"--gid",group) }
	args = append(args,name)
	if err := exec.Command("useradd",args...).Run(); err != nil { return fmt.Errorf("create service user %q: %w",name,err) }
	return nil
}

func lookupIDs(serviceUser, serviceGroup string) (int,int,error) {
	u, err := user.Lookup(serviceUser); if err != nil { return 0,0,err }
	g, err := user.LookupGroup(serviceGroup); if err != nil { return 0,0,err }
	uid, err := strconv.Atoi(u.Uid); if err != nil { return 0,0,err }
	gid, err := strconv.Atoi(g.Gid); if err != nil { return 0,0,err }
	return uid,gid,nil
}

func resolveServiceOutput(path, serviceUser string) (string,error) {
	path = strings.TrimSpace(path)
	if path == "" { return "/var/lib/vm-native-diagnos/reports",nil }
	if path == "~" || strings.HasPrefix(path,"~/") {
		u, err := user.Lookup(serviceUser); if err != nil { return "",err }
		return filepath.Join(u.HomeDir,strings.TrimPrefix(path,"~/")),nil
	}
	return filepath.Clean(path),nil
}

func installConfig(sourcePath, serviceUser, serviceGroup string) error {
	data, err := os.ReadFile(sourcePath); if err != nil { return fmt.Errorf("read config %s: %w",sourcePath,err) }
	if err := os.MkdirAll(filepath.Dir(serviceConfigPath),0755); err != nil { return err }
	if err := os.WriteFile(serviceConfigPath,data,0640); err != nil { return fmt.Errorf("write %s: %w",serviceConfigPath,err) }
	u, err := user.Lookup(serviceUser); if err != nil { return err }
	g, err := user.LookupGroup(serviceGroup); if err != nil { return err }
	uid,_ := strconv.Atoi(u.Uid); gid,_ := strconv.Atoi(g.Gid)
	if err := os.Chown(serviceConfigPath,uid,gid); err != nil { return err }
	return nil
}

func installServiceBinary(source string) error {
	info, err := os.Stat(source)
	if err != nil { return fmt.Errorf("service executable %q is unavailable: %w", source, err) }
	if !info.Mode().IsRegular() { return fmt.Errorf("service executable %q is not a regular file", source) }
	if info.Mode().Perm()&0111 == 0 { return fmt.Errorf("service executable %q is not executable", source) }
	if err := os.MkdirAll(filepath.Dir(serviceBinaryPath), 0755); err != nil { return fmt.Errorf("create service binary directory: %w", err) }

	data, err := os.ReadFile(source)
	if err != nil { return fmt.Errorf("read service executable %q: %w", source, err) }
	tmp, err := os.CreateTemp(filepath.Dir(serviceBinaryPath), ".vm-native-diagnos-*")
	if err != nil { return fmt.Errorf("stage service executable: %w", err) }
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0755); err != nil { _ = tmp.Close(); return fmt.Errorf("chmod staged service executable: %w", err) }
	if err := tmp.Chown(0, 0); err != nil { _ = tmp.Close(); return fmt.Errorf("chown staged service executable: %w", err) }
	if _, err := tmp.Write(data); err != nil { _ = tmp.Close(); return fmt.Errorf("write staged service executable: %w", err) }
	if err := tmp.Sync(); err != nil { _ = tmp.Close(); return fmt.Errorf("sync staged service executable: %w", err) }
	if err := tmp.Close(); err != nil { return fmt.Errorf("close staged service executable: %w", err) }
	if err := os.Rename(tmpName, serviceBinaryPath); err != nil { return fmt.Errorf("install service executable at %s: %w", serviceBinaryPath, err) }
	return nil
}
func renderServiceUnit(exe, serviceUser, serviceGroup, dataDir string) string {
	return fmt.Sprintf("[Unit]\nDescription=vm-native-diagnos Linux inspection service\nAfter=local-fs.target network-online.target\nWants=network-online.target\n\n[Service]\nType=simple\nExecStart=%s service run --config %s\nWorkingDirectory=%s\nUser=%s\nGroup=%s\nRestart=on-failure\nRestartSec=2s\nTimeoutStopSec=30s\nKillSignal=SIGTERM\nEnvironment=HOME=%s\n\nCapabilityBoundingSet=CAP_DAC_READ_SEARCH CAP_SYS_PTRACE CAP_SYSLOG\nAmbientCapabilities=CAP_DAC_READ_SEARCH CAP_SYS_PTRACE CAP_SYSLOG\n\nProtectSystem=strict\nProtectHome=true\nProtectKernelModules=true\nProtectKernelTunables=true\nProtectControlGroups=true\nPrivateTmp=true\nReadWritePaths=%s\nRestrictNamespaces=true\nRestrictRealtime=true\nLockPersonality=true\n\n[Install]\nWantedBy=multi-user.target\n", exe, serviceConfigPath, dataDir, serviceUser, serviceGroup, dataDir, dataDir)
}

func runSystemctl(args ...string) error {
	out, err := exec.Command("systemctl",args...).CombinedOutput()
	if err != nil { return fmt.Errorf("systemctl %s failed: %s",strings.Join(args," "),strings.TrimSpace(string(out))) }
	return nil
}
