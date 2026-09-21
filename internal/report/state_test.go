package report

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInspectionStateLifecycle(t *testing.T) {
	dir := t.TempDir()
	if err := WriteState(dir, InspectionState{Status: StatusIdle}); err != nil {
		t.Fatal(err)
	}
	if err := StartState(dir, "inv-1", "localhost", "test"); err != nil {
		t.Fatal(err)
	}
	s, err := ReadState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.Status != StatusRunning || s.ID != "inv-1" || s.Stage != "accepted" {
		t.Fatalf("unexpected running state: %+v", s)
	}
	if err := UpdateState(dir, "deep_investigation"); err != nil {
		t.Fatal(err)
	}
	if err := FinishState(dir); err != nil {
		t.Fatal(err)
	}
	s, err = ReadState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.Status != StatusDone || s.Stage != "done" {
		t.Fatalf("unexpected done state: %+v", s)
	}
	if _, err := os.Stat(filepath.Join(dir, "state.json")); err != nil {
		t.Fatal(err)
	}
}
