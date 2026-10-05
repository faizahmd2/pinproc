package contenders

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/faizahmd2/pinproc/internal/contract"
)

func mkproc(t *testing.T, root string, pid int, utime, stime, rssPages uint64) {
	t.Helper()
	dir := filepath.Join(root, itoa(pid))
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	// minimal /proc/<pid>/stat: pid (comm) state ppid ... fields; we need utime
	// (14), stime (15), rss (24). Build a 44-field line.
	f := make([]string, 52)
	for i := range f {
		f[i] = "0"
	}
	f[0], f[1], f[2] = itoa(pid), "(proc"+itoa(pid)+")", "S"
	f[13], f[14], f[23] = itoa(int(utime)), itoa(int(stime)), itoa(int(rssPages))
	line := ""
	for i, v := range f {
		if i > 0 {
			line += " "
		}
		line += v
	}
	os.WriteFile(filepath.Join(dir, "stat"), []byte(line+"\n"), 0644)
}

func TestTopMem(t *testing.T) {
	root := t.TempDir()
	mkproc(t, root, 10, 0, 0, 100)
	mkproc(t, root, 20, 0, 0, 500) // biggest
	mkproc(t, root, 30, 0, 0, 50)
	s := NewSampler(root)
	top := s.Top(contract.DimensionMemory, 2, time.Now())
	if len(top) != 2 {
		t.Fatalf("want 2, got %d", len(top))
	}
	if top[0].PID != 20 {
		t.Fatalf("top mem pid = %d, want 20", top[0].PID)
	}
}

func TestTopCPUDelta(t *testing.T) {
	root := t.TempDir()
	mkproc(t, root, 10, 100, 0, 0)
	mkproc(t, root, 20, 100, 0, 0)
	s := NewSampler(root)
	now := time.Unix(100, 0)
	if got := s.Top(contract.DimensionCPU, 5, now); len(got) != 0 {
		t.Fatalf("first tick should have no cpu values, got %d", len(got))
	}
	// pid 20 burns 100 ticks over 1s; pid 10 idle
	mkproc(t, root, 10, 100, 0, 0)
	mkproc(t, root, 20, 200, 0, 0)
	got := s.Top(contract.DimensionCPU, 5, now.Add(time.Second))
	if len(got) != 1 || got[0].PID != 20 {
		t.Fatalf("expected pid 20 as sole cpu contender, got %+v", got)
	}
	// 100 ticks / 100 Hz / 1s = 1.0 core = 100%
	if got[0].Value < 99 || got[0].Value > 101 {
		t.Fatalf("cpu%% = %v, want ~100", got[0].Value)
	}
}

func TestResetClearsDelta(t *testing.T) {
	root := t.TempDir()
	mkproc(t, root, 10, 100, 0, 0)
	s := NewSampler(root)
	s.Top(contract.DimensionCPU, 5, time.Unix(1, 0))
	s.Reset()
	if s.prevCPU != nil {
		t.Fatal("prevCPU not cleared")
	}
}
