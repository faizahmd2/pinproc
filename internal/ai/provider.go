// V2 access check: Diagnos V2 development branch is writable and connected.
// Package ai is the provider boundary. Investigation code exchanges plain
// canonical prompt/JSON data with this package and never vendor payloads.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Request struct {
	Prompt      string
	MaxTokens   int
	Temperature float64
}
type Response struct {
	JSON      json.RawMessage
	Raw       string
	TokensIn  int
	TokensOut int
}
type Provider interface {
	Name() string
	Complete(context.Context, Request) (Response, error)
}
type Config struct {
	Name, Model, BaseURL, APIKey string
	Timeout                      time.Duration
}

func New(cfg Config) (Provider, error) {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 90 * time.Second
	}
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("DIAGNOS_AI_API_KEY is required for provider %q", cfg.Name)
	}
	if cfg.BaseURL == "" {
		switch cfg.Name {
		case "openai":
			cfg.BaseURL = "https://api.openai.com/v1"
		case "anthropic":
			cfg.BaseURL = "https://api.anthropic.com/v1"
		}
	}
	if cfg.Model == "" || cfg.BaseURL == "" {
		return nil, fmt.Errorf("ai model and base_url are required for provider %q", cfg.Name)
	}
	switch cfg.Name {
	case "openai", "anthropic", "internal":
		return &httpProvider{cfg: cfg, client: &http.Client{Timeout: cfg.Timeout}}, nil
	default:
		return nil, fmt.Errorf("unknown AI provider %q (valid: anthropic, openai, internal)", cfg.Name)
	}
}

type httpProvider struct {
	cfg    Config
	client *http.Client
}

func (p *httpProvider) Name() string { return p.cfg.Name }
func (p *httpProvider) Complete(ctx context.Context, req Request) (Response, error) {
	body, headers, endpoint, err := p.payload(req)
	if err != nil {
		return Response{}, err
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Response{}, err
	}
	r.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	res, err := p.client.Do(r)
	if err != nil {
		return Response{}, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return Response{}, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return Response{}, fmt.Errorf("%s returned HTTP %d: %s", p.Name(), res.StatusCode, string(data))
	}
	text, err := extract(p.Name(), data)
	if err != nil {
		return Response{}, err
	}
	return Response{JSON: json.RawMessage(stripFences(text)), Raw: text}, nil
}
func (p *httpProvider) payload(req Request) ([]byte, map[string]string, string, error) {
	h := map[string]string{}
	base := strings.TrimRight(p.cfg.BaseURL, "/")
	switch p.cfg.Name {
	case "openai":
		h["Authorization"] = "Bearer " + p.cfg.APIKey
		b, e := json.Marshal(map[string]any{"model": p.cfg.Model, "messages": []map[string]string{{"role": "user", "content": req.Prompt}}, "temperature": req.Temperature, "max_tokens": req.MaxTokens})
		return b, h, base + "/chat/completions", e
	case "anthropic":
		h["x-api-key"] = p.cfg.APIKey
		h["anthropic-version"] = "2023-06-01"
		b, e := json.Marshal(map[string]any{"model": p.cfg.Model, "max_tokens": req.MaxTokens, "messages": []map[string]string{{"role": "user", "content": req.Prompt}}})
		return b, h, base + "/messages", e
	default:
		h["Authorization"] = "Bearer " + p.cfg.APIKey
		b, e := json.Marshal(map[string]any{"model": p.cfg.Model, "prompt": req.Prompt, "max_tokens": req.MaxTokens, "temperature": req.Temperature})
		return b, h, base, e
	}
}
func extract(provider string, data []byte) (string, error) {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", err
	}
	switch provider {
	case "openai":
		if choices, ok := raw["choices"].([]any); ok && len(choices) > 0 {
			if c, ok := choices[0].(map[string]any); ok {
				if m, ok := c["message"].(map[string]any); ok {
					if s, ok := m["content"].(string); ok {
						return s, nil
					}
				}
			}
		}
	case "anthropic":
		if parts, ok := raw["content"].([]any); ok && len(parts) > 0 {
			if p, ok := parts[0].(map[string]any); ok {
				if s, ok := p["text"].(string); ok {
					return s, nil
				}
			}
		}
	default:
		for _, key := range []string{"json", "text", "output"} {
			if s, ok := raw[key].(string); ok {
				return s, nil
			}
		}
	}
	return "", fmt.Errorf("%s response contained no text", provider)
}
func stripFences(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "```json")
	value = strings.TrimPrefix(value, "```")
	value = strings.TrimSuffix(value, "```")
	return strings.TrimSpace(value)
}
