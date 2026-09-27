package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultsToManagedConfigWithoutAI(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil { t.Fatal(err) }
	if cfg.AI.Enabled { t.Fatal("AI must be disabled by default") }
	if cfg.Server.Listen != "127.0.0.1:8080" { t.Fatalf("listen=%q", cfg.Server.Listen) }
	if cfg.AI.Provider != "" { t.Fatalf("provider=%q", cfg.AI.Provider) }
}

func TestSaveAndLoadManagedShape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := defaults()
	cfg.AI.Enabled = true
	cfg.AI.Provider = "example"
	cfg.AI.Config = map[string]any{"token": "secret", "model": "test"}
	if err := Save(path, &cfg); err != nil { t.Fatal(err) }
	info, err := os.Stat(path)
	if err != nil { t.Fatal(err) }
	if info.Mode().Perm() != 0640 { t.Fatalf("mode=%o", info.Mode().Perm()) }
	got, err := Load(path)
	if err != nil { t.Fatal(err) }
	if !got.AI.Enabled || got.AI.Provider != "example" || got.AI.Config["model"] != "test" { t.Fatalf("unexpected config: %#v", got.AI) }
}

func TestNonLoopbackNeedsAPIKey(t *testing.T) {
	cfg := defaults()
	cfg.Server.Listen = "0.0.0.0:8080"
	if err := Validate(&cfg); err == nil { t.Fatal("expected listener authentication error") }
}
