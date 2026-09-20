package capability

import (
	"github.com/faizahmd2/pinproc/internal/contract"
	"github.com/faizahmd2/pinproc/internal/source"
	"testing"
)

func TestBuiltinGraph(t *testing.T) {
	r, e := BuildBuiltin()
	if e != nil {
		t.Fatal(e)
	}
	if len(r.ForLevel(contract.L1Machine)) < 6 {
		t.Fatalf("expected six L1 capabilities")
	}
	for _, c := range r.All() {
		for _, n := range c.LeadsTo {
			if _, ok := r.Get(n); !ok {
				t.Fatalf("%s -> missing %s", c.ID, n)
			}
		}
	}
}

func TestScopeRejectsUnknownPID(t *testing.T) {
	r := NewRegistry()
	c := Capability{ID: "process.cpu", Level: contract.L2Owner, Accepts: contract.EntityProcess, Reads: func(contract.Entity, contract.Facts) []source.Read { return nil }, Parse: func(ParseInput) (contract.Evidence, error) { return contract.Evidence{}, nil }}
	_ = r.Register(c)
	if e := ValidateScope(c, contract.Entity{Kind: contract.EntityProcess, ID: "pid:99"}, &contract.Investigation{}); e == nil {
		t.Fatal("expected unknown scope rejection")
	}
}
