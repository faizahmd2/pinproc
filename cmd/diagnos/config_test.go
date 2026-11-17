package main

import (
	"strings"
	"testing"
	"time"

	"github.com/faizahmd2/pinproc/internal/config"
)

func TestSetKeyTypesAndValidation(t *testing.T) {
	base := mustDefaults(t)
	next, err := setKey(base, "alerts.cooldown", "1h")
	if err != nil || next.Alerts.Cooldown != time.Hour {
		t.Fatalf("cooldown: %v %v", next, err)
	}
	next, err = setKey(base, "monitor.cpu.cap_util", "95")
	if err != nil || next.Monitor.CPU.CapUtil != 95 {
		t.Fatalf("cap_util: %v", err)
	}
	next, err = setKey(base, "email.to", "a@example.com, b@example.com")
	if err != nil || strings.Join(next.Email.To, "|") != "a@example.com|b@example.com" {
		t.Fatalf("email.to: %v %v", next.Email.To, err)
	}
	next, err = setKey(base, "email.subject", "[{tag}] {host}")
	if err != nil || next.Email.Subject != "[{tag}] {host}" {
		t.Fatalf("subject: %v %v", next.Email.Subject, err)
	}
	if _, err := setKey(base, "report.timezone", "Mars/Olympus"); err == nil {
		t.Fatal("invalid timezone accepted")
	}
	if _, err := setKey(base, "alerts.cooldown", "soon"); err == nil {
		t.Fatal("invalid duration accepted")
	}
	if _, err := setKey(base, "nope.key", "1"); err == nil || !strings.Contains(err.Error(), "unknown setting") {
		t.Fatalf("unknown key: %v", err)
	}
	if _, err := setKey(base, "monitor", "1"); err == nil {
		t.Fatal("setting a section accepted")
	}
}

func mustDefaults(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}
