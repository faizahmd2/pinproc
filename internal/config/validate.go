package config

import (
	"fmt"
	"time"
)

func Validate(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}
	if cfg.Engine.Budget != "fast" && cfg.Engine.Budget != "normal" && cfg.Engine.Budget != "deep" {
		return fmt.Errorf("engine.budget must be fast, normal, or deep")
	}
	if cfg.Engine.ParallelWidth < 1 {
		cfg.Engine.ParallelWidth = 3
	}
	if cfg.Report.MaxFindings < 1 {
		cfg.Report.MaxFindings = 5
	}
	if cfg.Source.ReadTimeout <= 0 {
		cfg.Source.ReadTimeout = 2 * time.Second
	}
	if cfg.Decision.Provider != "jev" && cfg.Decision.Provider != "rules" {
		return fmt.Errorf("decision.provider must be jev or rules")
	}
	return nil
}
