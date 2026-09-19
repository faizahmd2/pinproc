package rules

import (
	"context"
	"encoding/json"
	"github.com/faizahmd2/vm-native-diagnos/internal/decision"
	"sort"
	"strings"
)

// Provider is the deterministic no-AI decision implementation.
type Provider struct{}

// New returns a deterministic provider.
func New() *Provider { return &Provider{} }

// Name returns the provider name.
func (*Provider) Name() string { return "rules" }

// Ask makes bounded decisions from typed state.
func (*Provider) Ask(_ context.Context, state any, questions map[string]decision.Question) (map[string]decision.Answer, error) {
	var m struct {
		Signals []struct {
			Dimension string `json:"Dimension"`
			Severity  int    `json:"Severity"`
			Force     bool   `json:"Force"`
		} `json:"signals"`
		Evidence []struct {
			Capability string `json:"capability"`
			Level      int    `json:"level"`
		} `json:"evidence"`
	}
	b, _ := json.Marshal(state)
	_ = json.Unmarshal(b, &m)

	strongest := ""
	best := -1
	for _, s := range m.Signals {
		if s.Severity > best {
			best = s.Severity
			strongest = s.Dimension
		}
	}

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
			choice := "stop"
			keys := make([]string, 0, len(q.Criteria))
			for candidate := range q.Criteria {
				if candidate != "stop" {
					keys = append(keys, candidate)
				}
			}
			sort.Strings(keys)
			pick := func(substr string) bool {
				for _, candidate := range keys {
					if strings.Contains(candidate, substr) {
						choice = candidate
						return true
					}
				}
				return false
			}
			switch strongest {
			case "memory":
				if !pick("process.memory") {
					pick("process.cpu")
				}
			case "io":
				if !pick("process.io") {
					pick("process.cpu")
				}
			case "network":
				if !pick("process.sockets") {
					pick("process.io")
				}
			case "scheduling":
				if !pick("thread.scheduler") {
					pick("thread.cpu")
				}
			default:
				if !pick("thread.scheduler") {
					if !pick("thread.cpu") {
						pick("process.cpu")
					}
				}
			}
			out[key] = decision.Answer{Type: q.Type, Choice: choice, Confidence: 0.75}
		case "explains_anomaly":
			out[key] = decision.Answer{Type: q.Type, Noul: 0.8, Confidence: 0.6}
		case "deeper_warranted":
			n := 0.1
			if len(m.Evidence) > 0 && m.Evidence[len(m.Evidence)-1].Capability != "thread.scheduler" {
				n = 0.8
			}
			out[key] = decision.Answer{Type: q.Type, Noul: n, Confidence: 0.8}
		case "severity":
			out[key] = decision.Answer{Type: q.Type, Score: float64(maxSeverity(m.Signals)), Confidence: 0.7}
		default:
			out[key] = decision.Answer{Type: q.Type, Noul: 0.5}
		}
	}
	return out, nil
}

func maxSeverity(s []struct {
	Dimension string `json:"Dimension"`
	Severity  int    `json:"Severity"`
	Force     bool   `json:"Force"`
}) int {
	m := 0
	for _, x := range s {
		if x.Severity > m {
			m = x.Severity
		}
	}
	return m
}
