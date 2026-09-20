package engine

import (
	"testing"

	"github.com/faizahmd2/pinproc/internal/contract"
)

func TestBuildMachineSnapshot(t *testing.T) {
	inv := &contract.Investigation{
		Machine: contract.MachineIdentity{CPUs: 4, MemTotal: 8 << 30},
		Evidence: []contract.Evidence{
			{Observations: []contract.Observation{
				{Key: "cpu.utilization", Value: 23.5, Unit: "percent"},
				{Key: "load.one_per_core", Value: 0.25, Unit: "ratio"},
				{Key: "mem.available_pct", Value: 62.5, Unit: "percent"},
				{Key: "mem.swap_used_pct", Value: 4, Unit: "percent"},
			}},
		},
	}
	s := buildMachineSnapshot(inv)
	if s.CPUs != 4 {
		t.Fatalf("CPUs=%d", s.CPUs)
	}
	if s.CPUUtilizationPct != 23.5 {
		t.Fatalf("CPU utilization=%v", s.CPUUtilizationPct)
	}
	if s.Load1 != 1 {
		t.Fatalf("load1=%v", s.Load1)
	}
	if s.MemoryAvailableBytes != 5<<30 {
		t.Fatalf("memory available=%d", s.MemoryAvailableBytes)
	}
	if s.MemoryUsedBytes != 3<<30 {
		t.Fatalf("memory used=%d", s.MemoryUsedBytes)
	}
	if s.MemoryUsedPct != 37.5 {
		t.Fatalf("memory used pct=%v", s.MemoryUsedPct)
	}
	if s.SwapUsedPct != 4 {
		t.Fatalf("swap used pct=%v", s.SwapUsedPct)
	}
	if s.RootDiskPath != "/" {
		t.Fatalf("root disk path=%q", s.RootDiskPath)
	}
}
