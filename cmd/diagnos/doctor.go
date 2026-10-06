package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/faizahmd2/pinproc/internal/config"
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

	// Trigger mode: pinproc self-triggers by reading pressure (no socket).
	if _, err := os.Stat("/proc/pressure/cpu"); err == nil {
		add("trigger mode", statusOK, "pull (PSI + utilization)")
	} else {
		add("trigger mode", statusWarn, "PSI unavailable; using utilization/load only")
	}

	// Last captured report, if any.
	if b, err := os.Stat(filepath.Join(config.DataDirectory, "report.md")); err == nil {
		add("last report", statusOK, "captured "+b.ModTime().Format("02 Jan 15:04"))
	} else {
		add("last report", statusOK, "none yet")
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
