package source

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/faizahmd2/diagnos/internal/contract"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Recorder persists source snapshots as replayable fixtures.
type Recorder struct {
	inner Source
	dir   string
	seq   int
}

// NewRecorder wraps a source with a fixture recorder.
func NewRecorder(inner Source, dir string) *Recorder { return &Recorder{inner: inner, dir: dir} }

// Name returns the recorder source name.
func (r *Recorder) Name() string { return "record:" + r.inner.Name() }

// Facts delegates to the wrapped source.
func (r *Recorder) Facts(c context.Context) (contract.Facts, error) { return r.inner.Facts(c) }

// Snapshot records the next snapshot.
func (r *Recorder) Snapshot(c context.Context, reads []Read) (Snapshot, error) {
	s, e := r.inner.Snapshot(c, reads)
	if e != nil {
		return s, e
	}
	return s, r.write(s)
}

// Sample records both snapshots.
func (r *Recorder) Sample(c context.Context, reads []Read, w time.Duration) (Sample, error) {
	s, e := r.inner.Sample(c, reads, w)
	if e != nil {
		return s, e
	}
	if e = r.write(s.T0); e != nil {
		return s, e
	}
	if e = r.write(s.T1); e != nil {
		return s, e
	}
	return s, nil
}

// Close closes the wrapped source.
func (r *Recorder) Close() error { return r.inner.Close() }

type indexItem struct {
	Key   string `json:"key"`
	Path  string `json:"path"`
	Error string `json:"error,omitempty"`
	Size  int    `json:"bytes"`
}

func (r *Recorder) write(s Snapshot) error {
	if e := os.MkdirAll(r.dir, 0755); e != nil {
		return e
	}
	root := filepath.Join(r.dir, fmt.Sprintf("snapshot-%03d", r.seq))
	r.seq++
	if e := os.MkdirAll(root, 0755); e != nil {
		return e
	}
	keys := make([]string, 0, len(s.Reads))
	for k := range s.Reads {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	items := make([]indexItem, 0)
	for _, k := range keys {
		for _, raw := range s.Reads[k] {
			rel := strings.TrimPrefix(filepath.Clean(raw.Path), string(filepath.Separator))
			out := filepath.Join(root, "data", rel)
			if e := os.MkdirAll(filepath.Dir(out), 0755); e != nil {
				return e
			}
			if e := os.WriteFile(out, raw.Data, 0644); e != nil {
				return e
			}
			it := indexItem{Key: k, Path: raw.Path, Size: len(raw.Data)}
			if raw.Err != nil {
				it.Error = raw.Err.Error()
			}
			items = append(items, it)
		}
	}
	b, e := json.MarshalIndent(struct {
		At    time.Time   `json:"at"`
		Reads []indexItem `json:"reads"`
	}{s.At, items}, "", "  ")
	if e != nil {
		return e
	}
	return os.WriteFile(filepath.Join(root, "index.json"), b, 0644)
}
