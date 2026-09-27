package provider

import (
	"encoding/json"
	"fmt"

	"github.com/faizahmd2/pinproc/internal/decision"
)

// ProtocolVersion is the stable wire contract between pinproc and provider executables.
const ProtocolVersion = 1

// Request is one provider invocation.
type Request struct {
	ProtocolVersion int                          `json:"protocol_version"`
	RequestID       string                       `json:"request_id"`
	Method          string                       `json:"method"`
	Config          map[string]any               `json:"config,omitempty"`
	State           json.RawMessage              `json:"state,omitempty"`
	Questions       map[string]decision.Question `json:"questions,omitempty"`
}

// Response is one provider result.
type Response struct {
	ProtocolVersion int                        `json:"protocol_version"`
	RequestID       string                     `json:"request_id"`
	Answers         map[string]decision.Answer `json:"answers,omitempty"`
	Error           string                     `json:"error,omitempty"`
}

func (r Response) Validate(requestID string) error {
	if r.ProtocolVersion != ProtocolVersion {
		return fmt.Errorf("unsupported provider protocol version %d", r.ProtocolVersion)
	}
	if r.RequestID != requestID {
		return fmt.Errorf("provider response request_id %q does not match %q", r.RequestID, requestID)
	}
	if r.Error != "" {
		return fmt.Errorf("provider error: %s", r.Error)
	}
	return nil
}
