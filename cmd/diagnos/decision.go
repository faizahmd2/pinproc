package main

import (
	"fmt"
	"os"

	"github.com/faizahmd2/pinproc/internal/config"
	"github.com/faizahmd2/pinproc/internal/decision"
	"github.com/faizahmd2/pinproc/internal/decision/execprovider"
	drules "github.com/faizahmd2/pinproc/internal/decision/rules"
	"github.com/faizahmd2/pinproc/internal/provider"
)

func makeDecisionProvider(cfg *config.Config) (decision.Provider, error) {
	if cfg == nil || !cfg.AI.Enabled || cfg.AI.Provider == "" {
		return drules.New(), nil
	}
	manifest, err := provider.Find(cfg.AI.Provider)
	if err != nil || !provider.BinaryInstalled(manifest, provider.BinaryDir) {
		return drules.New(), nil
	}
	return execprovider.New(manifest, cfg.AI.Config), nil
}

func decisionNotice(cfg *config.Config) string {
	if cfg == nil || !cfg.AI.Enabled || cfg.AI.Provider == "" {
		return "AI is not configured; using deterministic rules. Run 'sudo pinproc setup ai' for better adaptive results."
	}
	manifest, err := provider.Find(cfg.AI.Provider)
	if err != nil {
		return fmt.Sprintf("Configured AI provider %q is unavailable; using deterministic rules. Install the provider or run 'sudo pinproc setup ai'.", cfg.AI.Provider)
	}
	if !provider.BinaryInstalled(manifest, provider.BinaryDir) {
		return fmt.Sprintf("Configured AI provider %q is unavailable; using deterministic rules. Reinstall the provider or run 'sudo pinproc setup ai'.", cfg.AI.Provider)
	}
	if _, err := os.Stat(manifest.BinaryPath(provider.BinaryDir)); err != nil {
		return fmt.Sprintf("Configured AI provider %q is unavailable; using deterministic rules.", cfg.AI.Provider)
	}
	return ""
}
