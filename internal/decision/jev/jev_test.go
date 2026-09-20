package jev

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/faizahmd2/pinproc/internal/decision"
)

func TestProviderUsesDocumentedWireFormat(t *testing.T) {
	var got request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method=%s", r.Method)
		}
		if r.URL.Path != "/v1/systemone" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("authorization header missing")
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"jev-1.13.0","answers":{"route":{"type":"choice","choice":"io","confidence":0.91,"probabilities":{"cpu":0.04,"io":0.91,"none":0.05}},"blocked":{"type":"noul","noul":0.88},"severity":{"type":"score","score":2.25,"legend":{"0":"normal","1":"suspicious","2":"critical"},"confidence":0.73,"probabilities":{"0":0.0,"1":0.75,"2":0.25}}},"usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()

	questions := map[string]decision.Question{
		"route": {
			Type:         decision.QChoice,
			Instructions: "Which resource is constrained?",
			Criteria: map[string]string{
				"cpu":  "CPU is scarce",
				"io":   "I/O is scarce",
				"none": "Nothing is scarce",
			},
		},
		"blocked": {
			Type:         decision.QNoul,
			Instructions: "Is the machine blocked?",
			Criteria:     map[string]string{"true": "Blocked", "false": "Not blocked"},
		},
		"severity": {
			Type:         decision.QScore,
			Instructions: "How abnormal is the machine?",
			Levels:       []string{"normal", "suspicious", "critical"},
		},
	}

	p := New(srv.URL, "jev-latest", "test-key", time.Second)
	answers, err := p.Ask(context.Background(), map[string]any{"host": "example"}, questions)
	if err != nil {
		t.Fatal(err)
	}

	if got.Questions["route"].Type != "choice" {
		t.Fatalf("choice type: %#v", got.Questions["route"])
	}
	choiceCriteria, ok := got.Questions["route"].Criteria.(map[string]any)
	if !ok || choiceCriteria["cpu"] != "CPU is scarce" {
		t.Fatalf("choice criteria: %#v", got.Questions["route"].Criteria)
	}
	if got.Questions["blocked"].Type != "noul" {
		t.Fatalf("noul type: %#v", got.Questions["blocked"])
	}
	if got.Questions["severity"].Type != "score" {
		t.Fatalf("score type: %#v", got.Questions["severity"])
	}
	levels, ok := got.Questions["severity"].Criteria.([]any)
	if !ok || len(levels) != 3 || levels[2] != "critical" {
		t.Fatalf("score criteria: %#v", got.Questions["severity"].Criteria)
	}

	if answers["route"].Choice != "io" || answers["route"].Confidence != 0.91 {
		t.Fatalf("choice answer: %#v", answers["route"])
	}
	if answers["blocked"].Noul != 0.88 || answers["blocked"].Confidence != 0 {
		t.Fatalf("noul answer: %#v", answers["blocked"])
	}
	if answers["severity"].Score != 2.25 || answers["severity"].Legend["2"] != "critical" {
		t.Fatalf("score answer: %#v", answers["severity"])
	}
}
