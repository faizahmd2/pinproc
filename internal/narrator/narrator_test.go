package narrator

import (
	"context"
	"github.com/faizahmd2/vm-native-diagnos/internal/contract"
	"strings"
	"testing"
)

func TestNarrateUsesOnlyEvidence(t *testing.T) {
	inv := &contract.Investigation{Hypotheses: []contract.Hypothesis{{Statement: "CPU saturation is observed.", Grade: contract.GradeObserved}}, Path: []contract.Step{{Capability: "machine.processes", Scope: "machine"}}}
	s, e := NewRules().Narrate(context.Background(), inv)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(s, "CPU saturation is observed.") || !strings.Contains(s, "machine.processes(machine)") {
		t.Fatalf("unexpected narrative %q", s)
	}
}
