package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/faizahmd2/diagnos/internal/config"
	"github.com/faizahmd2/diagnos/internal/source"
	"github.com/spf13/cobra"
)

type doctorCheck struct {
	Name, Status string
	Required     bool
	Detail       string
}

// newDoctorCmd verifies local read-only prerequisites and optional integrations.
func newDoctorCmd() *cobra.Command {
	var asJSON bool
	return &cobra.Command{Use: "doctor [host]", Short: "check Diagnos runtime prerequisites", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		_ = args
		checks := []doctorCheck{checkPath("/proc/stat", true), checkPath("/proc/meminfo", true), checkPath("/proc/vmstat", true), checkPath("/proc/loadavg", true), checkPath("/sys/fs/cgroup", true), checkPath("/proc/pressure/cpu", false)}
		facts, err := source.NewLocal("/proc", "/sys", 1<<20).Facts(context.Background())
		if err != nil {
			checks = append(checks, doctorCheck{"facts", "error", true, err.Error()})
		} else {
			checks = append(checks, doctorCheck{"kernel", "ok", true, facts.Kernel}, doctorCheck{"cgroup_v2", doctorStatus(facts.CgroupV2), false, ""}, doctorCheck{"psi", doctorStatus(facts.PSI), false, ""}, doctorCheck{"effective_uid", "ok", true, fmt.Sprint(os.Geteuid())})
		}
		cfg, cfgErr := config.Load(cfgPath)
		if cfgErr != nil {
			checks = append(checks, doctorCheck{"config", "warning", false, cfgErr.Error()})
		} else {
			checks = append(checks, doctorCheck{"config", "ok", false, ""})
			if cfg.Decision.Provider == "jev" && cfg.Decision.BaseURL != "" {
				checks = append(checks, networkCheck("decision", strings.TrimRight(cfg.Decision.BaseURL, "/")+"/v1/systemone"))
			}
			if cfg.Identity.DockerSocket != "" {
				checks = append(checks, unixCheck("docker", cfg.Identity.DockerSocket))
			}
		}
		checks = append(checks, capabilityCheck())
		if asJSON {
			out := make([]map[string]any, 0, len(checks))
			for _, c := range checks {
				out = append(out, map[string]any{"name": c.Name, "status": c.Status, "required": c.Required, "detail": c.Detail})
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(out)
		}
		ok := true
		for _, c := range checks {
			fmt.Fprintf(cmd.OutOrStdout(), "%-14s %-8s %s\n", c.Name, c.Status, c.Detail)
			if c.Required && c.Status != "ok" {
				ok = false
			}
		}
		if !ok {
			return fmt.Errorf("doctor found required prerequisites unavailable")
		}
		return nil
	}}
}
func checkPath(path string, required bool) doctorCheck {
	if _, err := os.Stat(path); err != nil {
		s := "warning"
		if required {
			s = "error"
		}
		return doctorCheck{path, s, required, err.Error()}
	}
	return doctorCheck{path, "ok", required, ""}
}
func doctorStatus(ok bool) string {
	if ok {
		return "ok"
	}
	return "warning"
}
func capabilityCheck() doctorCheck {
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return doctorCheck{"caps", "warning", false, err.Error()}
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "CapEff:") {
			return doctorCheck{"caps", "ok", false, strings.TrimSpace(strings.TrimPrefix(line, "CapEff:"))}
		}
	}
	return doctorCheck{"caps", "warning", false, "CapEff unavailable"}
}
func networkCheck(name, rawURL string) doctorCheck {
	client := &http.Client{Timeout: 2 * time.Second}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return doctorCheck{name, "warning", false, err.Error()}
	}
	resp, err := client.Do(req)
	if err != nil {
		return doctorCheck{name, "warning", false, err.Error()}
	}
	defer resp.Body.Close()
	return doctorCheck{name, "ok", false, resp.Status}
}
func unixCheck(name, path string) doctorCheck {
	conn, err := net.DialTimeout("unix", path, 2*time.Second)
	if err != nil {
		return doctorCheck{name, "warning", false, err.Error()}
	}
	_ = conn.Close()
	return doctorCheck{name, "ok", false, ""}
}
