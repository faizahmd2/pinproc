package decision

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// RecordItem is a normalized provider request/response pair.
type RecordItem struct {
	Questions map[string]Question `json:"questions"`
	State     any                 `json:"state"`
	Answers   map[string]Answer   `json:"answers"`
	Error     string              `json:"error,omitempty"`
}

// RecordingProvider wraps a provider and writes decisions.json.
type RecordingProvider struct {
	inner Provider
	dir   string
	mu    sync.Mutex
	items []RecordItem
}

// NewRecordingProvider creates a recording decision provider.
func NewRecordingProvider(inner Provider, dir string) *RecordingProvider {
	return &RecordingProvider{inner: inner, dir: dir}
}

// Name returns the wrapped provider name.
func (r *RecordingProvider) Name() string { return "record:" + r.inner.Name() }

// Ask records a decision request and response.
func (r *RecordingProvider) Ask(ctx context.Context, state any, q map[string]Question) (map[string]Answer, error) {
	a, e := r.inner.Ask(ctx, state, q)
	it := RecordItem{Questions: q, State: state, Answers: a}
	if e != nil {
		it.Error = e.Error()
	}
	r.mu.Lock()
	r.items = append(r.items, it)
	snap := append([]RecordItem(nil), r.items...)
	r.mu.Unlock()
	if b, er := json.MarshalIndent(snap, "", "  "); er == nil {
		_ = os.MkdirAll(r.dir, 0755)
		_ = os.WriteFile(filepath.Join(r.dir, "decisions.json"), b, 0644)
	}
	return a, e
}
