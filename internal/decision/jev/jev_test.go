package jev

import (
	"context"
	"github.com/faizahmd2/diagnos/internal/decision"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestProvider parses the documented response shape without network access.
func TestProvider(t *testing.T) {
	srv := httptest.NewServer(nil)
	_ = srv
	_ = context.Background()
	_ = http.MethodPost
	_ = time.Second
	_ = decision.QChoice
	srv.Close()
	data := []byte(`{"answers":{"primary_dimension":{"type":"choice","choice":"cpu","probabilities":{"cpu":0.8},"confidence":0.9},"cpu_constrained":{"type":"noul","noul":0.2},"severity":{"type":"score","score":2.7,"confidence":0.8}}}`)
	a, e := parse(data)
	if e != nil {
		t.Fatal(e)
	}
	if a["primary_dimension"].Choice != "cpu" || a["severity"].Score != 2.7 {
		t.Fatalf("bad parsed answers: %#v", a)
	}
	if a["cpu_constrained"].Confidence != 0.6 {
		t.Fatalf("bad noul confidence: %v", a["cpu_constrained"].Confidence)
	}
}
