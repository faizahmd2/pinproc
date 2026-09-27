package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/faizahmd2/pinproc/internal/decision"
	"github.com/faizahmd2/pinproc/internal/decision/jev"
	"github.com/faizahmd2/pinproc/internal/provider"
)

type request struct {
	ProtocolVersion int `json:"protocol_version"`
	Action string `json:"action"`
	State json.RawMessage `json:"state"`
	Questions map[string]decision.Question `json:"questions"`
	Config map[string]string `json:"config"`
}

type response struct {
	ProtocolVersion int `json:"protocol_version"`
	Answers map[string]decision.Answer `json:"answers,omitempty"`
	Error string `json:"error,omitempty"`
}

func main() {
	var req request
	if err := json.NewDecoder(os.Stdin).Decode(&req); err != nil { writeError(fmt.Errorf("decode request: %w", err)); return }
	if req.ProtocolVersion != provider.ProtocolVersion { writeError(fmt.Errorf("unsupported protocol version %d", req.ProtocolVersion)); return }
	if req.Action != "ask" { writeError(fmt.Errorf("unsupported action %q", req.Action)); return }
	key := req.Config["api_key"]
	if key == "" { writeError(fmt.Errorf("api_key is not configured")); return }
	timeout := 10 * time.Second
	if raw := req.Config["timeout"]; raw != "" { if parsed, err := time.ParseDuration(raw); err == nil && parsed > 0 { timeout = parsed } }
	p := jev.New(req.Config["base_url"], req.Config["model"], key, timeout)
	answers, err := p.Ask(context.Background(), req.State, req.Questions)
	if err != nil { writeError(err); return }
	_ = json.NewEncoder(os.Stdout).Encode(response{ProtocolVersion: provider.ProtocolVersion, Answers: answers})
}

func writeError(err error) {
	_ = json.NewEncoder(os.Stdout).Encode(response{ProtocolVersion: provider.ProtocolVersion, Error: err.Error()})
}