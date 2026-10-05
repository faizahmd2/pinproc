package rules

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/faizahmd2/pinproc/internal/decision"
)

// Provider is the deterministic no-AI decision implementation.
//
// Its job is to reproduce, without a model, the one decision that actually
// matters for a useful report: descend from the machine down to the process
// that owns the constrained resource, then stop. It never has to pick which
// process — the engine ranks and offers capabilities — it only picks the
// capability that advances toward ownership for the strongest dimension, and
// decides when enough has been collected.
type Provider struct{}

// New returns a deterministic provider.
func New() *Provider { return &Provider{} }

// Name returns the provider name.
func (*Provider) Name() string { return "rules" }

type stateView struct {
	Signals []struct {
		Dimension string `json:"Dimension"`
		Severity  int    `json:"Severity"`
		Force     bool   `json:"Force"`
	} `json:"signals"`
	Evidence []struct {
		Capability string `json:"capability"`
		Level      int    `json:"level"`
		Entity     struct {
			Kind string `json:"kind"`
			ID   string `json:"id"`
		} `json:"entity"`
	} `json:"evidence"`
	Focus string `json:"focus"`
}

// Ask makes bounded decisions from typed state.
func (*Provider) Ask(_ context.Context, state any, questions map[string]decision.Question) (map[string]decision.Answer, error) {
	var m stateView
	b, _ := json.Marshal(state)
	_ = json.Unmarshal(b, &m)

	strongest := strongestDimension(m)
	if strongest == "" {
		// No pressure signal fired; steer by the operator's requested dimension.
		strongest = m.Focus
	}
	// Have we already attributed the anomaly to an owning entity (a process,
	// thread or cgroup), rather than only to the machine as a whole?
	owned := hasOwnerEvidence(m)

	out := map[string]decision.Answer{}
	for key, q := range questions {
		switch key {
		case "primary_dimension":
			choice := strongest
			if choice == "" {
				choice = "none"
			}
			if _, ok := q.Criteria[choice]; !ok {
				choice = "none"
			}
			out[key] = decision.Answer{Type: q.Type, Choice: choice, Confidence: 0.8}
		case "next_capability":
			out[key] = decision.Answer{Type: q.Type, Choice: pickCapability(strongest, q.Criteria), Confidence: 0.75}
		case "explains_anomaly":
			// Only call the anomaly explained once we have named the owning
			// entity; until then, keep descending.
			n := 0.2
			if owned {
				n = 0.85
			}
			out[key] = decision.Answer{Type: q.Type, Noul: n, Confidence: 0.6}
		case "deeper_warranted":
			// Deeper inspection is warranted until an owner is identified; after
			// that the deterministic path stops (threads/mechanism are left to
			// an AI provider for richer reports).
			n := 0.8
			if owned {
				n = 0.2
			}
			out[key] = decision.Answer{Type: q.Type, Noul: n, Confidence: 0.8}
		case "severity":
			out[key] = decision.Answer{Type: q.Type, Score: float64(maxSeverity(m)), Confidence: 0.7}
		default:
			// Dimension-constrained style noul questions: answer from signals.
			out[key] = decision.Answer{Type: q.Type, Noul: dimensionNoul(key, strongest), Confidence: 0.6}
		}
	}
	return out, nil
}

func strongestDimension(m stateView) string {
	strongest := ""
	best := -1
	for _, s := range m.Signals {
		if s.Severity > best {
			best = s.Severity
			strongest = s.Dimension
		}
	}
	return strongest
}

// hasOwnerEvidence reports whether any collected evidence is attributed to a
// non-machine entity (the owner we descend toward).
func hasOwnerEvidence(m stateView) bool {
	for _, e := range m.Evidence {
		if e.Entity.ID != "" && e.Entity.ID != "machine" && e.Entity.Kind != "machine" {
			return true
		}
		if e.Level >= 2 && strings.HasPrefix(e.Capability, "process.") {
			return true
		}
	}
	return false
}

// pickCapability chooses the legal capability that best advances toward the
// owning process for the strongest dimension. Criteria keys are capability ids.
func pickCapability(strongest string, criteria map[string]string) string {
	keys := make([]string, 0, len(criteria))
	for k := range criteria {
		if k != "stop" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	pick := func(prefixes ...string) string {
		for _, want := range prefixes {
			for _, k := range keys {
				if k == want || strings.HasPrefix(k, want) {
					return k
				}
			}
		}
		return ""
	}
	// Preference order per dimension: reach the process that owns the resource,
	// then its mechanism; fall back to the generic process attribution.
	var order []string
	switch strongest {
	case "memory":
		order = []string{"process.memory", "machine.processes", "process.cpu"}
	case "io":
		order = []string{"process.io", "machine.processes", "process.cpu"}
	case "network":
		order = []string{"process.sockets", "process.io", "machine.processes", "process.cpu"}
	case "filesystem":
		order = []string{"fs.usage", "process.io", "machine.processes"}
	case "scheduling":
		order = []string{"process.cpu", "thread.scheduler", "machine.processes"}
	case "limits":
		order = []string{"process.limits", "process.files", "machine.processes", "process.cpu"}
	default: // cpu and anything else
		order = []string{"process.cpu", "machine.processes", "thread.cpu"}
	}
	if c := pick(order...); c != "" {
		return c
	}
	// Otherwise take any concrete descent step before giving up.
	if c := pick("process.", "fs.usage", "cgroup.", "machine.processes"); c != "" {
		return c
	}
	return "stop"
}

func dimensionNoul(key, strongest string) float64 {
	if strings.HasPrefix(key, strongest) {
		return 0.8
	}
	return 0.2
}

func maxSeverity(m stateView) int {
	best := 0
	for _, s := range m.Signals {
		if s.Severity > best {
			best = s.Severity
		}
	}
	return best
}
