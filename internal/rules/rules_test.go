package rules

import (
	"github.com/faizahmd2/vm-native-diagnos/internal/contract"
	"testing"
)

func TestCPUForcing(t *testing.T) {
	ev := []contract.Evidence{{ID: "e1", Observations: []contract.Observation{{Key: "cpu.iowait_pct", Value: 42}}}}
	s := Default()
	out := EvalAll(s, ev)
	found := false
	for _, x := range out {
		if x.ID == "cpu.iowait_dominant" && x.Force && x.Dimension == contract.DimensionIO {
			found = true
		}
	}
	if !found {
		t.Fatal("expected forced io branch")
	}
}
