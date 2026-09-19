package engine

import (
	"encoding/json"
	"fmt"

	"github.com/faizahmd2/diagnos/internal/contract"
	"github.com/faizahmd2/diagnos/internal/rules"
)

// ModelEvidence is the bounded evidence view exposed to a decision provider.
type ModelEvidence struct {
	ID           string                 `json:"id"`
	Capability   string                 `json:"capability"`
	Entity       contract.Entity        `json:"entity"`
	Dimension    contract.Dimension     `json:"dimension"`
	Level        contract.Level         `json:"level"`
	Observations []contract.Observation `json:"observations,omitempty"`
	Facts        json.RawMessage        `json:"facts,omitempty"`
	Derived      []string               `json:"derived,omitempty"`
	Sources      []string               `json:"sources,omitempty"`
	Unavailable  string                 `json:"unavailable,omitempty"`
	Err          string                 `json:"err,omitempty"`
}

// State is the compact model-facing state.
type State struct {
	Machine  contract.MachineIdentity `json:"machine"`
	Facts    contract.Facts           `json:"facts"`
	Evidence []ModelEvidence          `json:"evidence"`
	Signals  []rules.Signal           `json:"signals"`
	Path     []contract.Step          `json:"path"`
}

// MarshalState creates a hard-bounded model-facing representation.
func MarshalState(inv *contract.Investigation, signals []rules.Signal, path []contract.Step) ([]byte, error) {
	if inv == nil {
		return nil, fmt.Errorf("investigation is nil")
	}
	for maxEvidence := 8; maxEvidence >= 0; maxEvidence-- {
		sourceEvidence := inv.Evidence
		if len(sourceEvidence) > maxEvidence {
			sourceEvidence = sourceEvidence[len(sourceEvidence)-maxEvidence:]
		}
		modelEvidence := make([]ModelEvidence, 0, len(sourceEvidence))
		for _, ev := range sourceEvidence {
			modelEvidence = append(modelEvidence, ModelEvidence{ID: ev.ID, Capability: ev.Capability, Entity: ev.Entity, Dimension: ev.Dimension, Level: ev.Level, Observations: ev.Observations, Facts: compactFacts(ev.Facts), Derived: ev.Derived, Sources: ev.Sources, Unavailable: ev.Unavailable, Err: ev.Err})
		}
		b, err := json.Marshal(State{Machine: inv.Machine, Facts: inv.Facts, Evidence: modelEvidence, Signals: signals, Path: path})
		if err != nil {
			return nil, err
		}
		if len(b) <= 8192 {
			return b, nil
		}
	}
	return nil, fmt.Errorf("model state exceeds 8KB after compaction")
}

func compactFacts(f any) json.RawMessage {
	if f == nil {
		return nil
	}
	b, err := json.Marshal(f)
	if err != nil || len(b) == 0 {
		return nil
	}
	const limit = 1800
	if len(b) <= limit {
		return b
	}
	marker, _ := json.Marshal(map[string]any{"truncated": true, "original_bytes": len(b)})
	return marker
}
