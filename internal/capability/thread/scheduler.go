package thread

import (
	"strconv"
	"strings"

	"github.com/faizahmd2/vm-native-diagnos/internal/capability/spec"
	"github.com/faizahmd2/vm-native-diagnos/internal/contract"
	"github.com/faizahmd2/vm-native-diagnos/internal/procfs"
	"github.com/faizahmd2/vm-native-diagnos/internal/source"
)

// SchedulerFacts contains thread scheduler evidence.
type SchedulerFacts struct {
	PID, TID              int64
	RunqueueWaitMS        float64
	Timeslices            uint64
	VoluntaryCtxSwitch    uint64
	NonvoluntaryCtxSwitch uint64
	State                 string
}

// Scheduler returns thread-level scheduling evidence.
func Scheduler() spec.Capability {
	return spec.Capability{
		ID: "thread.scheduler", Dimension: contract.DimensionScheduling, Level: contract.L4Mechanism,
		Kind: contract.KindSnapshot, Accepts: contract.EntityThread, Cost: contract.CostMedium,
		Summary: "thread run-queue wait, timeslices and context switching", LeadsTo: nil,
		Reads: func(e contract.Entity, _ contract.Facts) []source.Read {
			pid := strings.TrimPrefix(e.ParentID, "pid:")
			tid := strings.TrimPrefix(e.ID, "tid:")
			return []source.Read{{Key: "thread.schedstat", Path: "/proc/" + pid + "/task/" + tid + "/schedstat", Kind: source.ReadFile, Optional: true}, {Key: "thread.status", Path: "/proc/" + pid + "/task/" + tid + "/status", Kind: source.ReadFile, Optional: true}, {Key: "thread.stat", Path: "/proc/" + pid + "/task/" + tid + "/stat", Kind: source.ReadFile, Optional: true}}
		},
		Parse: parseScheduler,
	}
}

func parseScheduler(in spec.ParseInput) (contract.Evidence, error) {
	tid := strings.TrimPrefix(in.Scope.ID, "tid:")
	pid := strings.TrimPrefix(in.Scope.ParentID, "pid:")
	f := SchedulerFacts{PID: parseInt(pid), TID: parseInt(tid)}
	if b := schedRaw(in.Sample.T1, "thread.schedstat"); len(b) > 0 {
		if s, e := procfs.ParseSchedStat(b); e == nil {
			f.RunqueueWaitMS = float64(s.RunqueueWaitNS) / 1e6
			f.Timeslices = s.Timeslices
		}
	}
	if b := schedRaw(in.Sample.T1, "thread.status"); len(b) > 0 {
		if s, e := procfs.ParsePidStatus(b); e == nil {
			f.VoluntaryCtxSwitch = s.VoluntaryCtxSwitches
			f.NonvoluntaryCtxSwitch = s.NonvoluntaryCtxSwitches
		}
	}
	if b := schedRaw(in.Sample.T1, "thread.stat"); len(b) > 0 {
		if s, e := procfs.ParsePidStat(b); e == nil {
			f.State = string(s.State)
		}
	}
	return contract.Evidence{ID: "ev-thread-" + tid + "-scheduler", Capability: "thread.scheduler", Entity: in.Scope, Dimension: contract.DimensionScheduling, Level: contract.L4Mechanism, CollectedAt: in.Sample.T1.At, Facts: f, Observations: []contract.Observation{{Key: "thread.runqueue_wait_ms", Value: f.RunqueueWaitMS, Unit: "ms"}, {Key: "thread.timeslices", Value: float64(f.Timeslices), Unit: "count"}, {Key: "thread.vol_ctxsw", Value: float64(f.VoluntaryCtxSwitch), Unit: "count"}, {Key: "thread.invol_ctxsw", Value: float64(f.NonvoluntaryCtxSwitch), Unit: "count"}}, Sources: []string{"/proc/" + pid + "/task/" + tid + "/schedstat", "/proc/" + pid + "/task/" + tid + "/status", "/proc/" + pid + "/task/" + tid + "/stat"}, Verify: []string{"cat /proc/" + pid + "/task/" + tid + "/schedstat", "cat /proc/" + pid + "/task/" + tid + "/status"}}, nil
}
func schedRaw(s source.Snapshot, k string) []byte {
	r := s.Reads[k]
	if len(r) == 0 {
		return nil
	}
	return r[0].Data
}
func parseInt(s string) int64 { v, _ := strconv.ParseInt(s, 10, 64); return v }
