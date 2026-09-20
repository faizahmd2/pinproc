package source

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestSnapshotBoundsBlockedOpen(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "blocked")
	if err := syscall.Mkfifo(fifo, 0600); err != nil { t.Fatal(err) }

	s := NewLocalWithTimeout("/proc", "/sys", 4096, 50*time.Millisecond)
	start := time.Now()
	snap, err := s.Snapshot(context.Background(), []Read{{Key:"blocked", Path:fifo, Kind:ReadFile}})
	elapsed := time.Since(start)
	if err != nil && elapsed < 500*time.Millisecond {
		t.Fatalf("snapshot returned unexpected error too quickly: %v", err)
	}
	if len(snap.Reads["blocked"]) != 1 {
		t.Fatalf("expected one bounded result, got %#v", snap.Reads)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("blocked read exceeded timeout bound: %s", elapsed)
	}
	_ = os.Remove(fifo)
}

type contextShim struct{}
func (contextShim) Deadline() (time.Time, bool) { return time.Time{}, false }
func (contextShim) Done() <-chan struct{} { return nil }
func (contextShim) Err() error { return nil }
func (contextShim) Value(any) any { return nil }