package spec

import (
	"github.com/faizahmd2/vm-native-diagnos/internal/contract"
	"github.com/faizahmd2/vm-native-diagnos/internal/source"
	"time"
)

// Capability is a safe, typed investigation capability.
type Capability struct {
	ID        string
	Dimension contract.Dimension
	Level     contract.Level
	Kind      contract.CapabilityKind
	Accepts   contract.EntityKind
	Requires  []string
	Cost      contract.Cost
	Summary   string
	LeadsTo   []string
	Reads     func(contract.Entity, contract.Facts) []source.Read
	Parse     func(ParseInput) (contract.Evidence, error)
}

// ParseInput is the typed input supplied to a capability parser.
type ParseInput struct {
	Scope  contract.Entity
	Facts  contract.Facts
	Sample source.Sample
	Window time.Duration
	Prior  []contract.Evidence
}
