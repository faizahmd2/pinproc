package config

import "fmt"

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
	if cfg.Decision.Provider != "jev" && cfg.Decision.Provider != "rules" {
		return fmt.Errorf("decision.provider must be jev or rules")
	}
	return nil
}
