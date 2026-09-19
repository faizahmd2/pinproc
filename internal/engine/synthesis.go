package engine

import (
	"github.com/faizahmd2/diagnos/internal/contract"
	"github.com/faizahmd2/diagnos/internal/rules"
	"sort"
)

// Synthesize builds explicitly graded hypotheses from deterministic signals.
func Synthesize(signals []rules.Signal, ev []contract.Evidence) []contract.Hypothesis {
	out := make([]contract.Hypothesis, 0, len(signals))
	for _, s := range signals {
		entity := contract.Entity{Kind: contract.EntityMachine, ID: "machine"}
		for _, e := range ev {
			for _, id := range s.Support {
				if e.ID == id {
					entity = e.Entity
				}
			}
		}
		out = append(out, contract.Hypothesis{ID: "hy-" + s.ID, Statement: s.Statement, Dimension: s.Dimension, Entity: entity, Grade: contract.GradeObserved, Support: s.Support, Confidence: float64(s.Severity) / 4, Source: "rule:" + s.ID})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Confidence > out[j].Confidence })
	return out
}
