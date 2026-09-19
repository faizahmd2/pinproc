package contract

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

func TestRoundTripStable(t *testing.T) {
	in := Investigation{SchemaVersion: SchemaVersion, ID: "inv-test", Host: "host", Trigger: "manual", StartedAt: time.Unix(1, 2).UTC(), Budget: BudgetFast(), Spent: Spend{Steps: 1}, Evidence: []Evidence{{ID: "ev-1", Capability: "machine.cpu", Entity: Entity{Kind: EntityMachine, ID: "machine"}, Dimension: DimensionCPU, Level: L1Machine, CollectedAt: time.Unix(1, 2).UTC(), Sources: []string{"/proc/stat"}}}, StopReason: StopNoAnomaly}
	b1, e := json.Marshal(in)
	if e != nil {
		t.Fatal(e)
	}
	var out Investigation
	if e = json.Unmarshal(b1, &out); e != nil {
		t.Fatal(e)
	}
	b2, e := json.Marshal(out)
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(b1, b2) {
		t.Fatalf("round trip changed bytes: %s != %s", b1, b2)
	}
}
