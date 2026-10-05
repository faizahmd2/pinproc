package identity

import (
	"context"
	"github.com/faizahmd2/pinproc/internal/source"
	"os"
	"path/filepath"
	"testing"
)

// TestResolveSystemd proves cgroup-based systemd identity.
func TestResolveSystemd(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "123")
	if e := os.MkdirAll(p, 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(p, "cgroup"), []byte("0::/system.slice/payments.service\n"), 0644); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(p, "status"), []byte("Name:\tapi\nUid:\t1000 1000 1000 1000\n"), 0644); e != nil {
		t.Fatal(e)
	}
	src := source.NewLocal(root, filepath.Join(root, "sys"), 1<<20)
	svc, e := New(src).Resolve(context.Background(), "123")
	if e != nil {
		t.Fatal(e)
	}
	if svc.Name != "payments" || svc.Unit != "payments.service" {
		t.Fatalf("unexpected service %#v", svc)
	}
}

func TestRedactCmdline(t *testing.T) {
	cases := []struct{ in, want string }{
		{"--password=hunter2", "--password=***"},
		{"--db-token=sk-live-ABC", "--db-token=***"},
		{"--api-key=XYZ", "--api-key=***"},
		{"-psecretpw", "-p***"},
		{"--listen=127.0.0.1:8080", "--listen=127.0.0.1:8080"},
		{"serve", "serve"},
	}
	in := []string{"/usr/bin/app"}
	for _, c := range cases {
		in = append(in, c.in)
	}
	out := redactCmdline(in)
	if out[0] != "/usr/bin/app" {
		t.Fatalf("argv0 changed: %q", out[0])
	}
	for i, c := range cases {
		if out[i+1] != c.want {
			t.Errorf("redact %q = %q, want %q", c.in, out[i+1], c.want)
		}
	}
}

func TestRedactBareSecretFlag(t *testing.T) {
	out := redactCmdline([]string{"mysql", "--password", "topsecret", "--host", "db"})
	if out[2] != "***" {
		t.Errorf("expected bare secret value redacted, got %q", out[2])
	}
	if out[4] != "db" {
		t.Errorf("non-secret value should remain, got %q", out[4])
	}
}

func TestAllowedLogPath(t *testing.T) {
	ok := []string{"/var/log/app/app.log", "/opt/app/logs/x.log", "/home/svc/app.log"}
	bad := []string{"/var/log/../../etc/shadow", "/etc/shadow", "/root/.ssh/id_rsa", "relative/x.log"}
	for _, p := range ok {
		if !allowedLogPath(p) {
			t.Errorf("expected %q allowed", p)
		}
	}
	for _, p := range bad {
		if allowedLogPath(p) {
			t.Errorf("expected %q rejected", p)
		}
	}
}
