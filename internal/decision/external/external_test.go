package external

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/faizahmd2/pinproc/internal/decision"
	"github.com/faizahmd2/pinproc/internal/provider"
)

func TestProviderUsesStdinStdoutProtocol(t *testing.T) {
	dir := t.TempDir(); script := filepath.Join(dir, "provider.sh")
	scriptContent := "#!/bin/sh\ncat >/dev/null\nprintf '%s\\n' '{\"protocol_version\":1,\"answers\":{\"ok\":{\"type\":\"noul\",\"noul\":1}}}'\n"
	if err := os.WriteFile(script, []byte(scriptContent), 0755); err != nil { t.Fatal(err) }
	m := provider.Manifest{ProtocolVersion: 1, ID: "test", Name: "Test", Executable: script}
	p := New(m, map[string]string{"token":"secret"})
	answers, err := p.Ask(context.Background(), map[string]any{"x":1}, map[string]decision.Question{"ok": {Type: decision.QNoul}})
	if err != nil { t.Fatal(err) }
	if answers["ok"].Noul != 1 { t.Fatalf("answers=%#v", answers) }
}