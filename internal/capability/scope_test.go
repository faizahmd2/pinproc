package capability

import (
	"github.com/faizahmd2/pinproc/internal/contract"
	"github.com/faizahmd2/pinproc/internal/source"
	"testing"
)

func TestExactScopeValidation(t *testing.T) {
	r := NewRegistry()
	c := Capability{ID: "process.cpu", Level: contract.L2Owner, Accepts: contract.EntityProcess, Reads: func(contract.Entity, contract.Facts) []source.Read { return nil }, Parse: func(ParseInput) (contract.Evidence, error) { return contract.Evidence{}, nil }}
	_ = r.Register(c)
	inv := &contract.Investigation{}
	inv.AddObservedEntity(contract.Entity{Kind: contract.EntityProcess, ID: "pid:123"})
	if err := ValidateScope(c, contract.Entity{Kind: contract.EntityProcess, ID: "pid:123"}, inv); err != nil {
		t.Fatal(err)
	}
	if err := ValidateScope(c, contract.Entity{Kind: contract.EntityProcess, ID: "pid:1234"}, inv); err == nil {
		t.Fatal("substring scope must not pass")
	}
}
