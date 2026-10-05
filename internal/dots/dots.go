// Package dots is the in-memory ring buffer of "incident dots" — cheap,
// timestamped samples captured while a dimension is under watch. It holds the
// pre-incident history so that when an incident is confirmed the report can show
// how it built up and who was climbing, without ever writing to disk until
// capture. Discard is effective: it releases per-dot references so a watch that
// never escalates leaves no memory behind.
package dots

import (
	"time"

	"github.com/faizahmd2/pinproc/internal/contract"
)

// Contender is one process's share of the watched resource at a moment in time.
type Contender struct {
	PID   int     `json:"pid"`
	Comm  string  `json:"comm"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}

// Dot is one sample on the incident timeline for the armed dimension.
type Dot struct {
	At    time.Time   `json:"at"`
	Level float64     `json:"level"` // headline level (util% or used% for the dim)
	PSI   float64     `json:"psi"`   // PSI some avg10 at this moment
	Top   []Contender `json:"top,omitempty"`
}

// Ring is a fixed-capacity circular buffer of Dots. Not safe for concurrent use;
// the single monitor goroutine owns it.
type Ring struct {
	dim   contract.Dimension
	buf   []Dot
	head  int // index of the next write
	count int
}

// NewRing creates a ring sized to hold at most capacity dots. capacity < 1 is
// treated as 1. The backing array is allocated once and reused across watches.
func NewRing(capacity int) *Ring {
	if capacity < 1 {
		capacity = 1
	}
	return &Ring{buf: make([]Dot, capacity)}
}

// Cap returns the maximum number of dots retained.
func (r *Ring) Cap() int { return len(r.buf) }

// Len returns the number of dots currently held.
func (r *Ring) Len() int { return r.count }

// Dim returns the dimension this ring is currently armed for.
func (r *Ring) Dim() contract.Dimension { return r.dim }

// Arm sets the dimension under watch and clears any prior contents.
func (r *Ring) Arm(dim contract.Dimension) {
	r.Reset()
	r.dim = dim
}

// Add appends a dot, overwriting the oldest when full. The overwritten dot's
// slice reference is dropped (reassigned), so it becomes eligible for GC.
func (r *Ring) Add(d Dot) {
	r.buf[r.head] = d
	r.head = (r.head + 1) % len(r.buf)
	if r.count < len(r.buf) {
		r.count++
	}
}

// Snapshot returns the dots oldest-first as a new slice. The Dots themselves are
// shallow-copied (their Top slices are shared), which is safe because the caller
// receives them at capture time and the ring is reset afterwards.
func (r *Ring) Snapshot() []Dot {
	out := make([]Dot, 0, r.count)
	start := (r.head - r.count + len(r.buf)) % len(r.buf)
	for i := 0; i < r.count; i++ {
		out = append(out, r.buf[(start+i)%len(r.buf)])
	}
	return out
}

// Reset discards all dots and releases their references. The backing array is
// kept (fixed capacity) but every slot is zeroed so no Contender slice is
// retained — a watch that never escalates leaks nothing.
func (r *Ring) Reset() {
	for i := range r.buf {
		r.buf[i] = Dot{}
	}
	r.head = 0
	r.count = 0
	r.dim = ""
}
