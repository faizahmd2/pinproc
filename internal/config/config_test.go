package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAppYAMLPlaceholderDoesNotBecomeAIKey(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "app.yaml")
	data := []byte("decision:\n  provider: jev\n  api_key: "<replace-with-ai-key>"\n")
	if err := os.WriteFile(p, data, 0600); err != nil { t.Fatal(err) }
	cfg, err := Load(p)
	if err != nil { t.Fatal(err) }
	if cfg.Decision.Provider != "jev" { t.Fatalf("provider = %q", cfg.Decision.Provider) }
	if cfg.Decision.APIKey != "" { t.Fatalf("placeholder leaked into API key: %q", cfg.Decision.APIKey) }
}
