package process

import (
	"github.com/faizahmd2/pinproc/internal/capability/spec"
	"github.com/faizahmd2/pinproc/internal/contract"
	"github.com/faizahmd2/pinproc/internal/procfs"
	"github.com/faizahmd2/pinproc/internal/source"
	"path/filepath"
	"strconv"
	"strings"
)

// CPUFacts is process-level CPU evidence.
type CPUFacts struct {
	PID                                                                                                int64
	CPUPercent, UserPct, SystemPct, RunqueueWaitMS, VolCtxSwitchRate, InvolCtxSwitchRate, BlkIODelayMS float64
	Threads                                                                                            int64
	ThreadIDs                                                                                          []string
	State                                                                                              string
}

// CPU returns process CPU attribution for an observed PID.
func CPU() spec.Capability {
	return spec.Capability{ID: "process.cpu", Dimension: contract.DimensionCPU, Level: contract.L2Owner, Kind: contract.KindSampled, Accepts: contract.EntityProcess, Cost: contract.CostLow, Summary: "process CPU, scheduler wait, context switching and thread inventory", LeadsTo: []string{"thread.cpu", "thread.scheduler"}, Reads: func(e contract.Entity, _ contract.Facts) []source.Read {
		pid := entityPID(e)
		return []source.Read{{Key: "proc.stat", Path: "/proc/" + pid + "/stat", Kind: source.ReadFile}, {Key: "proc.status", Path: "/proc/" + pid + "/status", Kind: source.ReadFile}, {Key: "proc.schedstat", Path: "/proc/" + pid + "/schedstat", Kind: source.ReadFile, Optional: true}, {Key: "thread.stat", Path: "/proc/" + pid + "/task/*/stat", Kind: source.ReadGlob, Optional: true}}
	}, Parse: parse}
}
func parse(in spec.ParseInput) (contract.Evidence, error) {
	p0, e := procfs.ParsePidStat(raw(in.Sample.T0, "proc.stat"))
	if e != nil {
		return contract.Evidence{}, e
	}
	p1, e := procfs.ParsePidStat(raw(in.Sample.T1, "proc.stat"))
	if e != nil {
		return contract.Evidence{}, e
	}
	sec := in.Window.Seconds()
	if sec <= 0 {
		sec = 1
	}
	cpu := float64(du(p0.Utime+p0.Stime, p1.Utime+p1.Stime)) / float64(procfs.ClockTicks()) / sec / float64(1)
	user := float64(du(p0.Utime, p1.Utime)) / float64(procfs.ClockTicks()) / sec
	sys := float64(du(p0.Stime, p1.Stime)) / float64(procfs.ClockTicks()) / sec
	wait := 0.0
	if a := raw(in.Sample.T0, "proc.schedstat"); len(a) > 0 {
		if b := raw(in.Sample.T1, "proc.schedstat"); len(b) > 0 {
			x, _ := procfs.ParseSchedStat(a)
			y, _ := procfs.ParseSchedStat(b)
			wait = float64(du(x.RunqueueWaitNS, y.RunqueueWaitNS)) / 1e6
		}
	}
	vol, inv := 0.0, 0.0
	if a := raw(in.Sample.T1, "proc.status"); len(a) > 0 {
		if s, er := procfs.ParsePidStatus(a); er == nil {
			if o := raw(in.Sample.T0, "proc.status"); len(o) > 0 {
				if q, er2 := procfs.ParsePidStatus(o); er2 == nil {
					vol = float64(du(q.VoluntaryCtxSwitches, s.VoluntaryCtxSwitches)) / sec
					inv = float64(du(q.NonvoluntaryCtxSwitches, s.NonvoluntaryCtxSwitches)) / sec
				}
			}
		}
	}
	ids := []string{}
	for p := range index(in.Sample.T1.Reads["thread.stat"]) {
		if tid := threadID(p); tid != "" {
			ids = append(ids, tid)
		}
	}
	return contract.Evidence{ID: "ev-process-" + strconv.FormatInt(p1.PID, 10) + "-cpu", Capability: "process.cpu", Entity: contract.Entity{Kind: contract.EntityProcess, ID: "pid:" + strconv.FormatInt(p1.PID, 10)}, Dimension: contract.DimensionCPU, Level: contract.L2Owner, CollectedAt: in.Sample.T1.At, Window: in.Window, Observations: []contract.Observation{{Key: "proc.cpu_pct", Value: cpu, Unit: "percent"}, {Key: "proc.user_pct", Value: user, Unit: "percent"}, {Key: "proc.system_pct", Value: sys, Unit: "percent"}, {Key: "proc.runqueue_wait_ms", Value: wait, Unit: "ms"}, {Key: "proc.vol_ctxsw_rate", Value: vol, Unit: "count_per_sec"}, {Key: "proc.invol_ctxsw_rate", Value: inv, Unit: "count_per_sec"}, {Key: "proc.threads", Value: float64(p1.NumThreads), Unit: "count"}}, Facts: CPUGFacts{PID: p1.PID, CPUPercent: cpu, UserPct: user, SystemPct: sys, RunqueueWaitMS: wait, VolCtxSwitchRate: vol, InvolCtxSwitchRate: inv, Threads: p1.NumThreads, ThreadIDs: ids, State: string(p1.State)}, Sources: []string{"/proc/" + strconv.FormatInt(p1.PID, 10) + "/stat", "/proc/" + strconv.FormatInt(p1.PID, 10) + "/status", "/proc/" + strconv.FormatInt(p1.PID, 10) + "/task/*/stat"}, Verify: []string{"cat /proc/" + strconv.FormatInt(p1.PID, 10) + "/stat", "cat /proc/" + strconv.FormatInt(p1.PID, 10) + "/task/*/stat"}}, nil
}

type CPUGFacts = CPUFacts

func raw(s source.Snapshot, k string) []byte {
	r := s.Reads[k]
	if len(r) == 0 {
		return nil
	}
	return r[0].Data
}
func index(rs []source.Raw) map[string][]byte {
	m := map[string][]byte{}
	for _, r := range rs {
		m[r.Path] = r.Data
	}
	return m
}
func entityPID(e contract.Entity) string {
	v := strings.TrimPrefix(e.ID, "pid:")
	if _, err := strconv.Atoi(v); err != nil {
		return "0"
	}
	return v
}
func threadID(p string) string {
	a := strings.Split(filepath.ToSlash(p), "/task/")
	if len(a) != 2 {
		return ""
	}
	v := strings.Split(a[1], "/")[0]
	if _, e := strconv.Atoi(v); e != nil {
		return ""
	}
	return "tid:" + v
}
func du(a, b uint64) uint64 {
	if b >= a {
		return b - a
	}
	return 0
}
