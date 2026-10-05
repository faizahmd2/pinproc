package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/faizahmd2/pinproc/internal/config"
	dprovider "github.com/faizahmd2/pinproc/internal/provider"
	"github.com/spf13/cobra"
)

type checkState int

const (
	statusOK checkState = iota
	statusWarn
	statusFail
	statusUnknown
)

func (s checkState) label() string {
	switch s {
	case statusOK:
		return "ok  "
	case statusWarn:
		return "warn"
	case statusFail:
		return "FAIL"
	default:
		return "?   "
	}
}

type checkResult struct {
	name   string
	state  checkState
	detail string
}

// newDoctorCmd adds a read-only self-check. If it reports no FAIL, an
// investigation has everything it needs to run and attribute without error.
func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "check that pinproc has everything it needs to run",
		RunE: func(cmd *cobra.Command, args []string) error {
			results := runDoctor()
			worst := statusOK
			for _, r := range results {
				fmt.Fprintf(cmd.OutOrStdout(), "[%s] %s", r.state.label(), r.name)
				if r.detail != "" {
					fmt.Fprintf(cmd.OutOrStdout(), " — %s", r.detail)
				}
				fmt.Fprintln(cmd.OutOrStdout())
				if r.state == statusFail {
					worst = statusFail
				} else if r.state == statusWarn && worst != statusFail {
					worst = statusWarn
				}
			}
			fmt.Fprintln(cmd.OutOrStdout())
			switch worst {
			case statusFail:
				fmt.Fprintln(cmd.OutOrStdout(), "pinproc is not healthy — resolve the FAIL items above.")
				os.Exit(1)
			case statusWarn:
				fmt.Fprintln(cmd.OutOrStdout(), "pinproc can run; some optional features are degraded (see warnings).")
			default:
				fmt.Fprintln(cmd.OutOrStdout(), "pinproc is healthy.")
			}
			return nil
		},
	}
}

func runDoctor() []checkResult {
	var out []checkResult
	add := func(name string, state checkState, detail string) {
		out = append(out, checkResult{name, state, detail})
	}

	// Kernel sources: without these nothing works.
	if f, err := os.Open("/proc/stat"); err == nil {
		_ = f.Close()
		add("procfs readable (/proc)", statusOK, "")
	} else {
		add("procfs readable (/proc)", statusFail, err.Error())
	}
	if _, err := os.Stat("/sys/fs/cgroup"); err == nil {
		add("cgroup filesystem (/sys/fs/cgroup)", statusOK, "")
	} else {
		add("cgroup filesystem (/sys/fs/cgroup)", statusWarn, "cgroup evidence will be unavailable")
	}

	// Managed configuration.
	if err := ensureManagedConfigReadable(); err != nil {
		add("managed config", statusWarn, err.Error())
	} else if _, err := config.Load(cfgPath); err != nil {
		add("managed config parses", statusFail, err.Error())
	} else {
		add("managed config parses", statusOK, config.ConfigPath)
	}

	cfg, cfgErr := config.Load(cfgPath)
	if cfgErr != nil {
		cfg = nil
	}

	// Data directory writable (reports). Only meaningful for the service user.
	dir := config.DataDirectory
	if err := tryWritable(dir); err == nil {
		add("data dir writable", statusOK, dir)
	} else if os.IsPermission(err) {
		add("data dir writable", statusUnknown, "not writable by this user; the service runs as "+config.ServiceUser)
	} else {
		add("data dir writable", statusFail, err.Error())
	}

	// Privileged read capability: reading another user's /proc/<pid>/io needs
	// CAP_DAC_READ_SEARCH. If this fails, process I/O attribution degrades.
	if canReadForeignProcIO() {
		add("privileged proc reads (CAP_DAC_READ_SEARCH)", statusOK, "")
	} else {
		add("privileged proc reads (CAP_DAC_READ_SEARCH)", statusWarn, "per-process I/O for other users may be unavailable; run as the pinproc service")
	}

	// Kernel log for OOM evidence.
	if canReadKmsg() {
		add("kernel log readable (/dev/kmsg)", statusOK, "")
	} else {
		add("kernel log readable (/dev/kmsg)", statusWarn, "OOM-kill evidence will be unavailable; needs CAP_SYSLOG or root")
	}

	// AI provider, if configured.
	if cfg != nil && cfg.AI.Provider != "" {
		if m, err := dprovider.LoadInstalled(cfg.AI.Provider); err != nil {
			add("AI provider "+cfg.AI.Provider, statusWarn, "configured but unavailable ("+err.Error()+"); using deterministic rules")
		} else {
			add("AI provider "+m.ID, statusOK, m.Executable)
		}
	} else {
		add("AI provider", statusOK, "not configured; deterministic rules")
	}

	// Service reachability over the configured listen address.
	listen := "127.0.0.1:8080"
	if cfg != nil && cfg.Server.Listen != "" {
		listen = cfg.Server.Listen
	}
	if reachHealth(listen) {
		add("service reachable", statusOK, "http://"+listen+"/health")
	} else {
		add("service reachable", statusWarn, "no response on "+listen+" — is pinproc.service running?")
	}

	return out
}

func tryWritable(dir string) error {
	f, err := os.CreateTemp(dir, ".pinproc-doctor-*")
	if err != nil {
		return err
	}
	name := f.Name()
	_ = f.Close()
	return os.Remove(name)
}

func canReadForeignProcIO() bool {
	// pid 1 is owned by root; reading its io needs the read capability unless we
	// are root ourselves.
	f, err := os.Open(filepath.Join("/proc", "1", "io"))
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

func canReadKmsg() bool {
	f, err := os.OpenFile("/dev/kmsg", os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

func reachHealth(listen string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + listen + "/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
