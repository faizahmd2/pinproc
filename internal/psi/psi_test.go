package psi

import (
	"context"
	"testing"
	"time"
)

func TestConfigSpec(t *testing.T) {
	cases := []struct {
		name    string
		cfg     Config
		want    string
		wantErr bool
	}{
		{"memory some", Config{Resource: Memory, Kind: Some, Stall: 150 * time.Millisecond, Window: time.Second}, "some 150000 1000000", false},
		{"default kind+window", Config{Resource: IO, Stall: 50 * time.Millisecond}, "some 50000 1000000", false},
		{"cpu full rejected", Config{Resource: CPU, Kind: Full, Stall: 10 * time.Millisecond, Window: time.Second}, "", true},
		{"window too small", Config{Resource: CPU, Stall: 10 * time.Millisecond, Window: 100 * time.Millisecond}, "", true},
		{"window too large", Config{Resource: CPU, Stall: time.Second, Window: 20 * time.Second}, "", true},
		{"stall exceeds window", Config{Resource: Memory, Stall: 2 * time.Second, Window: time.Second}, "", true},
		{"bad resource", Config{Resource: "disk", Stall: 10 * time.Millisecond, Window: time.Second}, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := c.cfg.spec()
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Fatalf("spec = %q, want %q", got, c.want)
			}
		})
	}
}

// TestArmAndClose exercises the real kernel path when PSI triggers are available
// (Linux >= 5.2). It is skipped elsewhere so the suite still passes on macOS/CI
// without PSI. It does not assert a firing — it only proves arm+close is clean.
func TestArmAndClose(t *testing.T) {
	if !Available() {
		t.Skip("PSI not available on this host")
	}
	tr, err := Arm(Config{Resource: Memory, Kind: Some, Stall: 100 * time.Millisecond, Window: time.Second})
	if err != nil {
		t.Skipf("PSI present but triggers unavailable (kernel <5.2?): %v", err)
	}
	if tr.Resource() != Memory {
		t.Fatalf("resource = %q", tr.Resource())
	}
	// Wait with an immediate deadline: should return the context error promptly,
	// proving Wait honours cancellation rather than blocking forever.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := tr.Wait(ctx); err == nil {
		t.Log("trigger fired during the test window (acceptable)")
	} else if err != context.DeadlineExceeded {
		t.Fatalf("unexpected Wait error: %v", err)
	}
	if err := tr.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}
