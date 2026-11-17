package render

import (
	"strings"
	"testing"
	"time"

	"github.com/faizahmd2/pinproc/internal/contract"
	"github.com/faizahmd2/pinproc/internal/doc"
	"github.com/faizahmd2/pinproc/internal/inventory"
	"github.com/faizahmd2/pinproc/internal/journal"
)

func alertDoc() *doc.Document {
	d := doc.New(doc.KindAlert, nil, sample())
	d.Trigger, d.Severity, d.Dimension = "auto:memory", doc.SeverityCritical, contract.DimensionMemory
	d.Incident = &contract.Incident{Dimension: contract.DimensionMemory, Cause: &contract.ServiceCause{Key: "unit:postgresql@16.service", Service: "postgresql@16", Value: 5 << 30, Unit: "bytes"}}
	d.Tag = "production"
	return d
}

func TestSubjectPlaceholders(t *testing.T) {
	d := alertDoc()
	if got := Subject(d, ""); got != "[pinproc] CRITICAL · memory pressure on prod-api-node-04 — postgresql@16 5.00 GiB" {
		t.Fatalf("default subject = %q", got)
	}
	if got := Subject(d, "[{tag}] {host} ({ip}) {dimension}: {cause}"); got != "[production] prod-api-node-04 (10.0.4.15) memory: postgresql@16 5.00 GiB" {
		t.Fatalf("custom subject = %q", got)
	}
	d.Tag = ""
	if got := Subject(d, "[{tag}] {host}: {cause}"); got != "prod-api-node-04: postgresql@16 5.00 GiB" {
		t.Fatalf("empty tag subject = %q", got)
	}
	r := doc.New(doc.KindReport, nil, sample())
	r.Tag = "production"
	if got := Subject(r, "{cause} ignored for reports"); got != "[pinproc] prod-api-node-04 — machine report (production)" {
		t.Fatalf("report subject = %q", got)
	}
}

func TestReportHasNoFindingsAndShowsWorkloadSections(t *testing.T) {
	s := sample()
	s.Services[0].Systemd = &inventory.UnitInfo{File: "/lib/systemd/system/postgresql@.service", Restart: "on-failure", Restarts: 2, DependsOn: []string{"network-online.target"}}
	s.Services[0].Listen = []inventory.Listener{{Proto: "tcp", Addr: "127.0.0.1", Port: 5432, Clients: 12}}
	s.Services[0].Peers = []inventory.Peer{{Addr: "10.0.0.9", Port: 6379, Conns: 3, Name: "redis"}}
	s.Checks = []inventory.Check{{Name: "Swap", State: "warn", Value: "none", Cmd: "swapon --show"}}
	d := doc.New(doc.KindReport, nil, s)
	d.Findings = []doc.Finding{{Statement: "should never render in a report"}}
	b, err := HTML(d, Options{})
	if err != nil {
		t.Fatal(err)
	}
	h := string(b)
	for _, want := range []string{"Runs", "Uses", "Connects", "Verify", "127.0.0.1:5432 (12 clients)", "redis 10.0.0.9:6379 ×3", "restarted 2×", "Host checks", "swapon --show", "systemctl status postgresql@16.service"} {
		if !strings.Contains(h, want) {
			t.Errorf("report missing %q", want)
		}
	}
	if strings.Contains(h, "should never render") || strings.Contains(h, "Also noticed") {
		t.Error("report must not show findings")
	}
}

func TestAlertShowsLogsAndOnlyItsResource(t *testing.T) {
	d := alertDoc()
	d.Logs = &journal.Excerpt{Sections: []journal.Section{
		{Title: "postgresql@16.service", Cmd: "journalctl -u postgresql@16.service", Lines: []journal.Line{{At: time.Date(2026, 11, 17, 10, 44, 0, 0, time.UTC), Priority: 3, Message: "out of shared memory", Count: 4}}},
		{Title: "Kernel", Cmd: "journalctl -k", Lines: []journal.Line{{Priority: 3, Ident: "kernel", Message: "Out of memory: Killed process 1104 (postgres)"}}},
	}}
	b, err := HTML(d, Options{})
	if err != nil {
		t.Fatal(err)
	}
	h := string(b)
	for _, want := range []string{"Logs from the incident window", "out of shared memory", "×4", "Killed process 1104", "Memory Zero-Sum"} {
		if !strings.Contains(h, want) {
			t.Errorf("alert missing %q", want)
		}
	}
	if strings.Contains(h, "CPU Time &amp; Scheduler") || strings.Contains(h, "Also noticed") {
		t.Error("a memory alert should show only the memory breakdown, and no empty findings box")
	}
	e, err := RenderEmail(d, Options{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.HTML, "Logs at the time") || !strings.Contains(e.HTML, "Killed process 1104") {
		t.Error("email should carry the log excerpt")
	}
}
