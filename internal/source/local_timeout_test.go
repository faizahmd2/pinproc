//go:build linux

package source

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestSnapshotBoundsBlockedOpen(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "blocked")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(fifo)

	s := NewLocalWithTimeout("/proc", "/sys", 4096, 50*time.Millisecond)
	start := time.Now()
	snap, err := s.Snapshot(context.Background(), []Read{{Key: "blocked", Path: fifo, Kind: ReadFile}})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("snapshot should return bounded raw failure, not abort: %v", err)
	}
	rows := snap.Reads["blocked"]
	if len(rows) != 1 {
		t.Fatalf("expected one bounded result, got %#v", rows)
	}
	if rows[0].Err == nil || !strings.Contains(rows[0].Err.Error(), "timed out") {
		t.Fatalf("expected timed-out raw result, got %#v", rows[0].Err)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("blocked read exceeded timeout bound: %s", elapsed)
	}
}
