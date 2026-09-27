package report

import (
	"testing"
	"time"
)

func TestInspectionStateLifecycle(t *testing.T) {
	dir := t.TempDir()
	if err := WriteState(dir, InspectionState{Status: StatusIdle}); err != nil {
		t.Fatal(err)
	}
	start := time.Now().Add(-time.Second)
	if err := StartState(dir, start); err != nil {
		t.Fatal(err)
	}
	s, err := ReadState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.Status != StatusRunning || s.StartedAt == nil {
		t.Fatalf("unexpected running state: %+v", s)
	}
	if err := FinishState(dir); err != nil {
		t.Fatal(err)
	}
	s, err = ReadState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.Status != StatusDone || s.FinishedAt == nil {
		t.Fatalf("unexpected done state: %+v", s)
	}
}
