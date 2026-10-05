// Package aggregate folds individual processes into the service that owns them, so
// a report can state definitively "this service, these components, caused the
// distress" instead of pointing at one transient worker PID. Grouping is by a
// stable key (service cgroup / container / process group) supplied by the caller;
// per-component shares are kept alongside the service total so churning children
// are still attributed to their parent service.
package aggregate

import "sort"

// Proc is one process's contribution to a dimension, already tagged with the
// stable service key and display name it belongs to.
type Proc struct {
	PID     int
	Comm    string
	Key     string
	Display string
	Value   float64
}

// Component is one process's share within its service.
type Component struct {
	PID   int
	Comm  string
	Value float64
	Pct   float64 // percentage of the service total
}

// Service is an aggregated owner of a resource dimension.
type Service struct {
	Key        string
	Display    string
	Procs      int
	Value      float64
	Components []Component
}

// Group sums procs by their Key, returns services sorted by total value
// (descending), each with up to maxComponents top components and their share.
func Group(procs []Proc, maxComponents int) []Service {
	byKey := map[string]*Service{}
	order := []string{}
	for _, p := range procs {
		s := byKey[p.Key]
		if s == nil {
			s = &Service{Key: p.Key, Display: p.Display}
			byKey[p.Key] = s
			order = append(order, p.Key)
		}
		if s.Display == "" {
			s.Display = p.Display
		}
		s.Procs++
		s.Value += p.Value
		s.Components = append(s.Components, Component{PID: p.PID, Comm: p.Comm, Value: p.Value})
	}
	out := make([]Service, 0, len(order))
	for _, k := range order {
		s := byKey[k]
		sort.SliceStable(s.Components, func(i, j int) bool { return s.Components[i].Value > s.Components[j].Value })
		for i := range s.Components {
			if s.Value > 0 {
				s.Components[i].Pct = s.Components[i].Value / s.Value * 100
			}
		}
		if maxComponents > 0 && len(s.Components) > maxComponents {
			s.Components = s.Components[:maxComponents]
		}
		out = append(out, *s)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Value > out[j].Value })
	return out
}
