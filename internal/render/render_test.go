package render

import (
	"strings"
	"testing"
	"time"

	"github.com/faizahmd2/pinproc/internal/contract"
	"github.com/faizahmd2/pinproc/internal/doc"
	"github.com/faizahmd2/pinproc/internal/inventory"
)

func sample() *inventory.Snapshot {
	return &inventory.Snapshot{
		CollectedAt: time.Date(2026, 11, 17, 10, 45, 0, 0, time.UTC), Window: time.Second,
		Host: inventory.Host{Hostname: "prod-api-node-04", PrimaryIP: "10.0.4.15", OS: "Ubuntu 24.04 LTS", Kernel: "6.8.0-45-generic", Arch: "x86_64", Uptime: 42 * 24 * time.Hour},
		CPU:  inventory.CPU{Cores: 4, BusyPct: 96, UserPct: 80, SystemPct: 16, Buckets: []inventory.Bucket{{Key: "user", Label: "Applications", Value: 3.2}, {Key: "kernel", Label: "Kernel", Value: 0.64}, {Key: "idle", Label: "Idle headroom", Value: 0.16}}},
		Memory: inventory.Memory{TotalBytes: 8 << 30, AccountedBytes: 8 << 30, UsedBytes: 7 << 30, UsedPct: 87.5, AvailableBytes: 1 << 30,
			Buckets: []inventory.Bucket{{Key: "apps", Label: "Applications", Value: 6 << 30, Parts: []inventory.Share{{Name: "postgresql@16", Value: 5 << 30}}}, {Key: "free", Label: "Free", Value: 2 << 30}}},
		Disks: []inventory.Disk{{Name: "nvme0n1", Kind: "NVMe", SizeBytes: 80 << 30, UtilPct: 98, AwaitMS: 44,
			Buckets: []inventory.Bucket{{Key: "used:/", Label: "Used · /", Value: 40 << 30}, {Key: "free", Label: "Free", Value: 40 << 30}},
			Mounts:  []inventory.MountUse{{Path: "/", Device: "nvme0n1p1", FSType: "ext4", UsedBytes: 40 << 30, AvailBytes: 40 << 30, UsedPct: 50}}}},
		Services: []inventory.Service{{Key: "unit:postgresql@16.service", Name: "postgresql@16", Unit: "postgresql@16.service", Kind: "systemd", Procs: 3, MainPID: 1104,
			Exe: "/usr/lib/postgresql/16/bin/postgres", Cmdline: `postgres -D /data <script>alert(1)</script>`, MemBytes: 5 << 30, CPUCores: 2.15, OOMScore: 614}},
		Processes: []inventory.Process{{PID: 1104, Comm: "postgres", Exe: "/usr/lib/postgresql/16/bin/postgres", RSSBytes: 5 << 30, CPUCores: 2.15}},
	}
}

func TestHTMLRendersBothLayoutsAndEscapes(t *testing.T) {
	ist, zone, err := ParseZone("IST")
	if err != nil {
		t.Fatal(err)
	}
	o := Options{Location: ist, Zone: zone}
	for _, k := range []doc.Kind{doc.KindAlert, doc.KindReport} {
		d := doc.New(k, nil, sample())
		d.Dimension = contract.DimensionMemory
		d.Severity = doc.SeverityCritical
		d.Incident = &contract.Incident{Dimension: contract.DimensionMemory, Cause: &contract.ServiceCause{Key: "unit:postgresql@16.service", Service: "postgresql@16", Value: 5 << 30, Unit: "bytes"}}
		b, err := HTML(d, o)
		if err != nil {
			t.Fatalf("%s: %v", k, err)
		}
		h := string(b)
		for _, want := range []string{"prod-api-node-04", "10.0.4.15", "17 Nov 2026 16:15 IST", "postgresql@16.service", "/usr/lib/postgresql/16/bin/postgres"} {
			if !strings.Contains(h, want) {
				t.Errorf("%s: missing %q", k, want)
			}
		}
		if strings.Contains(h, "<script>alert(1)") {
			t.Errorf("%s: command line was not escaped", k)
		}
		if strings.Contains(h, "ZgotmplZ") {
			t.Errorf("%s: template produced an unsafe-value placeholder", k)
		}
	}
}

func TestEmailIsInlineAndHasTextPart(t *testing.T) {
	d := doc.New(doc.KindAlert, nil, sample())
	d.Severity, d.Headline = doc.SeverityCritical, "Memory pressure — postgresql@16 is the cause: 5.0 GiB."
	e, err := RenderEmail(d, Options{}, "pinproc-alert.html")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.Subject, "prod-api-node-04") || !strings.Contains(e.Subject, "postgresql@16") {
		t.Errorf("subject = %q", e.Subject)
	}
	if strings.Contains(e.HTML, "<script") || strings.Contains(e.HTML, "class=") {
		t.Error("email HTML must be inline-styled with no scripts or classes")
	}
	if !strings.Contains(e.HTML, "17 Nov 2026 10:45 UTC") || !strings.Contains(e.HTML, "pinproc-alert.html") {
		t.Error("email missing time or attachment note")
	}
	if e.Text == "" {
		t.Error("empty text alternative")
	}
}

func TestParseZone(t *testing.T) {
	cases := map[string]string{"": "UTC", "utc": "UTC", "IST": "IST", "+05:30": "UTC+05:30", "-08:00": "UTC-08:00"}
	for in, want := range cases {
		_, label, err := ParseZone(in)
		if err != nil || label != want {
			t.Errorf("ParseZone(%q) = %q, %v; want %q", in, label, err, want)
		}
	}
	loc, _, _ := ParseZone("IST")
	if _, off := time.Date(2026, 1, 1, 0, 0, 0, 0, loc).Zone(); off != 19800 {
		t.Errorf("IST offset = %d", off)
	}
	if _, _, err := ParseZone("Mars/Olympus"); err == nil {
		t.Error("expected error for unknown zone")
	}
}

func TestFormatting(t *testing.T) {
	if got := Bytes(5 << 30); got != "5.00 GiB" {
		t.Errorf("Bytes = %q", got)
	}
	if got := MiB(3212 << 20); got != "3,212.0" {
		t.Errorf("MiB = %q", got)
	}
	if got := Rate(482.5e6); got != "482.5 MB/s" {
		t.Errorf("Rate = %q", got)
	}
}
