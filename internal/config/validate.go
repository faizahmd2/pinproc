package config

import (
	"fmt"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/faizahmd2/pinproc/internal/render"
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
	switch strings.ToLower(strings.TrimSpace(cfg.Callback.Format)) {
	case "", "html":
		cfg.Callback.Format = "html"
	case "json", "text":
		cfg.Callback.Format = strings.ToLower(strings.TrimSpace(cfg.Callback.Format))
	default:
		return fmt.Errorf("callback.format must be html, json or text")
	}
	if strings.TrimSpace(cfg.Report.Timezone) == "" {
		cfg.Report.Timezone = "UTC"
	}
	if _, _, err := render.ParseZone(cfg.Report.Timezone); err != nil {
		return fmt.Errorf("report.timezone: %w", err)
	}
	if cfg.Alerts.Cooldown <= 0 {
		cfg.Alerts.Cooldown = 30 * time.Minute
	}
	if err := validateEmail(&cfg.Email); err != nil {
		return err
	}
	if cfg.Callback.Enabled {
		u, err := url.Parse(cfg.Callback.URL)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return fmt.Errorf("callback.url must be an absolute http or https URL")
		}
	}
	return nil
}

func validateEmail(e *EmailConfig) error {
	if e.Port == 0 {
		e.Port = 587
	}
	if e.Timeout <= 0 {
		e.Timeout = 15 * time.Second
	}
	if e.Timeout > 60*time.Second {
		e.Timeout = 60 * time.Second
	}
	switch strings.ToLower(strings.TrimSpace(e.Security)) {
	case "", "starttls":
		e.Security = "starttls"
	case "tls", "ssl":
		e.Security = "tls"
	case "none":
		e.Security = "none"
	default:
		return fmt.Errorf("email.security must be starttls, tls or none")
	}
	if !e.Enabled {
		return nil
	}
	if strings.TrimSpace(e.Host) == "" {
		return fmt.Errorf("email.host is required when email is enabled")
	}
	if e.Port < 1 || e.Port > 65535 {
		return fmt.Errorf("email.port must be 1-65535")
	}
	if _, err := mail.ParseAddress(e.From); err != nil {
		return fmt.Errorf("email.from: %w", err)
	}
	if len(e.To) == 0 {
		return fmt.Errorf("email.to needs at least one recipient")
	}
	for _, to := range e.To {
		if _, err := mail.ParseAddress(to); err != nil {
			return fmt.Errorf("email.to %q: %w", to, err)
		}
	}
	if e.Security == "none" && e.Username != "" && !isLocalHost(e.Host) {
		return fmt.Errorf("email.security none would send the SMTP password in clear text to %s; use starttls or tls", e.Host)
	}
	return nil
}

func isLocalHost(h string) bool {
	return h == "localhost" || h == "127.0.0.1" || h == "::1"
}
