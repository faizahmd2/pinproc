package doc

import (
	"testing"

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
