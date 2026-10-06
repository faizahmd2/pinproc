package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(p, []byte("version: 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Source.ReadTimeout != 2*time.Second {
		t.Fatalf("read_timeout = %v", cfg.Source.ReadTimeout)
	}
	if cfg.Report.MaxFindings != 5 {
		t.Fatalf("max_findings = %d", cfg.Report.MaxFindings)
	}
	// monitor defaults are filled in by Normalize
	if cfg.Monitor.CalmCadence <= 0 {
		t.Fatalf("monitor not normalized: %+v", cfg.Monitor)
	}
}

func TestSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	cfg := defaults()
	cfg.Callback.Enabled = true
	cfg.Callback.URL = "https://example.com/pinproc"
	if err := Save(p, &cfg); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Callback.Enabled || got.Callback.URL != "https://example.com/pinproc" {
		t.Fatalf("round trip = %#v", got.Callback)
	}
}
