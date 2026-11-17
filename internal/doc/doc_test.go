package doc

import (
	"strings"
	"testing"

	"github.com/faizahmd2/pinproc/internal/contract"
	"github.com/faizahmd2/pinproc/internal/inventory"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	d := New(KindReport, nil, &inventory.Snapshot{Host: inventory.Host{Hostname: "h1"}, Notes: []string{"n"}})
	path, err := Save(t.TempDir(), "report", d)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != KindReport || got.Snapshot.Host.Hostname != "h1" || len(got.Notes) != 1 || got.Schema != Schema {
		t.Fatalf("round trip = %+v", got)
	}
}

func TestFindingsAreSelective(t *testing.T) {
	hyps := []contract.Hypothesis{
		{Statement: "cause restated", Dimension: contract.DimensionMemory, Source: "owner:memory", Confidence: 0.9},
		{Statement: "weak memory hunch", Dimension: contract.DimensionMemory, Confidence: 0.25},
		{Statement: "swap storm", Dimension: contract.DimensionMemory, Confidence: 0.75},
		{Statement: "largest dir on /", Dimension: contract.DimensionFilesystem, Source: "owner:filesystem", Confidence: 0.9},
		{Statement: "very sure about cpu", Dimension: contract.DimensionCPU, Confidence: 0.8},
	}
	got := findings(hyps, contract.DimensionMemory, contract.DimensionMemory)
	var s []string
	for _, f := range got {
		s = append(s, f.Statement)
	}
	if strings.Join(s, "|") != "swap storm|very sure about cpu" {
		t.Fatalf("findings = %v", s)
	}
	if r := New(KindReport, &contract.Investigation{Hypotheses: hyps}, nil); len(r.Findings) != 0 {
		t.Fatalf("a report must carry no findings, got %v", r.Findings)
	}
}
