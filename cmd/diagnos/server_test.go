package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/faizahmd2/pinproc/internal/config"
	"github.com/faizahmd2/pinproc/internal/contract"
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


func TestWantsJSONOnlyWhenTrue(t *testing.T) {
	for _, tc := range []struct {
		name string
		url  string
		want bool
	}{
		{name: "default", url: "/report", want: false},
		{name: "true", url: "/report?json=true", want: true},
		{name: "uppercase true", url: "/report?json=TRUE", want: true},
		{name: "false", url: "/report?json=false", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.url, nil)
			if got := wantsJSON(req); got != tc.want {
				t.Fatalf("wantsJSON()=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestReportReturnsMarkdownByDefault(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	inv := &contract.Investigation{
		Host:              "test-host",
		StartedAt:         now,
		IncidentCheckedAt: now,
	}
	if err := report.Write(inv, dir); err != nil {
		t.Fatal(err)
	}

	s := &nativeServer{cfg: &config.Config{}, report: dir}
	req := httptest.NewRequest(http.MethodGet, "/report", nil)
	rec := httptest.NewRecorder()
	s.handleReport(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "text/markdown; charset=utf-8" {
		t.Fatalf("content-type=%q", got)
	}
	if !strings.HasPrefix(rec.Body.String(), "pinproc · test-host") {
		t.Fatalf("unexpected markdown body: %s", rec.Body.String())
	}
}

func TestReportReturnsJSONWhenRequested(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	inv := &contract.Investigation{
		Host:              "test-host",
		StartedAt:         now,
		IncidentCheckedAt: now,
	}
	if err := report.Write(inv, dir); err != nil {
		t.Fatal(err)
	}

	s := &nativeServer{cfg: &config.Config{}, report: dir}
	req := httptest.NewRequest(http.MethodGet, "/report?json=true", nil)
	rec := httptest.NewRecorder()
	s.handleReport(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("content-type=%q", got)
	}
	if !json.Valid(rec.Body.Bytes()) {
		t.Fatalf("response is not valid JSON: %s", rec.Body.String())
	}
}
