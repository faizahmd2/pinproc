package provider

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateManifest(t *testing.T) {
	m := Manifest{ProtocolVersion: ProtocolVersion, ID: "test-provider", Name: "Test Provider", Executable: "/usr/libexec/pinproc/providers/test", Settings: []Setting{{Name: "api_key", Type: "secret", Required: true}}}
	if err := Validate(m); err != nil { t.Fatal(err) }
}

func TestDiscoverSkipsInvalidManifest(t *testing.T) {
	dir := t.TempDir()
	good := "protocol_version: 1\nid: good\nname: Good\nexecutable: /usr/bin/good\n"
	bad := "protocol_version: 99\nid: bad\nname: Bad\nexecutable: /usr/bin/bad\n"
	if err := os.WriteFile(filepath.Join(dir, "good.yaml"), []byte(good), 0600); err != nil { t.Fatal(err) }
	if err := os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte(bad), 0600); err != nil { t.Fatal(err) }
	got, err := Discover(dir); if err != nil { t.Fatal(err) }
	if len(got) != 1 || got[0].ID != "good" { t.Fatalf("got %#v", got) }
}