package monitor

import (
	"testing"
	"time"

	"github.com/faizahmd2/pinproc/internal/contract"
	"github.com/faizahmd2/pinproc/internal/pressure"
)

func newTestMonitor() *Monitor {
	m := New(DefaultConfig(), nil, nil, nil)
	return m
}

func cpuLevels(util, psi float64) pressure.Levels {
	return pressure.Levels{CPUUtilPct: util, CPUStallPct: psi}
}

func TestIdleStaysCalmBelowThreshold(t *testing.T) {
	m := newTestMonitor()
	act, _, _ := m.decide(time.Unix(0, 0), cpuLevels(50, 0))
	if act != actCalm {
		t.Fatalf("act = %v, want calm", act)
	}
}

func TestArmThenCaptureOnThreshold(t *testing.T) {
	m := newTestMonitor()
	t0 := time.Unix(100, 0)
	// util 82% >= armUtil 80 -> arm
	if act, dim, _ := m.decide(t0, cpuLevels(82, 0)); act != actArm || dim != contract.DimensionCPU {
		t.Fatalf("expected arm cpu, got %v %v", act, dim)
	}
	// util 95% >= capUtil 90 -> capture
	act, _, reason := m.decide(t0.Add(2*time.Second), cpuLevels(95, 0))
	if act != actCapture {
		t.Fatalf("expected capture, got %v (%s)", act, reason)
	}
	if m.phase != idle {
		t.Fatal("phase should return to idle after capture")
	}
}

func TestSustainedEscalation(t *testing.T) {
	m := newTestMonitor()
	t0 := time.Unix(0, 0)
	m.decide(t0, cpuLevels(82, 0)) // arm
	// stays at 82 (>=arm, <cap) for < sustain -> watch
	if act, _, _ := m.decide(t0.Add(30*time.Second), cpuLevels(82, 0)); act != actWatch {
		t.Fatalf("expected watch before sustain, got %v", act)
	}
	// after 60s sustained -> capture
	act, _, reason := m.decide(t0.Add(61*time.Second), cpuLevels(82, 0))
	if act != actCapture || reason != "sustained elevation" {
		t.Fatalf("expected sustained capture, got %v (%s)", act, reason)
	}
}

func TestDiscardOnRecovery(t *testing.T) {
	m := newTestMonitor()
	t0 := time.Unix(0, 0)
	m.decide(t0, cpuLevels(82, 0)) // arm
	act, _, reason := m.decide(t0.Add(4*time.Second), cpuLevels(40, 0))
	if act != actDiscard || reason != "recovered" {
		t.Fatalf("expected discard on recovery, got %v (%s)", act, reason)
	}
	if m.phase != idle {
		t.Fatal("phase should be idle after discard")
	}
}

func TestCooldownSuppressesReArm(t *testing.T) {
	m := newTestMonitor()
	t0 := time.Unix(0, 0)
	m.decide(t0, cpuLevels(82, 0))                      // arm
	m.decide(t0.Add(time.Second), cpuLevels(95, 0))     // capture (sets lastCapture)
	// immediately high again, but within cooldown -> no arm
	if act, _, _ := m.decide(t0.Add(2*time.Second), cpuLevels(95, 0)); act != actCalm {
		t.Fatalf("expected calm during cooldown, got %v", act)
	}
	// after cooldown -> arms again
	if act, _, _ := m.decide(t0.Add(3*time.Minute), cpuLevels(95, 0)); act != actArm {
		t.Fatalf("expected arm after cooldown, got %v", act)
	}
}

func TestIOArmsOnPSIOnly(t *testing.T) {
	m := newTestMonitor()
	l := pressure.Levels{IOStallPct: 35} // >= io arm psi 30
	if act, dim, _ := m.decide(time.Unix(0, 0), l); act != actArm || dim != contract.DimensionIO {
		t.Fatalf("expected arm io on PSI, got %v %v", act, dim)
	}
}

func TestWatchWindowDiscard(t *testing.T) {
	m := newTestMonitor()
	t0 := time.Unix(0, 0)
	m.decide(t0, cpuLevels(82, 0)) // arm
	// elevated but never crossing cap, sustain would fire at 60s; use mem-like
	// config? Simpler: set a tiny window.
	m.cfg.WatchWindow = 5 * time.Second
	m.cfg.CPU.Sustain = 0 // disable sustain so only window applies
	act, _, reason := m.decide(t0.Add(6*time.Second), cpuLevels(82, 0))
	if act != actDiscard || reason != "watch window elapsed" {
		t.Fatalf("expected window discard, got %v (%s)", act, reason)
	}
}

func TestNetworkArmsAndCaptures(t *testing.T) {
	m := newTestMonitor()
	t0 := time.Unix(0, 0)
	// conntrack 85% >= arm 80 -> arm network
	if act, dim, _ := m.decide(t0, pressure.Levels{ConntrackPct: 85}); act != actArm || dim != contract.DimensionNetwork {
		t.Fatalf("expected arm network, got %v %v", act, dim)
	}
	// conntrack 96% >= cap 95 -> capture
	if act, _, _ := m.decide(t0.Add(2*time.Second), pressure.Levels{ConntrackPct: 96}); act != actCapture {
		t.Fatalf("expected network capture, got %v", act)
	}
}

func TestNetworkArmsOnRetrans(t *testing.T) {
	m := newTestMonitor()
	if act, dim, _ := m.decide(time.Unix(0, 0), pressure.Levels{TCPRetransPerSec: 60}); act != actArm || dim != contract.DimensionNetwork {
		t.Fatalf("expected arm network on retrans, got %v %v", act, dim)
	}
}

func TestPriorityPrefersCPUWhenComparable(t *testing.T) {
	m := newTestMonitor()
	// CPU and IO equally elevated (both 10 over their arm thresholds). Raw exceedance
	// ties, so the CPU priority weight should break it toward CPU.
	l := pressure.Levels{CPUUtilPct: 90, IOUtilPct: 90}
	if act, dim, _ := m.decide(time.Unix(0, 0), l); act != actArm || dim != contract.DimensionCPU {
		t.Fatalf("expected CPU preferred on comparable elevation, got %v %v", act, dim)
	}
}

func TestSevereIOStillWinsOverCalmCPU(t *testing.T) {
	m := newTestMonitor()
	// CPU not elevated; IO clearly saturated -> IO should arm.
	l := pressure.Levels{CPUUtilPct: 50, IOUtilPct: 99}
	if act, dim, _ := m.decide(time.Unix(0, 0), l); act != actArm || dim != contract.DimensionIO {
		t.Fatalf("expected IO to win when CPU calm, got %v %v", act, dim)
	}
}
