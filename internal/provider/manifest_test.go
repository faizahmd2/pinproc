package provider

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListAndValidateManifests(t *testing.T) {
	dir := t.TempDir()
	data := []byte(`protocol_version: 1
id: example
name: Example
executable: pinproc-provider-example
settings:
  - key: api_key
    label: API key
    type: secret
    required: true
`)
	if err := os.WriteFile(filepath.Join(dir, "example.yaml"), data, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "example" {
		t.Fatalf("unexpected manifests: %#v", got)
	}
}
