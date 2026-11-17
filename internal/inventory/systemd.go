package inventory

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// systemd facts come from `systemctl show` (read-only D-Bus queries). One call
// covers every unit, bounded by a short timeout; without systemd it is skipped.

const systemctlTimeout = 2 * time.Second

func haveSystemd() bool {
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		return false
	}
	_, err := exec.LookPath("systemctl")
	return err == nil
}

func runSystemctl(ctx context.Context, args ...string) []byte {
	ctx, cancel := context.WithTimeout(ctx, systemctlTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "systemctl", append([]string{"--no-pager"}, args...)...)
	cmd.Env = []string{"LANG=C", "SYSTEMD_PAGER=cat", "SYSTEMD_COLORS=0"}
	out, err := cmd.Output()
	if err != nil && len(out) == 0 {
		return nil
	}
	return out
}

var unitProps = []string{"Id", "Description", "FragmentPath", "DropInPaths", "Type", "Restart", "NRestarts",
	"ActiveEnterTimestampMonotonic", "ActiveState", "SubState", "User", "WorkingDirectory", "Requires", "Wants",
	"BindsTo", "After", "MemoryMax", "CPUQuotaPerSecUSec", "LimitNOFILESoft"}

// unitInfo queries systemd for the given units.
func unitInfo(ctx context.Context, units []string, boot int64) map[string]*UnitInfo {
	if len(units) == 0 || !haveSystemd() {
		return nil
	}
	args := []string{"show", "-p", strings.Join(unitProps, ",")}
	args = append(args, units...)
	return parseUnitShow(runSystemctl(ctx, args...), boot)
}

func parseUnitShow(out []byte, boot int64) map[string]*UnitInfo {
	res := map[string]*UnitInfo{}
	for _, block := range bytes.Split(out, []byte("\n\n")) {
		kv := map[string]string{}
		for _, line := range strings.Split(string(block), "\n") {
			if k, v, ok := strings.Cut(line, "="); ok {
				kv[k] = strings.TrimSpace(v)
			}
		}
		id := kv["Id"]
		if id == "" {
			continue
		}
		u := &UnitInfo{
			Description: kv["Description"], File: kv["FragmentPath"], Type: kv["Type"], Restart: kv["Restart"],
			User: kv["User"], WorkingDir: kv["WorkingDirectory"],
		}
		if kv["DropInPaths"] != "" {
			u.DropIns = strings.Fields(kv["DropInPaths"])
		}
		u.Restarts, _ = strconv.Atoi(kv["NRestarts"])
		if st := kv["ActiveState"]; st != "" {
			u.State = st
			if sub := kv["SubState"]; sub != "" && sub != st {
				u.State += " (" + sub + ")"
			}
		}
		if mono, err := strconv.ParseInt(kv["ActiveEnterTimestampMonotonic"], 10, 64); err == nil && mono > 0 && boot > 0 {
			u.ActiveSince = time.Unix(boot, 0).Add(time.Duration(mono) * time.Microsecond).UTC()
		}
		if v := kv["MemoryMax"]; v != "" && v != "infinity" {
			if n, err := strconv.ParseUint(v, 10, 64); err == nil {
				u.MemoryMax = strconv.FormatFloat(float64(n)/(1<<20), 'f', 0, 64) + " MiB"
			} else {
				u.MemoryMax = v
			}
		}
		if v := kv["CPUQuotaPerSecUSec"]; v != "" && v != "infinity" {
			if d, err := time.ParseDuration(strings.ReplaceAll(v, "us", "µs")); err == nil {
				u.CPUQuota = strconv.FormatFloat(d.Seconds()*100, 'f', 0, 64) + "%"
			} else {
				u.CPUQuota = v
			}
		}
		if v := kv["LimitNOFILESoft"]; v != "" && v != "infinity" {
			u.LimitNOFILE = v
		}
		u.DependsOn = dependencies(id, kv["Requires"], kv["BindsTo"], kv["Wants"], kv["After"])
		res[id] = u
	}
	return res
}

// dependencies keeps the units a service really leans on (databases, sockets,
// mounts, other services) and drops systemd's own plumbing.
func dependencies(self string, lists ...string) []string {
	seen := map[string]bool{self: true}
	var out []string
	for _, l := range lists {
		for _, u := range strings.Fields(l) {
			if seen[u] || noiseUnit(u) {
				continue
			}
			seen[u] = true
			out = append(out, u)
		}
	}
	if len(out) > 10 {
		out = out[:10]
	}
	return out
}

func noiseUnit(u string) bool {
	switch u {
	case "network-online.target", "remote-fs.target", "nss-lookup.target", "time-sync.target", "docker.socket", "containerd.service", "docker.service":
		return false
	case "-.mount", "system.slice", "user.slice":
		return true
	}
	if strings.HasPrefix(u, "systemd-") || strings.HasPrefix(u, "sys-") || strings.HasPrefix(u, "dev-") {
		return true
	}
	return strings.HasSuffix(u, ".target") || strings.HasSuffix(u, ".slice")
}

// failedUnits lists units systemd reports as failed (systemctl --failed).
func failedUnits(ctx context.Context) ([]string, bool) {
	if !haveSystemd() {
		return nil, false
	}
	out := runSystemctl(ctx, "list-units", "--state=failed", "--no-legend", "--plain")
	if out == nil {
		return nil, true
	}
	var names []string
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) > 0 {
			names = append(names, strings.TrimPrefix(f[0], "●"))
		}
	}
	return names, true
}
