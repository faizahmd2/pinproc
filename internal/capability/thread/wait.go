package thread

import (
	"strconv"
	"strings"

	"github.com/faizahmd2/vm-native-diagnos/internal/capability/spec"
	"github.com/faizahmd2/vm-native-diagnos/internal/contract"
	"github.com/faizahmd2/vm-native-diagnos/internal/source"
)

// WaitFacts summarizes bounded wait-point and thread-state evidence.
type WaitFacts struct {
	PID     int64
	Threads int
	States  map[string]int
	Wchan   map[string]int
}

// Wait returns a read-only scheduler wait-point snapshot.
func Wait() spec.Capability {
	return spec.Capability{ID: "thread.wait", Dimension: contract.DimensionScheduling, Level: contract.L4Mechanism, Kind: contract.KindSnapshot, Accepts: contract.EntityProcess, Cost: contract.CostHigh, Summary: "thread state and kernel wait-point inventory",
		Reads: func(e contract.Entity, _ contract.Facts) []source.Read {
			p := strings.TrimPrefix(e.ID, "pid:")
			return []source.Read{{Key: "thread.wchan", Path: "/proc/" + p + "/task/*/wchan", Kind: source.ReadGlob, Optional: true}, {Key: "thread.stat", Path: "/proc/" + p + "/task/*/stat", Kind: source.ReadGlob, Optional: true}}
		},
		Parse: parseWait}
}

func parseWait(in spec.ParseInput) (contract.Evidence, error) {
	pid, _ := strconv.ParseInt(strings.TrimPrefix(in.Scope.ID, "pid:"), 10, 64)
	states := map[string]int{}
	wchan := map[string]int{}
	for _, r := range in.Sample.T1.Reads["thread.stat"] {
		f := strings.Fields(string(r.Data))
		if len(f) > 2 {
			states[f[2]]++
		}
	}
	for _, r := range in.Sample.T1.Reads["thread.wchan"] {
		w := strings.TrimSpace(string(r.Data))
		if w != "" {
			wchan[w]++
		}
	}
	f := WaitFacts{PID: pid, Threads: len(in.Sample.T1.Reads["thread.stat"]), States: states, Wchan: wchan}
	return contract.Evidence{ID: "ev-process-" + strconv.FormatInt(pid, 10) + "-wait", Capability: "thread.wait", Entity: in.Scope, Dimension: contract.DimensionScheduling, Level: contract.L4Mechanism, CollectedAt: in.Sample.T1.At, Facts: f, Observations: []contract.Observation{{Key: "thread.wait_threads", Value: float64(f.Threads), Unit: "count"}}, Sources: []string{"/proc/" + strconv.FormatInt(pid, 10) + "/task/*/wchan", "/proc/" + strconv.FormatInt(pid, 10) + "/task/*/stat"}, Verify: []string{"cat /proc/" + strconv.FormatInt(pid, 10) + "/task/*/wchan", "grep State /proc/" + strconv.FormatInt(pid, 10) + "/task/*/status"}}, nil
}
