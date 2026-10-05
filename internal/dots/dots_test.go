package dots

import (
	"testing"
	"time"

	"github.com/faizahmd2/pinproc/internal/contract"
)

func TestRingOrderAndOverwrite(t *testing.T) {
	r := NewRing(3)
	r.Arm(contract.DimensionCPU)
	base := time.Unix(0, 0)
	for i := 0; i < 5; i++ {
		r.Add(Dot{At: base.Add(time.Duration(i) * time.Second), Level: float64(i)})
	}
	if r.Len() != 3 {
		t.Fatalf("len = %d, want 3 (capacity)", r.Len())
	}
	snap := r.Snapshot()
	// should hold the last three, oldest-first: levels 2,3,4
	want := []float64{2, 3, 4}
	if len(snap) != 3 {
		t.Fatalf("snapshot len = %d", len(snap))
	}
	for i, w := range want {
		if snap[i].Level != w {
			t.Errorf("snap[%d].Level = %v, want %v", i, snap[i].Level, w)
		}
	}
}

func TestResetReleasesAndClears(t *testing.T) {
	r := NewRing(4)
	r.Arm(contract.DimensionMemory)
	for i := 0; i < 4; i++ {
		r.Add(Dot{Level: float64(i), Top: []Contender{{PID: i, Comm: "x"}}})
	}
	r.Reset()
	if r.Len() != 0 {
		t.Fatalf("len after reset = %d, want 0", r.Len())
	}
	if r.Dim() != "" {
		t.Fatalf("dim after reset = %q, want empty", r.Dim())
	}
	if len(r.Snapshot()) != 0 {
		t.Fatalf("snapshot after reset not empty")
	}
	// every backing slot must be zeroed so no Top slice is retained
	for i := range r.buf {
		if r.buf[i].Top != nil {
			t.Fatalf("slot %d still holds a Top slice after reset", i)
		}
	}
}

func TestArmClearsPrevious(t *testing.T) {
	r := NewRing(2)
	r.Arm(contract.DimensionCPU)
	r.Add(Dot{Level: 1})
	r.Arm(contract.DimensionIO) // re-arm different dimension
	if r.Len() != 0 {
		t.Fatalf("re-arm should clear; len = %d", r.Len())
	}
	if r.Dim() != contract.DimensionIO {
		t.Fatalf("dim = %q, want io", r.Dim())
	}
}
