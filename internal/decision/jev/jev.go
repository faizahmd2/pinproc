package jev

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/faizahmd2/vm-native-diagnos/internal/decision"
	"io"
	"net/http"
	"strings"
	"time"
)

// Provider calls TypeSafe System One using the normalized decision contract.
type Provider struct {
	baseURL, model, key string
	timeout             time.Duration
	client              *http.Client
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
	return &Provider{strings.TrimRight(baseURL, "/"), model, key, timeout, &http.Client{}}
}

// Name returns the provider name.
func (p *Provider) Name() string { return "jev" }

// Ask submits one bounded question set and retries transient failures.
func (p *Provider) Ask(ctx context.Context, state any, q map[string]decision.Question) (map[string]decision.Answer, error) {
	if p.key == "" {
		return nil, fmt.Errorf("TYPESAFE_API_KEY is not configured")
	}
	body, e := json.Marshal(map[string]any{"state": state, "model": p.model, "questions": q})
	if e != nil {
		return nil, e
	}
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		c, cancel := context.WithTimeout(ctx, p.timeout)
		req, e := http.NewRequestWithContext(c, http.MethodPost, p.baseURL+"/v1/systemone", strings.NewReader(string(body)))
		if e != nil {
			cancel()
			return nil, e
		}
		req.Header.Set("Authorization", "Bearer "+p.key)
		req.Header.Set("Content-Type", "application/json")
		res, e := p.client.Do(req)
		if e == nil {
			data, er := io.ReadAll(io.LimitReader(res.Body, 8<<20))
			res.Body.Close()
			cancel()
			if er != nil {
				return nil, er
			}
			if res.StatusCode >= 200 && res.StatusCode < 300 {
				return parse(data)
			}
			last = fmt.Errorf("jev HTTP %d: %s", res.StatusCode, string(data))
			if res.StatusCode != 408 && res.StatusCode != 429 && res.StatusCode < 500 {
				return nil, last
			}
		} else {
			cancel()
			last = e
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
func parse(data []byte) (map[string]decision.Answer, error) {
	var raw struct {
		Answers map[string]struct {
			Type          decision.QuestionType `json:"type"`
			Choice        string                `json:"choice"`
			Noul          float64               `json:"noul"`
			Score         float64               `json:"score"`
			Confidence    float64               `json:"confidence"`
			Probabilities map[string]float64    `json:"probabilities"`
		} `json:"answers"`
	}
	if e := json.Unmarshal(data, &raw); e != nil {
		return nil, e
	}
	out := map[string]decision.Answer{}
	for k, a := range raw.Answers {
		conf := a.Confidence
		if a.Type == decision.QNoul {
			conf = (a.Noul - 0.5) * 2
			if conf < 0 {
				conf = -conf
			}
		}
		out[k] = decision.Answer{Type: a.Type, Noul: a.Noul, Choice: a.Choice, Score: a.Score, Confidence: conf, Probabilities: a.Probabilities}
	}
	return out, nil
}
