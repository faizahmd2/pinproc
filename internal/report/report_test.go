package report

import (
	"strings"
	"testing"
	"time"

	"github.com/faizahmd2/pinproc/internal/contract"
)

func TestRenderMarkdownUsesMachineIdentityAndSnapshot(t *testing.T) {
	inv := &contract.Investigation{
		Host:              "localhost",
		IncidentCheckedAt: time.Date(2026, 9, 27, 21, 0, 0, 0, time.FixedZone("IST", 19800)),
		Machine: contract.MachineIdentity{
			Hostname:     "ip-10-0-0-1",
			PrimaryIP:    "10.0.0.1",
			PublicIP:     "203.0.113.5",
			OS:           "Ubuntu 24.04 LTS",
			Kernel:       "6.8.0-test",
			Architecture: "amd64",
			CPUs:         2,
			Uptime:       2 * time.Hour,
		},
		MachineSnapshot: contract.MachineSnapshot{
			CPUs: 2, CPUUtilizationPct: 15, Load1: 0.2,
			MemoryTotalBytes: 8 << 30, MemoryUsedBytes: 3 << 30, MemoryAvailableBytes: 5 << 30,
			MemoryCacheBytes: 2 << 30, MemoryUsedPct: 37.5,
			SwapUsedPct: 0, RootDiskPath: "/", RootDiskTotalBytes: 40 << 30,
			RootDiskUsedBytes: 10 << 30, RootDiskUsedPct: 25,
			PrimaryDiskDevice: "sda", DiskReadBPS: 2 << 20, DiskWriteBPS: 4 << 20,
			DiskUtilizationPct: 12, DiskAwaitMS: 1.5, NetworkRxBPS: 1 << 20,
			NetworkTxBPS: 2 << 20, NetworkRetransmitsPerSec: 0.2, TCPInUse: 8, TCPTimeWait: 2,
		},
		Duration:   time.Second,
		Spent:      contract.Spend{Depth: 1},
		StopReason: contract.StopSufficientEvidence,
		Hypotheses: []contract.Hypothesis{{Statement: "root filesystem is filling", Grade: contract.GradeObserved, Confidence: 0.9, Entity: contract.Entity{Kind: contract.EntityMachine, ID: "machine"}}},
	}
	got := RenderMarkdown(inv)
	for _, want := range []string{
		"pinproc · ip-10-0-0-1 · 203.0.113.5 (public) · 10.0.0.1 (private) · 2 vCPU",
		"27 Sep 2026, 09:00 PM (IST)",
		"root filesystem is filling",
		"## Machine",
		"| Resource | State | Details |",
		"| **CPU** | 🟢 idle | 15% busy · load 0.20 / 0.00 / 0.00 (1m/5m/15m)",
		"| **Memory** | 🟢 healthy | 3.0 GiB used · 5.0 GiB free of 8.0 GiB · caches 2.0 GiB · no swap |",
		"| **Disk I/O** | 🟢 active | `sda` · read 2.0 MB/s · write 4.0 MB/s · 12% of time busy · 1.5 ms latency |",
		"| **Network** | 🟢 active | in 1.0 MB/s · out 2.0 MB/s · 0.2/s retransmits",
		"| **Sockets** | 🟢 quiet | 0 open · 8 active · 2 just-closed, clearing (TIME_WAIT) · 0 refused (listen queue overflow) |",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q:\n%s", want, got)
		}
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
