package cgroup

import (
	"strconv"
	"strings"

	"github.com/faizahmd2/pinproc/internal/capability/spec"
	"github.com/faizahmd2/pinproc/internal/contract"
	"github.com/faizahmd2/pinproc/internal/source"
)

type CPUFacts struct {
	UsageUsec     uint64
	NrPeriods     uint64
	NrThrottled   uint64
	ThrottledUsec uint64
	QuotaUsec     int64
	PeriodUsec    uint64
}

type MemoryFacts struct {
	Current uint64
	Max     string
	Events  map[string]uint64
}

type IOFacts struct {
	ReadBytes  uint64
	WriteBytes uint64
}

func CPU() spec.Capability {
	return spec.Capability{
		ID: "cgroup.cpu", Dimension: contract.DimensionCPU, Level: contract.L2Owner,
		Kind: contract.KindSampled, Accepts: contract.EntityCgroup, Cost: contract.CostLow,
		Summary: "cgroup CPU usage and throttling",
		LeadsTo: nil,
		Reads: func(e contract.Entity, _ contract.Facts) []source.Read {
			p := safe(e.ID)
			return []source.Read{
				{Key: "cgroup.cpu.stat", Path: "/sys/fs/cgroup/" + p + "/cpu.stat", Kind: source.ReadFile, Optional: true},
				{Key: "cgroup.cpu.max", Path: "/sys/fs/cgroup/" + p + "/cpu.max", Kind: source.ReadFile, Optional: true},
			}
		},
		Parse: parseCPU,
	}
}

func Memory() spec.Capability {
	return spec.Capability{
		ID: "cgroup.memory", Dimension: contract.DimensionMemory, Level: contract.L2Owner,
		Kind: contract.KindSnapshot, Accepts: contract.EntityCgroup, Cost: contract.CostLow,
		Summary: "cgroup current/max memory and OOM events",
		Reads: func(e contract.Entity, _ contract.Facts) []source.Read {
			p := safe(e.ID)
			return []source.Read{
				{Key: "cgroup.memory.current", Path: "/sys/fs/cgroup/" + p + "/memory.current", Kind: source.ReadFile, Optional: true},
				{Key: "cgroup.memory.max", Path: "/sys/fs/cgroup/" + p + "/memory.max", Kind: source.ReadFile, Optional: true},
				{Key: "cgroup.memory.events", Path: "/sys/fs/cgroup/" + p + "/memory.events", Kind: source.ReadFile, Optional: true},
			}
		},
		Parse: parseMemory,
	}
}

func IO() spec.Capability {
	return spec.Capability{
		ID: "cgroup.io", Dimension: contract.DimensionIO, Level: contract.L2Owner,
		Kind: contract.KindSampled, Accepts: contract.EntityCgroup, Cost: contract.CostLow,
		Summary: "cgroup I/O bytes from io.stat",
		Reads: func(e contract.Entity, _ contract.Facts) []source.Read {
			p := safe(e.ID)
			return []source.Read{{Key: "cgroup.io.stat", Path: "/sys/fs/cgroup/" + p + "/io.stat", Kind: source.ReadFile, Optional: true}}
		},
		Parse: parseIO,
	}
}

func safe(id string) string {
	p := strings.TrimPrefix(id, "cgroup:")
	p = strings.TrimPrefix(p, "/")
	p = strings.TrimSpace(p)
	if p == "" || strings.Contains(p, "..") || strings.HasPrefix(p, ".") {
		return "invalid"
	}
	return p
}

func parseKV(data []byte) map[string]uint64 {
	out := map[string]uint64{}
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 {
			v, _ := strconv.ParseUint(f[1], 10, 64)
			out[f[0]] = v
		}
	}
	return out
}

func parseCPU(in spec.ParseInput) (contract.Evidence, error) {
	stats0 := parseKV(rawAt(in.Sample.T0, "cgroup.cpu.stat"))
	stats := parseKV(raw(in.Sample.T1, "cgroup.cpu.stat"))
	max := strings.Fields(string(raw(in.Sample.T1, "cgroup.cpu.max")))
	f := CPUFacts{UsageUsec: stats["usage_usec"], NrPeriods: stats["nr_periods"], NrThrottled: stats["nr_throttled"], ThrottledUsec: stats["throttled_usec"]}
	if len(max) >= 2 {
		if max[0] != "max" {
			f.QuotaUsec, _ = strconv.ParseInt(max[0], 10, 64)
		}
		f.PeriodUsec, _ = strconv.ParseUint(max[1], 10, 64)
	}
	throttledPct := 0.0
	if periods := du(stats0["nr_periods"], stats["nr_periods"]); periods > 0 {
		throttledPct = float64(du(stats0["nr_throttled"], stats["nr_throttled"])) / float64(periods) * 100
	}
	return contract.Evidence{
		ID: "ev-" + strings.ReplaceAll(in.Scope.ID, ":", "-") + "-cpu", Capability: "cgroup.cpu", Entity: in.Scope,
		Dimension: contract.DimensionCPU, Level: contract.L2Owner, CollectedAt: in.Sample.T1.At,
		Facts: f, Observations: []contract.Observation{
			{Key: "cgroup.cpu.usage_usec", Value: float64(f.UsageUsec), Unit: "usec"},
			{Key: "cgroup.cpu.nr_throttled", Value: float64(f.NrThrottled), Unit: "count"},
			{Key: "cgroup.cpu.throttled_usec", Value: float64(f.ThrottledUsec), Unit: "usec"},
			{Key: "cgroup.throttled_pct", Value: throttledPct, Unit: "percent"},
		},
		Sources: []string{"/sys/fs/cgroup/<scope>/cpu.stat", "/sys/fs/cgroup/<scope>/cpu.max"},
		Verify:  []string{"cat /sys/fs/cgroup/<scope>/cpu.stat", "cat /sys/fs/cgroup/<scope>/cpu.max"},
	}, nil
}

func parseMemory(in spec.ParseInput) (contract.Evidence, error) {
	cur := parseNumber(raw(in.Sample.T1, "cgroup.memory.current"))
	max := strings.TrimSpace(string(raw(in.Sample.T1, "cgroup.memory.max")))
	events := parseKV(raw(in.Sample.T1, "cgroup.memory.events"))
	f := MemoryFacts{Current: cur, Max: max, Events: events}
	obs := []contract.Observation{{Key: "cgroup.memory.current", Value: float64(cur), Unit: "bytes"}}
	if v, ok := events["oom_kill"]; ok {
		obs = append(obs, contract.Observation{Key: "cgroup.memory.oom_kill", Value: float64(v), Unit: "count"})
	}
	return contract.Evidence{
		ID: "ev-" + strings.ReplaceAll(in.Scope.ID, ":", "-") + "-memory", Capability: "cgroup.memory", Entity: in.Scope,
		Dimension: contract.DimensionMemory, Level: contract.L2Owner, CollectedAt: in.Sample.T1.At,
		Facts: f, Observations: obs,
		Sources: []string{"/sys/fs/cgroup/<scope>/memory.current", "/sys/fs/cgroup/<scope>/memory.max", "/sys/fs/cgroup/<scope>/memory.events"},
		Verify:  []string{"cat /sys/fs/cgroup/<scope>/memory.current", "cat /sys/fs/cgroup/<scope>/memory.events"},
	}, nil
}

func parseIO(in spec.ParseInput) (contract.Evidence, error) {
	var f IOFacts
	for _, line := range strings.Split(string(raw(in.Sample.T1, "cgroup.io.stat")), "\n") {
		for _, field := range strings.Fields(line)[1:] {
			p := strings.SplitN(field, "=", 2)
			if len(p) != 2 {
				continue
			}
			v, _ := strconv.ParseUint(p[1], 10, 64)
			switch p[0] {
			case "rbytes":
				f.ReadBytes += v
			case "wbytes":
				f.WriteBytes += v
			}
		}
	}
	return contract.Evidence{
		ID: "ev-" + strings.ReplaceAll(in.Scope.ID, ":", "-") + "-io", Capability: "cgroup.io", Entity: in.Scope,
		Dimension: contract.DimensionIO, Level: contract.L2Owner, CollectedAt: in.Sample.T1.At,
		Facts: f, Observations: []contract.Observation{{Key: "cgroup.io.read_bytes", Value: float64(f.ReadBytes), Unit: "bytes"}, {Key: "cgroup.io.write_bytes", Value: float64(f.WriteBytes), Unit: "bytes"}},
		Sources: []string{"/sys/fs/cgroup/<scope>/io.stat"}, Verify: []string{"cat /sys/fs/cgroup/<scope>/io.stat"},
	}, nil
}

func du(a, b uint64) uint64 {
	if b >= a {
		return b - a
	}
	return 0
}
func rawAt(s source.Snapshot, k string) []byte {
	r := s.Reads[k]
	if len(r) == 0 {
		return nil
	}
	return r[0].Data
}
func raw(s source.Snapshot, k string) []byte {
	r := s.Reads[k]
	if len(r) == 0 {
		return nil
	}
	return r[0].Data
}
func parseNumber(b []byte) uint64 {
	v, _ := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
	return v
}
