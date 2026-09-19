package capability

import (
	"fmt"
	"github.com/faizahmd2/diagnos/internal/capability/spec"
	"github.com/faizahmd2/diagnos/internal/contract"
	"regexp"
	"sort"
)

// Capability is the public capability definition.
type Capability = spec.Capability

// ParseInput is the public capability parser input.
type ParseInput = spec.ParseInput

// Registry stores the legal capability graph.
type Registry struct{ items map[string]Capability }

var idRE = regexp.MustCompile("^[a-z]+(\\.[a-z_]+)+$")

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{items: map[string]Capability{}} }

// Register adds a capability.
func (r *Registry) Register(c Capability) error {
	if !idRE.MatchString(c.ID) {
		return fmt.Errorf("invalid capability id %q", c.ID)
	}
	if c.Reads == nil || c.Parse == nil {
		return fmt.Errorf("capability %q incomplete", c.ID)
	}
	if _, ok := r.items[c.ID]; ok {
		return fmt.Errorf("duplicate capability %q", c.ID)
	}
	r.items[c.ID] = c
	return nil
}

// Get returns a capability by ID.
func (r *Registry) Get(id string) (Capability, bool) { c, ok := r.items[id]; return c, ok }

// All returns all capabilities sorted by ID.
func (r *Registry) All() []Capability {
	out := make([]Capability, 0, len(r.items))
	for _, c := range r.items {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// ForLevel returns capabilities at a level.
func (r *Registry) ForLevel(l contract.Level) []Capability {
	out := []Capability{}
	for _, c := range r.items {
		if c.Level == l {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// ForDimension returns capabilities for a dimension.
func (r *Registry) ForDimension(d contract.Dimension) []Capability {
	out := []Capability{}
	for _, c := range r.items {
		if c.Dimension == d {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
