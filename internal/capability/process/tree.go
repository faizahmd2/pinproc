package process

import (
	"strconv"

	"github.com/faizahmd2/pinproc/internal/capability/spec"
	"github.com/faizahmd2/pinproc/internal/contract"
	"github.com/faizahmd2/pinproc/internal/procfs"
	"github.com/faizahmd2/pinproc/internal/source"
)

// TreeFacts describes bounded process parent/child relationships.
type TreeFacts struct {
	PID         int64
	PPID        int64
	Descendants []int64
	Zombies     int
}

// Tree returns a bounded process-tree view rooted at the selected process.
func Tree() spec.Capability {
	return spec.Capability{ID: "process.tree", Dimension: contract.DimensionScheduling, Level: contract.L3Execution, Kind: contract.KindSnapshot, Accepts: contract.EntityProcess, Cost: contract.CostMedium, Summary: "bounded process parent/child tree and zombie detection",
		Reads: func(_ contract.Entity, _ contract.Facts) []source.Read {
			return []source.Read{{Key: "proc.all_stat", Path: "/proc/[0-9]*/stat", Kind: source.ReadGlob, MaxBytes: 4096, Optional: true}}
		},
		Parse: parseTree}
}

func parseTree(in spec.ParseInput) (contract.Evidence, error) {
	pid, _ := strconv.ParseInt(depthEntityPID(in.Scope), 10, 64)
	parents := map[int64]int64{}
	states := map[int64]byte{}
	for _, r := range in.Sample.T1.Reads["proc.all_stat"] {
		s, e := procfs.ParsePidStat(r.Data)
		if e != nil {
			continue
		}
		parents[s.PID] = s.PPID
		states[s.PID] = byte(s.State)
	}
	desc := []int64{}
	queue := []int64{pid}
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		for child, pp := range parents {
			if pp == p {
				desc = append(desc, child)
				if len(desc) < 256 {
					queue = append(queue, child)
				}
			}
		}
	}
	z := 0
	for _, x := range desc {
		if states[x] == 'Z' {
			z++
		}
	}
	f := TreeFacts{PID: pid, PPID: parents[pid], Descendants: desc, Zombies: z}
	return contract.Evidence{ID: "ev-process-" + strconv.FormatInt(pid, 10) + "-tree", Capability: "process.tree", Entity: contract.Entity{Kind: contract.EntityProcess, ID: "pid:" + strconv.FormatInt(pid, 10)}, Dimension: contract.DimensionScheduling, Level: contract.L3Execution, CollectedAt: in.Sample.T1.At, Facts: f, Observations: []contract.Observation{{Key: "proc.descendants", Value: float64(len(desc)), Unit: "count"}, {Key: "proc.zombie_children", Value: float64(z), Unit: "count"}}, Sources: []string{"/proc/[0-9]*/stat"}, Verify: []string{"cat /proc/*/stat"}}, nil
}
