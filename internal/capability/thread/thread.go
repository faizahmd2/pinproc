package thread

import (
	"github.com/faizahmd2/pinproc/internal/capability/spec"
	"github.com/faizahmd2/pinproc/internal/contract"
	"github.com/faizahmd2/pinproc/internal/procfs"
	"github.com/faizahmd2/pinproc/internal/source"
	"path/filepath"
	"strconv"
	"strings"
)

// CPUFacts is one thread's CPU evidence.
type CPUFacts struct {
	PID, TID                       int64
	CPUPercent, UserPct, SystemPct float64
	State                          string
}

// CPU returns thread-level CPU attribution.
func CPU() spec.Capability {
	return spec.Capability{ID: "thread.cpu", Dimension: contract.DimensionCPU, Level: contract.L3Execution, Kind: contract.KindSampled, Accepts: contract.EntityThread, Cost: contract.CostMedium, Summary: "per-thread CPU consumption and state", LeadsTo: nil, Reads: func(e contract.Entity, _ contract.Facts) []source.Read {
		pid := strings.TrimPrefix(e.ParentID, "pid:")
		tid := strings.TrimPrefix(e.ID, "tid:")
		return []source.Read{{Key: "thread.stat", Path: "/proc/" + pid + "/task/" + tid + "/stat", Kind: source.ReadFile}}
	}, Parse: parse}
}
func parse(in spec.ParseInput) (contract.Evidence, error) {
	a, e := procfs.ParsePidStat(raw(in.Sample.T0, "thread.stat"))
	if e != nil {
		return contract.Evidence{}, e
	}
	b, e := procfs.ParsePidStat(raw(in.Sample.T1, "thread.stat"))
	if e != nil {
		return contract.Evidence{}, e
	}
	sec := in.Window.Seconds()
	if sec <= 0 {
		sec = 1
	}
	cpu := float64(du(a.Utime+a.Stime, b.Utime+b.Stime)) / float64(procfs.ClockTicks()) / sec
	user := float64(du(a.Utime, b.Utime)) / float64(procfs.ClockTicks()) / sec
	sys := float64(du(a.Stime, b.Stime)) / float64(procfs.ClockTicks()) / sec
	id := "tid:" + strconv.FormatInt(b.PID, 10)
	pid := int64(0)
	if in.Scope.ParentID != "" {
		pid, _ = strconv.ParseInt(strings.TrimPrefix(in.Scope.ParentID, "pid:"), 10, 64)
	}
	return contract.Evidence{ID: "ev-thread-" + strconv.FormatInt(b.PID, 10) + "-cpu", Capability: "thread.cpu", Entity: contract.Entity{Kind: contract.EntityThread, ID: id, ParentID: in.Scope.ParentID}, Dimension: contract.DimensionCPU, Level: contract.L3Execution, CollectedAt: in.Sample.T1.At, Window: in.Window, Observations: []contract.Observation{{Key: "thread.cpu_pct", Value: cpu, Unit: "percent"}, {Key: "thread.user_pct", Value: user, Unit: "percent"}, {Key: "thread.system_pct", Value: sys, Unit: "percent"}}, Facts: CPUFacts{PID: pid, TID: b.PID, CPUPercent: cpu, UserPct: user, SystemPct: sys, State: string(b.State)}, Sources: []string{filepath.ToSlash("/proc/" + strings.TrimPrefix(in.Scope.ParentID, "pid:") + "/task/" + strings.TrimPrefix(id, "tid:") + "/stat")}, Verify: []string{filepath.ToSlash("cat /proc/" + strings.TrimPrefix(in.Scope.ParentID, "pid:") + "/task/" + strings.TrimPrefix(id, "tid:") + "/stat")}}, nil
}
func raw(s source.Snapshot, k string) []byte {
	r := s.Reads[k]
	if len(r) == 0 {
		return nil
	}
	return r[0].Data
}
func du(a, b uint64) uint64 {
	if b >= a {
		return b - a
	}
	return 0
}
