package engine

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/faizahmd2/pinproc/internal/contract"
	"github.com/faizahmd2/pinproc/internal/rules"
)

// Synthesize builds a bounded set of explicitly graded hypotheses. focus is the
// dimension the operator explicitly asked about (via hint/dimension), used to
// attribute an owner even when no kernel pressure signal fired.
func Synthesize(focus contract.Dimension, signals []rules.Signal, ev []contract.Evidence, limit ...int) []contract.Hypothesis {
	maxFindings := 5
	if len(limit) > 0 && limit[0] > 0 {
		maxFindings = limit[0]
	}

	// Owner attribution: promote a machine-level resource signal to the process
	// that actually owns the resource, with the measured proof and an
	// application-oriented next step. These become the lead findings.
	owners := ownerFindings(focus, signals, ev)
	ownedDims := map[contract.Dimension]bool{}
	for _, h := range owners {
		ownedDims[h.Dimension] = true
	}

	var out []contract.Hypothesis
	out = append(out, owners...)
	for _, s := range signals {
		entity := contract.Entity{Kind: contract.EntityMachine, ID: "machine"}
		for _, e := range ev {
			for _, id := range s.Support {
				if e.ID == id {
					entity = e.Entity
				}
			}
		}
		// When we have attributed this dimension to an owning process, keep the
		// report lean: drop the generic machine-level restatement of it.
		if entity.Kind == contract.EntityMachine && ownedDims[s.Dimension] {
			continue
		}
		out = append(out, contract.Hypothesis{
			ID: "hy-" + s.ID, Statement: s.Statement, Dimension: s.Dimension, Entity: entity,
			Grade: contract.GradeObserved, Support: append([]string{}, s.Support...),
			Confidence: float64(s.Severity) / 4, Source: "rule:" + s.ID,
		})
	}

	// Cross-dimension coincidence: high iowait is often the symptom while I/O saturation is the cause.
	hasIOWait := hasSignal(signals, "cpu.iowait_dominant")
	hasIOSat := hasSignal(signals, "io.saturated")
	if hasIOWait && hasIOSat {
		support := append(signalSupport(signals, "cpu.iowait_dominant"), signalSupport(signals, "io.saturated")...)
		out = append(out, contract.Hypothesis{
			ID: "hy-correlated-io-wait", Statement: "CPU busy time is primarily I/O wait, not compute, while block I/O is saturated",
			Dimension: contract.DimensionIO, Entity: entityForSupport(ev, support), Grade: contract.GradeCorrelated,
			Support: uniqueStrings(support), Confidence: 0.88, Source: "pattern:io_wait_plus_io_saturation",
		})
	}

	// Thread concentration: only synthesize when the top two threads account for most process CPU.
	out = append(out, threadConcentrationHypotheses(ev)...)

	// A saturated machine CPU finding is contradicted when no process has materially elevated user CPU.
	if hasSignal(signals, "cpu.saturated") {
		maxUser := 0.0
		var processEvidence []string
		for _, e := range ev {
			if e.Capability != "process.cpu" {
				continue
			}
			processEvidence = append(processEvidence, e.ID)
			if v, ok := observation(e, "proc.user_pct"); ok && v > maxUser {
				maxUser = v
			}
		}
		if len(processEvidence) > 0 && maxUser <= 20 {
			for i := range out {
				if out[i].Source == "rule:cpu.saturated" {
					out[i].Contradicts = append(out[i].Contradicts, processEvidence...)
				}
			}
		}
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].Confidence > out[j].Confidence })
	if len(out) > maxFindings {
		out = out[:maxFindings]
	}
	return out
}

// ownerFindings attributes the strongest machine-level resource signal to the
// process that owns the resource, citing the measured proof and a concrete,
// application-level next step (never a kernel-level one).
func ownerFindings(focus contract.Dimension, signals []rules.Signal, ev []contract.Evidence) []contract.Hypothesis {
	dim := strongestForcedDimension(signals)
	if dim == "" {
		dim = focus
	}
	if dim == "" {
		return nil
	}
	if dim == contract.DimensionFilesystem {
		return filesystemOwnerFindings(ev)
	}
	capID, metric := "", ""
	switch dim {
	case contract.DimensionCPU, contract.DimensionScheduling:
		capID, metric = "process.cpu", "proc.cpu_pct"
	case contract.DimensionMemory:
		capID, metric = "process.memory", "proc.rss"
	case contract.DimensionIO:
		capID, metric = "process.io", "proc.write_bps"
	default:
		return nil
	}

	var best *contract.Evidence
	bestVal := 0.0
	for i := range ev {
		e := &ev[i]
		if e.Capability != capID {
			continue
		}
		v, ok := observation(*e, metric)
		if dim == contract.DimensionIO { // disk I/O is read + write
			if r, ok2 := observation(*e, "proc.read_bps"); ok2 {
				v += r
				ok = true
			}
		}
		if ok && v >= bestVal {
			bestVal, best = v, e
		}
	}
	if best == nil || bestVal <= 0 {
		return nil
	}

	name := entityName(best.Entity)
	stmt, action := "", ""
	switch dim {
	case contract.DimensionCPU, contract.DimensionScheduling:
		// proc.cpu_pct is a fraction of one core (1.0 = one full core); present
		// it as top-style percent where 100% is one core.
		stmt = name + " is using " + trimFloat(bestVal*100) + "% CPU (" + fmt.Sprintf("%.1f", bestVal) + " cores)"
		action = "Profile or throttle " + name + "; it is the dominant CPU consumer."
	case contract.DimensionMemory:
		stmt = name + " is holding " + humanBytes(bestVal) + " of memory (RSS)"
		action = "Check " + name + " for a leak or cap its memory; it is the dominant consumer."
	case contract.DimensionIO:
		stmt = name + " is driving " + perSec(bestVal) + " of disk I/O"
		action = "Throttle or relocate " + name + "'s I/O; it is the dominant disk user."
	}
	return []contract.Hypothesis{{
		ID: "hy-owner-" + string(dim), Statement: stmt, Dimension: dim, Entity: best.Entity,
		Grade: contract.GradeObserved, Support: []string{best.ID}, Confidence: 0.92,
		Source: "owner:" + string(dim), Action: action,
	}}
}

// filesystemOwnerFindings attributes disk-space pressure to the largest paths,
// so the operator sees what to reclaim rather than a raw "disk 95% full".
func filesystemOwnerFindings(ev []contract.Evidence) []contract.Hypothesis {
	for i := range ev {
		e := &ev[i]
		if e.Capability != "fs.usage" {
			continue
		}
		var f struct {
			Mount          string
			TopDirectories []string
			TopFiles       []string
		}
		b, _ := json.Marshal(e.Facts)
		if json.Unmarshal(b, &f) != nil {
			continue
		}
		proof := ""
		// Skip the mount root itself (it just restates the total); report the
		// largest actual subdirectory or file so the operator knows what to clear.
		for _, d := range f.TopDirectories {
			if p, _ := splitSizedPath(d); p != "" && p != f.Mount {
				proof = humanizeSizedPath(d)
				break
			}
		}
		if proof == "" && len(f.TopFiles) > 0 {
			proof = humanizeSizedPath(f.TopFiles[0])
		}
		if proof == "" {
			continue
		}
		return []contract.Hypothesis{{
			ID: "hy-owner-filesystem", Statement: "Largest space on " + f.Mount + ": " + proof,
			Dimension: contract.DimensionFilesystem, Entity: e.Entity, Grade: contract.GradeObserved,
			Support: []string{e.ID}, Confidence: 0.9, Source: "owner:filesystem",
			Action: "Reclaim or rotate the largest paths shown above (owning service's data/logs), not the filesystem itself.",
		}}
	}
	return nil
}

// splitSizedPath parses "path (N bytes)" into (path, N).
func splitSizedPath(s string) (string, float64) {
	i := strings.LastIndex(s, " (")
	if i < 0 || !strings.HasSuffix(s, " bytes)") {
		return s, 0
	}
	path := s[:i]
	num := strings.TrimSuffix(s[i+2:], " bytes)")
	var n float64
	fmt.Sscanf(num, "%f", &n)
	return path, n
}

// humanizeSizedPath turns "path (N bytes)" into "path (N.N GiB)".
func humanizeSizedPath(s string) string {
	path, n := splitSizedPath(s)
	if n <= 0 {
		return s
	}
	return path + " (" + humanBytes(n) + ")"
}

func strongestForcedDimension(signals []rules.Signal) contract.Dimension {
	best := -1
	var dim contract.Dimension
	for _, s := range signals {
		if s.Force && s.Severity > best {
			best = s.Severity
			dim = s.Dimension
		}
	}
	return dim
}

func entityName(e contract.Entity) string {
	if e.Service != nil && e.Service.Name != "" {
		return e.Service.Name + " (" + e.ID + ")"
	}
	if e.Display != "" {
		return e.Display + " (" + e.ID + ")"
	}
	return e.ID
}

func trimFloat(v float64) string { return fmt.Sprintf("%.0f", v) }

func humanBytes(v float64) string {
	const u = 1024.0
	switch {
	case v >= u*u*u:
		return fmt.Sprintf("%.1f GiB", v/(u*u*u))
	case v >= u*u:
		return fmt.Sprintf("%.0f MiB", v/(u*u))
	case v >= u:
		return fmt.Sprintf("%.0f KiB", v/u)
	default:
		return fmt.Sprintf("%.0f B", v)
	}
}

func perSec(v float64) string { return humanBytes(v) + "/s" }

func threadConcentrationHypotheses(ev []contract.Evidence) []contract.Hypothesis {
	type group struct {
		entity  contract.Entity
		total   float64
		threads []struct {
			id  string
			cpu float64
			ev  string
		}
	}
	groups := map[string]*group{}
	for _, e := range ev {
		if e.Capability != "process.cpu" {
			continue
		}
		total, ok := observation(e, "proc.cpu_pct")
		if !ok || total <= 0 {
			continue
		}
		g := &group{entity: e.Entity, total: total}
		key := e.Entity.ID
		groups[key] = g
	}
	for _, e := range ev {
		if e.Capability != "thread.cpu" {
			continue
		}
		cpu, ok := observation(e, "thread.cpu_pct")
		if !ok {
			continue
		}
		pid := e.Entity.ParentID
		if g := groups[pid]; g != nil {
			g.threads = append(g.threads, struct {
				id  string
				cpu float64
				ev  string
			}{e.Entity.ID, cpu, e.ID})
		}
	}
	var out []contract.Hypothesis
	for _, g := range groups {
		sort.Slice(g.threads, func(i, j int) bool { return g.threads[i].cpu > g.threads[j].cpu })
		if len(g.threads) < 2 {
			continue
		}
		share := (g.threads[0].cpu + g.threads[1].cpu) / g.total
		if share <= 0.70 {
			continue
		}
		name := g.entity.Display
		if name == "" {
			name = g.entity.ID
		}
		out = append(out, contract.Hypothesis{
			ID:        "hy-thread-concentration-" + g.entity.ID,
			Statement: fmt.Sprintf("CPU is concentrated in %d threads of %s", 2, name),
			Dimension: contract.DimensionCPU, Entity: g.entity, Grade: contract.GradeInferred,
			Support: []string{g.threads[0].ev, g.threads[1].ev}, Confidence: share, Source: "pattern:thread_concentration",
		})
	}
	return out
}

func hasSignal(ss []rules.Signal, id string) bool {
	for _, s := range ss {
		if s.ID == id {
			return true
		}
	}
	return false
}
func signalSupport(ss []rules.Signal, id string) []string {
	for _, s := range ss {
		if s.ID == id {
			return s.Support
		}
	}
	return nil
}
func entityForSupport(ev []contract.Evidence, ids []string) contract.Entity {
	for _, e := range ev {
		for _, id := range ids {
			if e.ID == id {
				return e.Entity
			}
		}
	}
	return contract.Entity{Kind: contract.EntityMachine, ID: "machine"}
}
func observation(e contract.Evidence, key string) (float64, bool) {
	for _, o := range e.Observations {
		if o.Key == key {
			return o.Value, true
		}
	}
	return 0, false
}
func uniqueStrings(in []string) []string {
	m := map[string]bool{}
	out := []string{}
	for _, x := range in {
		if x != "" && !m[x] {
			m[x] = true
			out = append(out, x)
		}
	}
	return out
}
