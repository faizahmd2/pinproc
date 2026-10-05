package aggregate

import "testing"

func TestGroupSumsAndRanks(t *testing.T) {
	procs := []Proc{
		{PID: 1, Comm: "stress", Key: "pgid:100", Display: "stress-ng", Value: 50},
		{PID: 2, Comm: "stress", Key: "pgid:100", Display: "stress-ng", Value: 60},
		{PID: 3, Comm: "stress", Key: "pgid:100", Display: "stress-ng", Value: 40},
		{PID: 9, Comm: "mysqld", Key: "unit:mysql.service", Display: "mysql", Value: 30},
	}
	svcs := Group(procs, 2)
	if len(svcs) != 2 {
		t.Fatalf("want 2 services, got %d", len(svcs))
	}
	// stress-ng total = 150 should rank first
	if svcs[0].Display != "stress-ng" || svcs[0].Value != 150 || svcs[0].Procs != 3 {
		t.Fatalf("svc0 = %+v", svcs[0])
	}
	// components sorted desc, capped to 2, with pct of 150
	if len(svcs[0].Components) != 2 {
		t.Fatalf("want 2 components, got %d", len(svcs[0].Components))
	}
	if svcs[0].Components[0].PID != 2 { // value 60 highest
		t.Fatalf("top component pid = %d, want 2", svcs[0].Components[0].PID)
	}
	if got := svcs[0].Components[0].Pct; got < 39.9 || got > 40.1 { // 60/150=40%
		t.Fatalf("top component pct = %v, want 40", got)
	}
}
