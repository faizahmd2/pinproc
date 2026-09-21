package rules

import (
	"encoding/json"
	"fmt"
	"github.com/faizahmd2/pinproc/internal/contract"
	"strings"
)

// Signal is deterministic evidence that a branch deserves attention.
type Signal struct {
	ID        string
	Dimension contract.Dimension
	Severity  int
	Statement string
	Support   []string
	Force     bool
}

// Rule evaluates typed evidence.
type Rule interface {
	ID() string
	Eval([]contract.Evidence) (Signal, bool)
}

// EvalAll evaluates rules deterministically.
func EvalAll(rs []Rule, ev []contract.Evidence) []Signal {
	out := []Signal{}
	for _, r := range rs {
		if s, ok := r.Eval(ev); ok {
			if s.Severity < 0 {
				s.Severity = 0
			}
			if s.Severity > 4 {
				s.Severity = 4
			}
			out = append(out, s)
		}
	}
	return out
}

// Default returns the V2 deterministic rule set.
func Default() []Rule {
	return []Rule{cpuSaturated{}, cpuPSI{}, cpuIOWait{}, cpuSteal{}, memPressure{}, memSwap{}, memOOM{}, ioSaturated{}, ioBlocked{}, fdExhausted{}, pidExhausted{}, netRetransmit{}, netListenOverflow{}, cgroupThrottled{}, fsNearlyFull{}, deletedOpenLarge{}}
}

type cpuSaturated struct{}

func (cpuSaturated) ID() string { return "cpu.saturated" }
func (cpuSaturated) Eval(ev []contract.Evidence) (Signal, bool) {
	o, ok := find(ev, "cpu.utilization")
	l, ok2 := find(ev, "load.one_per_core")
	if !ok || !ok2 || o.Value <= 85 || l.Value <= 1 {
		return Signal{}, false
	}
	return Signal{"cpu.saturated", contract.DimensionCPU, 2, fmt.Sprintf("CPU is saturated at %.1f%% with load %.2fx per core", o.Value, l.Value), mergeSupport(ev, "cpu.utilization", "load.one_per_core"), true}, true
}

type cpuPSI struct{}

func (cpuPSI) ID() string { return "cpu.psi_stalled" }
func (cpuPSI) Eval(ev []contract.Evidence) (Signal, bool) {
	o, ok := find(ev, "cpu.psi_full_avg10")
	if !ok || o.Value <= 20 {
		return Signal{}, false
	}
	return Signal{"cpu.psi_stalled", contract.DimensionCPU, 3, fmt.Sprintf("CPU pressure full avg10 is %.1f%%", o.Value), support(ev, "cpu.psi_full_avg10"), true}, true
}

type cpuIOWait struct{}

func (cpuIOWait) ID() string { return "cpu.iowait_dominant" }
func (cpuIOWait) Eval(ev []contract.Evidence) (Signal, bool) {
	o, ok := find(ev, "cpu.iowait_pct")
	if !ok || o.Value <= 20 {
		return Signal{}, false
	}
	return Signal{"cpu.iowait_dominant", contract.DimensionIO, 3, fmt.Sprintf("I/O wait is %.1f%% of CPU time", o.Value), support(ev, "cpu.iowait_pct"), true}, true
}

type cpuSteal struct{}

func (cpuSteal) ID() string { return "cpu.steal" }
func (cpuSteal) Eval(ev []contract.Evidence) (Signal, bool) {
	o, ok := find(ev, "cpu.steal_pct")
	if !ok || o.Value <= 5 {
		return Signal{}, false
	}
	return Signal{"cpu.steal", contract.DimensionCPU, 2, fmt.Sprintf("CPU steal time is %.1f%%", o.Value), support(ev, "cpu.steal_pct"), false}, true
}

type memPressure struct{}

func (memPressure) ID() string { return "mem.pressure" }
func (memPressure) Eval(ev []contract.Evidence) (Signal, bool) {
	if o, ok := find(ev, "mem.psi_full_avg10"); ok && o.Value > 10 {
		return Signal{"mem.pressure", contract.DimensionMemory, 3, fmt.Sprintf("memory PSI full avg10 is %.1f%%", o.Value), support(ev, "mem.psi_full_avg10"), true}, true
	}
	if o, ok := find(ev, "mem.direct_reclaim_rate"); ok && o.Value > 0 {
		return Signal{"mem.pressure", contract.DimensionMemory, 3, fmt.Sprintf("direct reclaim rate is %.1f pages/s", o.Value), support(ev, "mem.direct_reclaim_rate"), true}, true
	}
	return Signal{}, false
}

type memSwap struct{}

func (memSwap) ID() string { return "mem.swapping" }
func (memSwap) Eval(ev []contract.Evidence) (Signal, bool) {
	o, ok := find(ev, "mem.swapin_rate")
	used, ok2 := find(ev, "mem.swap_used_pct")
	if !ok || !ok2 || o.Value <= 0 || used.Value <= 0 {
		return Signal{}, false
	}
	return Signal{"mem.swapping", contract.DimensionMemory, 3, fmt.Sprintf("swap-in is %.1f pages/s with %.1f%% swap used", o.Value, used.Value), mergeSupport(ev, "mem.swapin_rate", "mem.swap_used_pct"), true}, true
}

type memOOM struct{}

func (memOOM) ID() string { return "mem.oom_recent" }
func (memOOM) Eval(ev []contract.Evidence) (Signal, bool) {
	o, ok := find(ev, "mem.oom_kill_delta")
	if !ok || o.Value <= 0 {
		return Signal{}, false
	}
	return Signal{"mem.oom_recent", contract.DimensionMemory, 4, "one or more OOM kills occurred in the sample window", support(ev, "mem.oom_kill_delta"), true}, true
}

type ioSaturated struct{}

func (ioSaturated) ID() string { return "io.saturated" }
func (ioSaturated) Eval(ev []contract.Evidence) (Signal, bool) {
	for _, e := range ev {
		for _, o := range e.Observations {
			if strings.HasSuffix(o.Key, ".util_pct") && o.Value > 80 {
				return Signal{"io.saturated", contract.DimensionIO, 3, fmt.Sprintf("block I/O utilization is %.1f%%", o.Value), []string{e.ID}, true}, true
			}
			if strings.HasSuffix(o.Key, ".await_ms") && o.Value > 50 {
				return Signal{"io.saturated", contract.DimensionIO, 3, fmt.Sprintf("block I/O await is %.1f ms", o.Value), []string{e.ID}, true}, true
			}
		}
	}
	return Signal{}, false
}

type ioBlocked struct{}

func (ioBlocked) ID() string { return "io.blocked_procs" }
func (ioBlocked) Eval(ev []contract.Evidence) (Signal, bool) {
	for _, k := range []string{"procs.blocked", "procs.d_state"} {
		if o, ok := find(ev, k); ok && o.Value > 2 {
			return Signal{"io.blocked_procs", contract.DimensionIO, 3, fmt.Sprintf("blocked processes: %.0f", o.Value), support(ev, k), true}, true
		}
	}
	return Signal{}, false
}

type fdExhausted struct{}

func (fdExhausted) ID() string { return "limits.fd_exhaustion" }
func (fdExhausted) Eval(ev []contract.Evidence) (Signal, bool) {
	o, ok := find(ev, "limits.fd_used_pct")
	if !ok || o.Value <= 80 {
		return Signal{}, false
	}
	return Signal{"limits.fd_exhaustion", contract.DimensionLimits, 3, fmt.Sprintf("file descriptor usage is %.1f%%", o.Value), support(ev, "limits.fd_used_pct"), true}, true
}

type pidExhausted struct{}

func (pidExhausted) ID() string { return "limits.pid_exhaustion" }
func (pidExhausted) Eval(ev []contract.Evidence) (Signal, bool) {
	o, ok := find(ev, "limits.pid_used_pct")
	if !ok || o.Value <= 80 {
		return Signal{}, false
	}
	return Signal{"limits.pid_exhaustion", contract.DimensionLimits, 3, fmt.Sprintf("PID usage is %.1f%%", o.Value), support(ev, "limits.pid_used_pct"), true}, true
}
func find(ev []contract.Evidence, key string) (contract.Observation, bool) {
	for _, e := range ev {
		for _, o := range e.Observations {
			if o.Key == key {
				return o, true
			}
		}
	}
	return contract.Observation{}, false
}
func support(ev []contract.Evidence, key string) []string {
	for _, e := range ev {
		for _, o := range e.Observations {
			if o.Key == key {
				return []string{e.ID}
			}
		}
	}
	return nil
}
func mergeSupport(ev []contract.Evidence, keys ...string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, e := range ev {
		for _, o := range e.Observations {
			for _, k := range keys {
				if o.Key == k && !seen[e.ID] {
					seen[e.ID] = true
					out = append(out, e.ID)
				}
			}
		}
	}
	return out
}

type netRetransmit struct{}

func (netRetransmit) ID() string { return "net.retransmit" }
func (netRetransmit) Eval(ev []contract.Evidence) (Signal, bool) {
	o, ok := find(ev, "tcp.retrans_rate")
	if !ok || o.Value <= 5 {
		return Signal{}, false
	}
	return Signal{"net.retransmit", contract.DimensionNetwork, 2, fmt.Sprintf("TCP retransmit rate is %.1f/s", o.Value), support(ev, "tcp.retransmit"), true}, true
}

type netListenOverflow struct{}

func (netListenOverflow) ID() string { return "net.listen_overflow" }
func (netListenOverflow) Eval(ev []contract.Evidence) (Signal, bool) {
	o, ok := find(ev, "tcp.listen_overflow_delta")
	if !ok || o.Value <= 0 {
		return Signal{}, false
	}
	return Signal{"net.listen_overflow", contract.DimensionNetwork, 3, fmt.Sprintf("%.0f connection(s) dropped — a listen backlog is full", o.Value), support(ev, "tcp.listen_overflow_delta"), true}, true
}

type cgroupThrottled struct{}

func (cgroupThrottled) ID() string { return "cgroup.cpu_throttled" }
func (cgroupThrottled) Eval(ev []contract.Evidence) (Signal, bool) {
	o, ok := find(ev, "cgroup.throttled_pct")
	if !ok || o.Value <= 5 {
		return Signal{}, false
	}
	return Signal{"cgroup.cpu_throttled", contract.DimensionCPU, 3, fmt.Sprintf("cgroup is CPU-throttled %.1f%% of periods", o.Value), support(ev, "cgroup.throttled_pct"), true}, true
}

type fsNearlyFull struct{}

func (fsNearlyFull) ID() string { return "fs.nearly_full" }
func (fsNearlyFull) Eval(ev []contract.Evidence) (Signal, bool) {
	for _, e := range ev {
		if e.Capability != "machine.filesystem" {
			continue
		}
		var f struct {
			Mounts []struct {
				Path         string
				UsedPct      float64
				InodeUsedPct float64
			}
		}
		b, _ := json.Marshal(e.Facts)
		if json.Unmarshal(b, &f) != nil {
			continue
		}
		worst := ""
		pct := 0.0
		for _, m := range f.Mounts {
			if m.UsedPct > pct {
				pct = m.UsedPct
				worst = m.Path
			}
			if m.InodeUsedPct > pct {
				pct = m.InodeUsedPct
				worst = m.Path
			}
		}
		if pct > 90 {
			return Signal{"fs.nearly_full", contract.DimensionFilesystem, 3, fmt.Sprintf("%s is %.0f%% full", worst, pct), []string{e.ID}, true}, true
		}
	}
	return Signal{}, false
}

type deletedOpenLarge struct{}

func (deletedOpenLarge) ID() string { return "fs.deleted_open_large" }
func (deletedOpenLarge) Eval(ev []contract.Evidence) (Signal, bool) {
	o, ok := find(ev, "proc.fd_deleted_bytes")
	if !ok || o.Value <= 500*1024*1024 {
		return Signal{}, false
	}
	return Signal{"fs.deleted_open_large", contract.DimensionFilesystem, 3, fmt.Sprintf("process holds %.1f MB of deleted-but-open files", o.Value/(1024*1024)), support(ev, "proc.fd_deleted_bytes"), true}, true
}
