package config

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

func Validate(cfg *Config) error {
	if cfg == nil { return fmt.Errorf("config is nil") }
	if cfg.Version == 0 { cfg.Version = 1 }
	if cfg.Source.ReadTimeout <= 0 { cfg.Source.ReadTimeout = 2 * time.Second }
	if cfg.Report.MaxFindings < 1 { cfg.Report.MaxFindings = 5 }
	if cfg.AI.Provider != "" && !ValidProviderID(cfg.AI.Provider) { return fmt.Errorf("ai.provider is invalid") }
	cfg.Server.Listen = strings.TrimSpace(cfg.Server.Listen)
	if cfg.Server.Listen == "" { cfg.Server.Listen = "127.0.0.1:8080" }
	if cfg.Callback.Timeout <= 0 { cfg.Callback.Timeout = 5 * time.Second }
	if cfg.Callback.Timeout > 30*time.Second { cfg.Callback.Timeout = 30 * time.Second }
	if cfg.Callback.Enabled { u, err := url.Parse(cfg.Callback.URL); if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") { return fmt.Errorf("callback.url must be an absolute http or https URL") } }
	return nil
}