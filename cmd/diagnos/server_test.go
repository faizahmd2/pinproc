package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/faizahmd2/pinproc/internal/config"
	"github.com/faizahmd2/pinproc/internal/report"
)

func TestTriggerRejectsConcurrentInspection(t *testing.T) {
	dir := t.TempDir()
	if err := report.StartState(dir, "inv-active", "localhost", "first"); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.Engine.Budget = "fast"
	s := &nativeServer{cfg: cfg, report: dir}
	s.mu.Lock()
	defer s.mu.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/trigger", nil)
	rec := httptest.NewRecorder()
	s.handleTrigger(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "running" || body["id"] != "inv-active" {
		t.Fatalf("unexpected response: %#v", body)
	}
}
