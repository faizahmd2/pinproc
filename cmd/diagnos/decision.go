package main

import (
	"fmt"

	"github.com/faizahmd2/pinproc/internal/config"
	"github.com/faizahmd2/pinproc/internal/decision"
	"github.com/faizahmd2/pinproc/internal/decision/jev"
	drules "github.com/faizahmd2/pinproc/internal/decision/rules"
)

// makeDecisionProvider builds an AI-service-neutral decision provider.
func makeDecisionProvider(cfg *config.Config) (decision.Provider, error) {
	if cfg == nil || cfg.Decision.Provider == "rules" {
		return drules.New(), nil
	}
	switch cfg.Decision.Provider {
	case "jev":
		if cfg.Decision.APIKey == "" {
			return drules.New(), nil
		}
		return jev.New(cfg.Decision.BaseURL, cfg.Decision.Model, cfg.Decision.APIKey, cfg.Decision.Timeout), nil
	default:
		return nil, fmt.Errorf("decision provider %q is not available", cfg.Decision.Provider)
	}
}

func decisionNotice(cfg *config.Config) string {
	if cfg == nil || cfg.Decision.Provider != "jev" || cfg.Decision.APIKey != "" {
		return ""
	}
	return "AI decision provider unavailable — decision.api_key is not configured in app.yaml. Using deterministic rules."
}
