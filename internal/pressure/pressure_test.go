package pressure

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "pressure"), 0755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"pressure/cpu":    "some avg10=12.34 avg60=5.00 avg300=1.00 total=123\n",
		"pressure/memory": "some avg10=0.00 avg60=0.00 avg300=0.00 total=1\nfull avg10=0.00 avg60=0.00 avg300=0.00 total=1\n",
		"pressure/io":     "some avg10=3.50 avg60=1.00 avg300=0.00 total=9\nfull avg10=2.00 avg60=0.00 avg300=0.00 total=8\n",
		"meminfo":         "MemTotal:        2000000 kB\nMemFree:          100000 kB\nMemAvailable:     500000 kB\nSwapTotal:       1000000 kB\nSwapFree:         750000 kB\n",
		"loadavg":         "3.20 1.10 0.40 2/400 12345\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestCalm(t *testing.T) {
	r := NewReader(writeFixture(t), 4)
	l := r.Calm()
	if l.CPUStallPct != 12.34 {
		t.Errorf("cpu stall = %v, want 12.34", l.CPUStallPct)
	}
	if l.IOStallPct != 3.50 {
		t.Errorf("io stall = %v, want 3.50", l.IOStallPct)
	}
	if l.MemStallPct != 0 {
		t.Errorf("mem stall = %v, want 0", l.MemStallPct)
	}
	// used = (2,000,000 - 500,000) / 2,000,000 = 75%
	if l.MemUsedPct != 75 {
		t.Errorf("mem used = %v, want 75", l.MemUsedPct)
	}
	// swap used = (1,000,000 - 750,000) / 1,000,000 = 25%
	if l.SwapUsedPct != 25 {
		t.Errorf("swap used = %v, want 25", l.SwapUsedPct)
	}
	// load1/core = 3.20 / 4 = 0.8
	if l.Load1PerCore != 0.8 {
		t.Errorf("load1/core = %v, want 0.8", l.Load1PerCore)
	}
}

func TestCalmMissingFilesDegrade(t *testing.T) {
	r := NewReader(t.TempDir(), 2) // empty: no files
	l := r.Calm()                  // must not panic; all zero
	if l != (Levels{}) {
		t.Fatalf("expected zero Levels, got %+v", l)
	}
}

// BenchmarkCalm shows the calm-path cost and allocations per tick.
func BenchmarkCalm(b *testing.B) {
	root := writeFixtureB(b)
	r := NewReader(root, 4)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = r.Calm()
	}
}

func writeFixtureB(b *testing.B) string {
	b.Helper()
	root := b.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "pressure"), 0755)
	files := map[string]string{
		"pressure/cpu":    "some avg10=12.34 avg60=5.00 avg300=1.00 total=123\n",
		"pressure/memory": "some avg10=0.00 avg60=0.00 avg300=0.00 total=1\nfull avg10=0.00 avg60=0.00 avg300=0.00 total=1\n",
		"pressure/io":     "some avg10=3.50 avg60=1.00 avg300=0.00 total=9\n",
		"meminfo":         "MemTotal:        2000000 kB\nMemAvailable:     500000 kB\nSwapTotal:       1000000 kB\nSwapFree:         750000 kB\n",
		"loadavg":         "3.20 1.10 0.40 2/400 12345\n",
	}
	for name, body := range files {
		_ = os.WriteFile(filepath.Join(root, name), []byte(body), 0644)
	}
	return root
}

func TestCPUUtilDelta(t *testing.T) {
	root := t.TempDir()
	stat := filepath.Join(root, "stat")
	// first tick establishes baseline
	os.WriteFile(stat, []byte("cpu  100 0 50 1000 0 0 0 0 0 0\n"), 0644)
	r := NewReader(root, 2)
	if u := r.Calm().CPUUtilPct; u != 0 {
		t.Fatalf("first tick util should be 0, got %v", u)
	}
	// second tick: busy += 150 (100 user... actually busy delta), idle += 50
	// new: user 200 system 100 idle 1050 => total delta = (200+100+1050)-(100+50+1000)=200; busy delta=(300-150)=150 -> 75%
	os.WriteFile(stat, []byte("cpu  200 0 100 1050 0 0 0 0 0 0\n"), 0644)
	if u := r.Calm().CPUUtilPct; u < 74.9 || u > 75.1 {
		t.Fatalf("util = %v, want ~75", u)
	}
}
