package main

import (
	"fmt"
	"strings"

	"github.com/faizahmd2/pinproc/internal/config"
	"github.com/faizahmd2/pinproc/internal/decision"
	"github.com/faizahmd2/pinproc/internal/decision/external"
	dprovider "github.com/faizahmd2/pinproc/internal/provider"
	drules "github.com/faizahmd2/pinproc/internal/decision/rules"
)

func makeDecisionProvider(cfg *config.Config) (decision.Provider, error) {
	if cfg == nil || strings.TrimSpace(cfg.AI.Provider) == "" { return drules.New(), nil }
	m, err := dprovider.LoadInstalled(cfg.AI.Provider)
	if err != nil { return drules.New(), nil }
	return external.New(m, cfg.AI.Config), nil
}

func decisionNotice(cfg *config.Config) string {
	if cfg == nil || strings.TrimSpace(cfg.AI.Provider) == "" { return "AI reasoning is not configured; pinproc is using deterministic rules. Configure an AI provider with sudo pinproc setup ai for better contextual investigation." }
	if _, err := dprovider.LoadInstalled(cfg.AI.Provider); err != nil { return fmt.Sprintf("AI provider %q is unavailable; pinproc is using deterministic rules.", cfg.AI.Provider) }
	return ""
}