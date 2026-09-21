package jev

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/faizahmd2/pinproc/internal/decision"
)

// Provider adapts pinproc's provider-neutral decision contract to the TypeSafe
// System One HTTP contract. No TypeSafe wire types escape this package.
type Provider struct {
	baseURL string
	model   string
	key     string
	timeout time.Duration
	client  *http.Client
}

// New creates a Jev provider.
func New(baseURL, model, key string, timeout time.Duration) *Provider {
	if baseURL == "" {
		baseURL = "https://api.typesafe.ai"
	}
	if model == "" {
		model = "jev-latest"
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Provider{
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		key:     key,
		timeout: timeout,
		client:  &http.Client{},
	}
}

// Name returns the provider name.
func (p *Provider) Name() string { return "jev" }

// Ask submits a bounded question set using the TypeSafe System One wire format.
func (p *Provider) Ask(ctx context.Context, state any, questions map[string]decision.Question) (map[string]decision.Answer, error) {
	if p.key == "" {
		return nil, fmt.Errorf("decision API key is not configured")
	}

	wireQuestions, err := encodeQuestions(questions)
	if err != nil {
		return nil, err
	}

	body, err := json.Marshal(request{
		State:     state,
		Model:     p.model,
		Questions: wireQuestions,
	})
	if err != nil {
		return nil, err
	}

	var last error
	for attempt := 0; attempt < 3; attempt++ {
		reqCtx, cancel := context.WithTimeout(ctx, p.timeout)
		req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, p.baseURL+"/v1/systemone", strings.NewReader(string(body)))
		if err != nil {
			cancel()
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+p.key)
		req.Header.Set("Content-Type", "application/json")

		res, err := p.client.Do(req)
		if err == nil {
			data, readErr := io.ReadAll(io.LimitReader(res.Body, 8<<20))
			res.Body.Close()
			cancel()
			if readErr != nil {
				return nil, readErr
			}
			if res.StatusCode >= 200 && res.StatusCode < 300 {
				return decodeResponse(data, questions)
			}
			last = fmt.Errorf("jev HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(data)))
			if res.StatusCode != http.StatusRequestTimeout && res.StatusCode != http.StatusTooManyRequests && res.StatusCode < 500 {
				return nil, last
			}
		} else {
			cancel()
			last = err
		}

		if attempt < 2 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(250*(1<<attempt)) * time.Millisecond):
			}
		}
	}
	return nil, last
}

type request struct {
	State     any                     `json:"state"`
	Model     string                  `json:"model"`
	Questions map[string]wireQuestion `json:"questions"`
}

type wireQuestion struct {
	Type         string `json:"type"`
	Instructions string `json:"instructions"`
	Criteria     any    `json:"criteria,omitempty"`
}

func encodeQuestions(in map[string]decision.Question) (map[string]wireQuestion, error) {
	out := make(map[string]wireQuestion, len(in))
	for id, q := range in {
		if id == "" {
			return nil, fmt.Errorf("decision question id is empty")
		}
		wq := wireQuestion{
			Type:         string(q.Type),
			Instructions: q.Instructions,
		}
		switch q.Type {
		case decision.QChoice:
			if len(q.Criteria) < 1 {
				return nil, fmt.Errorf("choice question %q requires criteria", id)
			}
			wq.Criteria = q.Criteria
		case decision.QNoul:
			if len(q.Criteria) > 0 {
				wq.Criteria = q.Criteria
			}
		case decision.QScore:
			if len(q.Levels) < 2 {
				return nil, fmt.Errorf("score question %q requires at least two levels", id)
			}
			wq.Criteria = q.Levels
		default:
			return nil, fmt.Errorf("unsupported decision question type %q", q.Type)
		}
		out[id] = wq
	}
	return out, nil
}

type response struct {
	Answers map[string]json.RawMessage `json:"answers"`
}

type answerEnvelope struct {
	Type string `json:"type"`
}

type choiceAnswer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice"`
	Confidence    float64            `json:"confidence"`
	Probabilities map[string]float64 `json:"probabilities"`
}

type noulAnswer struct {
	Type string  `json:"type"`
	Noul float64 `json:"noul"`
}

type scoreAnswer struct {
	Type          string             `json:"type"`
	Score         float64            `json:"score"`
	Legend        map[string]string  `json:"legend"`
	Confidence    float64            `json:"confidence"`
	Probabilities map[string]float64 `json:"probabilities"`
}

func decodeResponse(data []byte, expected map[string]decision.Question) (map[string]decision.Answer, error) {
	var raw response
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	if raw.Answers == nil {
		return nil, fmt.Errorf("jev response has no answers")
	}

	out := make(map[string]decision.Answer, len(expected))
	for id, q := range expected {
		payload, ok := raw.Answers[id]
		if !ok {
			return nil, fmt.Errorf("jev response omitted answer %q", id)
		}
		var env answerEnvelope
		if err := json.Unmarshal(payload, &env); err != nil {
			return nil, fmt.Errorf("decode answer %q: %w", id, err)
		}
		if env.Type != string(q.Type) {
			return nil, fmt.Errorf("jev answer %q has type %q; expected %q", id, env.Type, q.Type)
		}

		switch q.Type {
		case decision.QChoice:
			var a choiceAnswer
			if err := json.Unmarshal(payload, &a); err != nil {
				return nil, fmt.Errorf("decode choice answer %q: %w", id, err)
			}
			out[id] = decision.Answer{
				Type:          decision.QChoice,
				Choice:        a.Choice,
				Confidence:    a.Confidence,
				Probabilities: a.Probabilities,
			}
		case decision.QNoul:
			var a noulAnswer
			if err := json.Unmarshal(payload, &a); err != nil {
				return nil, fmt.Errorf("decode noul answer %q: %w", id, err)
			}
			out[id] = decision.Answer{
				Type: decision.QNoul,
				Noul: a.Noul,
			}
		case decision.QScore:
			var a scoreAnswer
			if err := json.Unmarshal(payload, &a); err != nil {
				return nil, fmt.Errorf("decode score answer %q: %w", id, err)
			}
			out[id] = decision.Answer{
				Type:          decision.QScore,
				Score:         a.Score,
				Confidence:    a.Confidence,
				Probabilities: a.Probabilities,
				Legend:        a.Legend,
			}
		default:
			return nil, fmt.Errorf("unsupported decision question type %q", q.Type)
		}
	}
	return out, nil
}
