package identity

import (
	"context"
	"github.com/faizahmd2/vm-native-diagnos/internal/source"
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
