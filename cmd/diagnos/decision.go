package main

import (
	"fmt"
	"github.com/faizahmd2/pinproc/internal/config"
	"github.com/faizahmd2/pinproc/internal/decision"
	"github.com/faizahmd2/pinproc/internal/decision/jev"
	drules "github.com/faizahmd2/pinproc/internal/decision/rules"
)

// makeDecisionProvider builds an AI-service-neutral decision provider.
func makeDecisionProvider(cfg *config.Config, noAI bool) (decision.Provider, error) {
	if noAI || cfg == nil || cfg.Decision.Provider == "rules" {
		return drules.New(), nil
	}
	switch cfg.Decision.Provider {
	case "jev":
		key := cfg.Decision.APIKey
		if key == "" { return drules.New(), nil }
		return jev.New(cfg.Decision.BaseURL, cfg.Decision.Model, key, cfg.Decision.Timeout), nil
	default:
		return nil, fmt.Errorf("decision provider %q is not available in this milestone", cfg.Decision.Provider)
	}
}

func decisionNotice(cfg *config.Config, noAI bool) string {
	if noAI || cfg == nil || cfg.Decision.Provider != "jev" { return "" }
	if cfg.Decision.APIKey == "" { return "AI decision provider unavailable — decision.api_key is not configured in app.yaml. Using deterministic rules." }
	return ""
}
