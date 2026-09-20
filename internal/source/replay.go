package source

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/faizahmd2/pinproc/internal/contract"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Replay reads recorded fixture snapshots.
type Replay struct {
	dir string
	seq int
}

// NewReplay returns a replay source rooted at a fixture.
func NewReplay(dir string) *Replay { return &Replay{dir: dir} }

// Name returns the replay source name.
func (r *Replay) Name() string { return "replay:" + r.dir }

// Facts reads optional fixture facts.
func (r *Replay) Facts(context.Context) (contract.Facts, error) {
	b, e := os.ReadFile(filepath.Join(r.dir, "meta.json"))
	if e != nil {
		return contract.Facts{}, nil
	}
	var f contract.Facts
	e = json.Unmarshal(b, &f)
	return f, e
}

// Snapshot returns the next recorded snapshot.
func (r *Replay) Snapshot(context.Context, []Read) (Snapshot, error) { return r.next() }

// Sample returns the next two recorded snapshots.
func (r *Replay) Sample(_ context.Context, _ []Read, w time.Duration) (Sample, error) {
	t0, e := r.next()
	if e != nil {
		return Sample{}, e
	}
	t1, e := r.next()
	if e != nil {
		return Sample{}, e
	}
	return Sample{T0: t0, T1: t1, Window: w}, nil
}

// Close is a no-op.
func (r *Replay) Close() error { return nil }

type replayIndex struct {
	At    time.Time `json:"at"`
	Reads []struct {
		Key   string `json:"key"`
		Path  string `json:"path"`
		Error string `json:"error,omitempty"`
	} `json:"reads"`
}

func (r *Replay) next() (Snapshot, error) {
	root := filepath.Join(r.dir, fmt.Sprintf("snapshot-%03d", r.seq))
	r.seq++
	b, e := os.ReadFile(filepath.Join(root, "index.json"))
	if e != nil {
		return Snapshot{}, e
	}
	var idx replayIndex
	if e = json.Unmarshal(b, &idx); e != nil {
		return Snapshot{}, e
	}
	out := Snapshot{At: idx.At, Reads: map[string][]Raw{}}
	for _, it := range idx.Reads {
		rel := strings.TrimPrefix(filepath.Clean(it.Path), string(filepath.Separator))
		data, e := os.ReadFile(filepath.Join(root, "data", rel))
		if e != nil {
			data = nil
		}
		raw := Raw{Key: it.Key, Path: it.Path, Data: data}
		if it.Error != "" {
			raw.Err = errors.New(it.Error)
		}
		out.Reads[it.Key] = append(out.Reads[it.Key], raw)
		out.Bytes += int64(len(data))
	}
	return out, nil
}
