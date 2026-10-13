package config

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

func Validate(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}
	if cfg.Version == 0 {
		cfg.Version = 1
	}
	if cfg.Source.ReadTimeout <= 0 {
		cfg.Source.ReadTimeout = 2 * time.Second
	}
	if cfg.Report.MaxFindings < 1 {
		cfg.Report.MaxFindings = 5
	}
	if cfg.Callback.Timeout <= 0 {
		cfg.Callback.Timeout = 5 * time.Second
	}
	if cfg.Callback.Timeout > 30*time.Second {
		cfg.Callback.Timeout = 30 * time.Second
	}
	switch strings.ToUpper(strings.TrimSpace(cfg.Callback.Method)) {
	case "":
		cfg.Callback.Method = "POST"
	case "POST", "GET":
		cfg.Callback.Method = strings.ToUpper(strings.TrimSpace(cfg.Callback.Method))
	default:
		return fmt.Errorf("callback.method must be POST or GET")
	}
	if cfg.Callback.Enabled {
		u, err := url.Parse(cfg.Callback.URL)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return fmt.Errorf("callback.url must be an absolute http or https URL")
		}
	}
	return nil
}
