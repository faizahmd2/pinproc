package config

import (
	"fmt"
	"strings"
)

func Validate(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}

	if strings.TrimSpace(cfg.AI.Model) != "" {
		if _, ok := cfg.AI.Models[cfg.AI.Model]; !ok {
			return fmt.Errorf(
				"AI model %q is not configured under ai.models",
				cfg.AI.Model,
			)
		}
	}

	if cfg.AI.RequestLimits.MaxLinesContextFile <= 0 {
		return fmt.Errorf("ai.request_limits.max_lines_context_file must be greater than 0")
	}
	if cfg.Engine.Budget != "fast" && cfg.Engine.Budget != "normal" && cfg.Engine.Budget != "deep" {
		return fmt.Errorf("engine.budget must be fast, normal, or deep")
	}
	if cfg.Engine.ParallelWidth < 1 {
		cfg.Engine.ParallelWidth = 3
	}
	if cfg.Decision.Provider != "jev" && cfg.Decision.Provider != "llm" && cfg.Decision.Provider != "rules" {
		return fmt.Errorf("decision.provider must be jev, llm, or rules")
	}
	if cfg.Output.ReportType != "app-metrics" && cfg.Output.ReportType != "with-ai" {
		return fmt.Errorf("output.report_type must be app-metrics or with-ai")
	}
	if cfg.Output.ReportType == "with-ai" && strings.TrimSpace(cfg.AI.Provider) != "" && strings.TrimSpace(cfg.AI.APIKey) == "" {
		return fmt.Errorf("DIAGNOS_AI_API_KEY is required when output.report_type is with-ai")
	}

	return nil
}
