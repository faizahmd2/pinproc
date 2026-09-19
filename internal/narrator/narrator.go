package narrator

import (
	"context"
	"fmt"
	"github.com/faizahmd2/vm-native-diagnos/internal/contract"
	"strings"
)

// Narrator produces developer-facing prose from the stable investigation contract.
type Narrator interface {
	Name() string
	Narrate(context.Context, *contract.Investigation) (string, error)
}

// Rules is a zero-network narrator. It never invents evidence.
type Rules struct{}

// NewRules returns the deterministic narrator.
func NewRules() *Rules { return &Rules{} }

// Name returns the narrator name.
func (*Rules) Name() string { return "rules" }

// Narrate creates a concise evidence-grounded narrative.
func (*Rules) Narrate(_ context.Context, inv *contract.Investigation) (string, error) {
	if inv == nil {
		return "", fmt.Errorf("investigation is nil")
	}
	if len(inv.Hypotheses) == 0 {
		return "No material anomaly was established from collected evidence.", nil
	}
	var b strings.Builder
	b.WriteString(inv.Hypotheses[0].Statement)
	b.WriteString("\nEvidence path: ")
	path := make([]string, 0, len(inv.Path))
	for _, step := range inv.Path {
		path = append(path, step.Capability+"("+step.Scope+")")
	}
	b.WriteString(strings.Join(path, " -> "))
	if len(inv.Limitations) > 0 {
		b.WriteString("\nLimitations: ")
		b.WriteString(strings.Join(inv.Limitations, "; "))
	}
	return b.String(), nil
}
