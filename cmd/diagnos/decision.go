package main

import (
	"fmt"
	"github.com/faizahmd2/vm-native-diagnos/internal/config"
	"github.com/faizahmd2/vm-native-diagnos/internal/decision"
	"github.com/faizahmd2/vm-native-diagnos/internal/decision/jev"
	drules "github.com/faizahmd2/vm-native-diagnos/internal/decision/rules"
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
\nfunc decisionNotice(cfg *config.Config, noAI bool) string {\n\tif noAI || cfg == nil || cfg.Decision.Provider != "jev" { return "" }\n\tif cfg.Decision.APIKey == "" { return "AI decision provider unavailable — TYPESAFE_API_KEY is not set. Using deterministic rules." }\n\treturn ""\n}\n