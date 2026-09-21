package report

import (
	"strings"
	"testing"
	"time"

	"github.com/faizahmd2/pinproc/internal/contract"
)

func TestRenderMarkdownUsesMachineIdentityAndHidesDiagnosticsOnSuccess(t *testing.T) {
	inv := &contract.Investigation{
		Host: "localhost",
		Machine: contract.MachineIdentity{
			Hostname:     "ip-10-0-0-1",
			PrimaryIP:    "10.0.0.1",
			OS:           "Ubuntu 24.04 LTS",
			Kernel:       "6.8.0-test",
			Architecture: "amd64",
			CPUs:         2,
			Uptime:       2 * time.Hour,
		},
		MachineSnapshot: contract.MachineSnapshot{
			CPUs:               2,
			CPUUtilizationPct:  15,
			Load1:              0.2,
			MemoryTotalBytes:   8 << 30,
			MemoryUsedBytes:    3 << 30,
			MemoryUsedPct:      37.5,
			SwapUsedPct:        0,
			RootDiskPath:       "/",
			RootDiskTotalBytes: 40 << 30,
			RootDiskUsedBytes:  10 << 30,
			RootDiskUsedPct:    25,
		},
		Duration:   time.Second,
		Spent:      contract.Spend{Depth: 1},
		Budget:     contract.BudgetNormal(),
		StopReason: contract.StopSufficientEvidence,
		Hypotheses: []contract.Hypothesis{{Statement: "root filesystem is filling", Grade: contract.GradeObserved, Confidence: 0.9, Entity: contract.Entity{Kind: contract.EntityMachine, ID: "machine"}}},
	}
	got := RenderMarkdown(inv)
	if !strings.Contains(got, "# pinproc — ip-10-0-0-1") {
		t.Fatalf("missing hostname:\n%s", got)
	}
	for _, want := range []string{"IP: 10.0.0.1", "OS: Ubuntu 24.04 LTS", "Arch: amd64 · CPUs: 2", "CPU: 15% used · load1 0.20", "Memory:", "Disk /:"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Verify") || strings.Contains(got, "machine →") {
		t.Fatalf("success report leaked traversal diagnostics:\n%s", got)
	}
}

func TestRenderMarkdownShowsDiagnosticsForCollectionFailure(t *testing.T) {
	inv := &contract.Investigation{
		Host:       "localhost",
		Machine:    contract.MachineIdentity{Hostname: "test-host"},
		StopReason: contract.StopSufficientEvidence,
		Hypotheses: []contract.Hypothesis{{Statement: "test", Entity: contract.Entity{Kind: contract.EntityMachine, ID: "machine"}}},
		Evidence:   []contract.Evidence{{Err: "read failed", Verify: []string{"/proc/stat"}, Entity: contract.Entity{Kind: contract.EntityMachine, ID: "machine"}}},
	}
	got := RenderMarkdown(inv)
	if !strings.Contains(got, "Verify") {
		t.Fatalf("expected diagnostics:\n%s", got)
	}
}
