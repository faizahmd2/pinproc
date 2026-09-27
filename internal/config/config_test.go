package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadUsesProviderNeutralDefaults(t *testing.T) {
	dir := t.TempDir(); p := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(p, []byte("version: 1\nai:\n  provider: \"\"\n  config: {}\n"), 0600); err != nil { t.Fatal(err) }
	cfg, err := Load(p); if err != nil { t.Fatal(err) }
	if cfg.AI.Provider != "" { t.Fatalf("provider = %q", cfg.AI.Provider) }
	if cfg.Server.Listen != "127.0.0.1:8080" { t.Fatalf("listen = %q", cfg.Server.Listen) }
}

func TestSaveRoundTrip(t *testing.T) {
	dir := t.TempDir(); p := filepath.Join(dir, "config.yaml")
	cfg := defaults(); cfg.AI.Provider = "jev"; cfg.AI.Config["api_key"] = "secret"
	if err := Save(p, &cfg); err != nil { t.Fatal(err) }
	got, err := Load(p); if err != nil { t.Fatal(err) }
	if got.AI.Provider != "jev" || got.AI.Config["api_key"] != "secret" { t.Fatalf("round trip = %#v", got.AI) }
}