package engine

import (
	"github.com/faizahmd2/pinproc/internal/contract"
	"strings"
	"testing"
)

func TestMarshalStateBounded(t *testing.T) {
	inv := &contract.Investigation{Facts: contract.Facts{Kernel: "test"}, Evidence: make([]contract.Evidence, 0)}
	if _, e := MarshalState(inv, nil, nil); e != nil {
		t.Fatal(e)
	}
}

func TestMarshalStateCompactsDeepFacts(t *testing.T) {
	inv := &contract.Investigation{Machine: contract.MachineIdentity{Hostname: "host"}, Evidence: []contract.Evidence{{ID: "deep", Capability: "process.memory_maps", Facts: strings.Repeat("x", 20000)}}}
	b, err := MarshalState(inv, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) > 8192 {
		t.Fatalf("state too large: %d", len(b))
	}
	if !strings.Contains(string(b), "truncated") {
		t.Fatal("expected explicit truncation marker")
	}
}
