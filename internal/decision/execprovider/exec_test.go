package execprovider

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/faizahmd2/pinproc/internal/decision"
	"github.com/faizahmd2/pinproc/internal/provider"
)

func TestProviderProtocolRoundTrip(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "provider")
	content := []byte(`#!/bin/sh
set -eu
request="$(cat)"
request_id="$(printf '%s' "$request" | sed -n 's/.*"request_id":"\([^"]*\)".*/\1/p')"
printf '{"protocol_version":1,"request_id":"%s","answers":{"route":{"type":"choice","choice":"cpu","confidence":0.9}}}
' "$request_id"
`)
	if err := os.WriteFile(script, content, 0755); err != nil {
		t.Fatal(err)
	}
	m := provider.Manifest{
		ProtocolVersion: provider.ProtocolVersion,
		ID: "example",
		Name: "Example",
		Executable: filepath.Base(script),
	}
	p := NewWithBinaryDir(m, map[string]any{"token": "secret"}, dir)
	answers, err := p.Ask(context.Background(), map[string]any{"host": "test"}, map[string]decision.Question{
		"route": {Type: decision.QChoice, Instructions: "choose", Criteria: map[string]string{"cpu": "cpu"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if answers["route"].Choice != "cpu" {
		t.Fatalf("unexpected answers: %#v", answers)
	}
}
