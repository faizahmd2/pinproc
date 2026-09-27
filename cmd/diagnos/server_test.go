package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/faizahmd2/pinproc/internal/config"
	"github.com/faizahmd2/pinproc/internal/report"
)

func TestInvestigateReturnsProcessingWhenAlreadyRunning(t *testing.T) {
	dir := t.TempDir()
	if err := report.StartState(dir, time.Now()); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	s := &nativeServer{cfg: cfg, report: dir}
	s.mu.Lock()
	defer s.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/investigate", nil)
	rec := httptest.NewRecorder()
	s.handleInvestigate(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "processing" {
		t.Fatalf("unexpected response: %#v", body)
	}
	if _, ok := body["id"]; ok {
		t.Fatalf("unexpected id in response: %#v", body)
	}
}

func TestNonGetEndpointsAppearMissing(t *testing.T) {
	dir := t.TempDir()
	s := &nativeServer{cfg: &config.Config{}, report: dir}
	for _, path := range []string{"/investigate", "/report", "/health"} {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		rec := httptest.NewRecorder()
		switch path {
		case "/investigate":
			s.handleInvestigate(rec, req)
		case "/report":
			s.handleReport(rec, req)
		case "/health":
			s.handleHealth(rec, req)
		}
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s returned %d, body=%s", path, rec.Code, rec.Body.String())
		}
	}
}

func TestReportReturnsProcessingWithoutPreviousReport(t *testing.T) {
	dir := t.TempDir()
	if err := report.StartState(dir, time.Now()); err != nil {
		t.Fatal(err)
	}
	s := &nativeServer{cfg: &config.Config{}, report: dir}
	req := httptest.NewRequest(http.MethodGet, "/report", nil)
	rec := httptest.NewRecorder()
	s.handleReport(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "\"" + "id" + "\"") {
		t.Fatalf("unexpected id in response: %s", rec.Body.String())
	}
}
