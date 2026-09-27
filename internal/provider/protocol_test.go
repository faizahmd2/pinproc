package provider

import (
	"encoding/json"
	"testing"

	"github.com/faizahmd2/pinproc/internal/decision"
)

func TestResponseValidate(t *testing.T) {
	if err := (Response{ProtocolVersion: ProtocolVersion, RequestID: "req-1"}).Validate("req-1"); err != nil {
		t.Fatal(err)
	}
	if err := (Response{ProtocolVersion: 99, RequestID: "req-1"}).Validate("req-1"); err == nil {
		t.Fatal("expected version error")
	}
	if err := (Response{ProtocolVersion: ProtocolVersion, RequestID: "req-2"}).Validate("req-1"); err == nil {
		t.Fatal("expected request id error")
	}
}

func TestRequestJSONRoundTrip(t *testing.T) {
	in := Request{
		ProtocolVersion: ProtocolVersion,
		RequestID: "req-1",
		Method: "ask",
		Config: map[string]any{"endpoint": "http://example"},
		Questions: map[string]decision.Question{
			"route": {Type: decision.QChoice, Instructions: "choose"},
		},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out Request
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.ProtocolVersion != in.ProtocolVersion || out.RequestID != in.RequestID || out.Method != in.Method {
		t.Fatalf("round trip mismatch: %#v", out)
	}
	if out.Questions["route"].Type != decision.QChoice {
		t.Fatalf("question mismatch: %#v", out.Questions)
	}
}
