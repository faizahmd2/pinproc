package report

import (
	"strings"
	"testing"
	"time"

	"github.com/faizahmd2/pinproc/internal/contract"
)

func TestRenderMarkdownUsesMachineIdentityAndSnapshot(t *testing.T) {
	inv := &contract.Investigation{
		Host: "localhost",
		IncidentCheckedAt: time.Date(2026, 9, 27, 21, 0, 0, 0, time.FixedZone("IST", 19800)),
		Machine: contract.MachineIdentity{
			Hostname: "ip-10-0-0-1",
			PrimaryIP: "10.0.0.1",
			OS: "Ubuntu 24.04 LTS",
			Kernel: "6.8.0-test",
			Architecture: "amd64",
			CPUs: 2,
			Uptime: 2 * time.Hour,
		},
		MachineSnapshot: contract.MachineSnapshot{
			CPUs: 2, CPUUtilizationPct: 15, Load1: 0.2,
			MemoryTotalBytes: 8 << 30, MemoryUsedBytes: 3 << 30, MemoryUsedPct: 37.5,
			SwapUsedPct: 0, RootDiskPath: "/", RootDiskTotalBytes: 40 << 30,
			RootDiskUsedBytes: 10 << 30, RootDiskUsedPct: 25,
			PrimaryDiskDevice: "sda", DiskReadBPS: 2 << 20, DiskWriteBPS: 4 << 20,
			DiskUtilizationPct: 12, DiskAwaitMS: 1.5, NetworkRxBPS: 1 << 20,
			NetworkTxBPS: 2 << 20, NetworkRetransmitsPerSec: 0.2, TCPInUse: 8, TCPTimeWait: 2,
		},
		Duration: time.Second,
		Spent: contract.Spend{Depth: 1},
		StopReason: contract.StopSufficientEvidence,
		Hypotheses: []contract.Hypothesis{{Statement: "root filesystem is filling", Grade: contract.GradeObserved, Confidence: 0.9, Entity: contract.Entity{Kind: contract.EntityMachine, ID: "machine"}}},
	}
	got := RenderMarkdown(inv)
	for _, want := range []string{
		"# pinproc — ip-10-0-0-1",
		"Incident checked: 2026-09-27T21:00:00+05:30",
		"Hostname: ip-10-0-0-1",
		"IP: 10.0.0.1",
		"CPU: 15% used · load1 0.20",
		"Memory:",
		"Disk: sda",
		"Network: RX",
		"Sockets: 0 · TCP in-use 8",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q:\n%s", want, got)
		}
	}
}

func TestRenderMarkdownShowsDiagnosticsForCollectionFailure(t *testing.T) {
	inv := &contract.Investigation{
		Host: "localhost",
		Machine: contract.MachineIdentity{Hostname: "test-host"},
		StopReason: contract.StopSufficientEvidence,
		Hypotheses: []contract.Hypothesis{{Statement: "test", Entity: contract.Entity{Kind: contract.EntityMachine, ID: "machine"}}},
		Evidence: []contract.Evidence{{Err: "read failed", Verify: []string{"/proc/stat"}, Entity: contract.Entity{Kind: contract.EntityMachine, ID: "machine"}}},
	}
	got := RenderMarkdown(inv)
	if !strings.Contains(got, "Verify") {
		t.Fatalf("expected diagnostics:\n%s", got)
	}
}
