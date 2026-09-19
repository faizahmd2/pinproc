package capability

import (
	"fmt"
	"github.com/faizahmd2/diagnos/internal/contract"
	"sort"
)

// LegalNext returns legal descendants.
func (r *Registry) LegalNext(from string, _ contract.Facts) []string {
	c, ok := r.items[from]
	if !ok {
		return nil
	}
	out := append([]string(nil), c.LeadsTo...)
	sort.Strings(out)
	return out
}

// ValidateGraph validates all edges and rejects cycles.
func (r *Registry) ValidateGraph() error {
	for id, c := range r.items {
		for _, n := range c.LeadsTo {
			if _, ok := r.items[n]; !ok {
				return fmt.Errorf("%s leads to unknown %s", id, n)
			}
		}
		if e := r.visit(id, map[string]bool{}, map[string]bool{}); e != nil {
			return e
		}
	}
	return nil
}
func (r *Registry) visit(id string, temp, perm map[string]bool) error {
	if temp[id] {
		return fmt.Errorf("capability graph cycle at %s", id)
	}
	if perm[id] {
		return nil
	}
	temp[id] = true
	for _, n := range r.items[id].LeadsTo {
		if e := r.visit(n, temp, perm); e != nil {
			return e
		}
	}
	delete(temp, id)
	perm[id] = true
	return nil
}
