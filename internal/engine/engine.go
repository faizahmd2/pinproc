package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/faizahmd2/diagnos/internal/capability"
	"github.com/faizahmd2/diagnos/internal/contract"
	"github.com/faizahmd2/diagnos/internal/decision"
	drules "github.com/faizahmd2/diagnos/internal/decision/rules"
	"github.com/faizahmd2/diagnos/internal/identity"
	"github.com/faizahmd2/diagnos/internal/rules"
	"github.com/faizahmd2/diagnos/internal/source"
)

// Options configures the bounded investigation engine.
type Options struct {
	Source         source.Source
	Registry       *capability.Registry
	Rules          []rules.Rule
	Decision       decision.Provider
	Identity       *identity.Resolver
	Budget         contract.Budget
	Clock          func() time.Time
	Logger         *slog.Logger
	ParallelWidth  int
}

// Request starts one investigation.
type Request struct {
	Host      string
	Trigger   string
	Hint      string
	Dimension contract.Dimension
}

// Engine executes adaptive resource investigations.
type Engine struct{ opt Options }

// New creates an investigation engine.
func New(o Options) *Engine {
	if o.Clock == nil {
		o.Clock = time.Now
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.Budget.MaxDepth == 0 {
		o.Budget = contract.BudgetNormal()
	}
	if o.Rules == nil {
		o.Rules = rules.Default()
	}
	if o.ParallelWidth < 1 {
		o.ParallelWidth = 3
	}
	if o.ParallelWidth > 3 {
		o.ParallelWidth = 3
	}
	return &Engine{opt: o}
}

// Run executes the complete bounded descent.
func (e *Engine) Run(ctx context.Context, req Request) (*contract.Investigation, error) {
	if e.opt.Source == nil || e.opt.Registry == nil || e.opt.Decision == nil {
		return nil, fmt.Errorf("engine requires source, registry and decision")
	}
	start := e.opt.Clock()
	facts, err := e.opt.Source.Facts(ctx)
	if err != nil {
		return nil, err
	}
	inv := &contract.Investigation{
		SchemaVersion: contract.SchemaVersion,
		ID:            fmt.Sprintf("inv-%d", start.UnixNano()),
		Host:          req.Host,
		Trigger:       req.Trigger,
		Hint:          req.Hint,
		StartedAt:     start,
		Budget:        e.opt.Budget,
		Facts:         facts,
		Evidence:      []contract.Evidence{},
		Hypotheses:    []contract.Hypothesis{},
		Path:          []contract.Step{},
	}
	if e.opt.Identity != nil {
		if machine, ie := e.opt.Identity.ResolveMachine(ctx); ie == nil {
			inv.Machine = machine
		} else {
			inv.Limitations = append(inv.Limitations, "machine identity unavailable: "+ie.Error())
		}
	}

	sweep, spent, err := e.sweep(ctx, inv)
	inv.Evidence = append(inv.Evidence, sweep...)
	for i := range inv.Evidence {
		registerObserved(inv, inv.Evidence[i])
	}
	spent.Wall = e.opt.Clock().Sub(start)
	inv.Spent = spent
	if err != nil {
		inv.StopReason = contract.StopError
		inv.Duration = e.opt.Clock().Sub(start)
		return inv, err
	}
	if stop := ShouldStop(e.opt.Clock(), start, inv.Spent, e.opt.Budget); stop != "" {
		inv.StopReason = stop
		inv.Duration = e.opt.Clock().Sub(start)
		inv.Hypotheses = Synthesize(rules.EvalAll(e.opt.Rules, inv.Evidence), inv.Evidence)
		return inv, nil
	}

	signals := rules.EvalAll(e.opt.Rules, inv.Evidence)
	if !hasSevere(signals) && req.Hint == "" && req.Dimension == "" {
		inv.StopReason = contract.StopNoAnomaly
		inv.Duration = e.opt.Clock().Sub(start)
		return inv, nil
	}

	if e.opt.Identity != nil {
		_ = e.opt.Identity.ResolveAll(ctx, inv)
		for i := range inv.Evidence {
			registerObserved(inv, inv.Evidence[i])
		}
	}

	state, _ := MarshalState(inv, signals, inv.Path)
	ans, err := e.opt.Decision.Ask(ctx, json.RawMessage(state), decision.AssessQuestions())
	inv.Spent.DecisionCalls++
	if err != nil {
		e.opt.Logger.Warn("decision provider failed; falling back", "error", err)
		ans, _ = drules.New().Ask(ctx, json.RawMessage(state), decision.AssessQuestions())
	}
	choice := ""
	if a, ok := ans["primary_dimension"]; ok {
		choice = a.Choice
	}
	inv.Path = append(inv.Path, contract.Step{
		Depth:      contract.L1Machine,
		Capability: "machine.sweep",
		Scope:      "machine",
		DecidedBy:  "model:" + e.opt.Decision.Name(),
		Reason:     "primary=" + choice,
		Bytes:      inv.Spent.Bytes,
		Duration:   e.opt.Clock().Sub(start),
	})

	front := e.initialFrontier(signals, choice, req.Dimension)
	visited := map[string]bool{}
	for len(front) > 0 {
		if stop := ShouldStop(e.opt.Clock(), start, inv.Spent, e.opt.Budget); stop != "" {
			inv.StopReason = stop
			break
		}
		take := front
		if len(take) > e.opt.ParallelWidth {
			take = take[:e.opt.ParallelWidth]
		}
		front = front[len(take):]
		valid := make([]contract.Candidate, 0, len(take))
		var reads []source.Read

		for _, c := range take {
			cap, ok := e.opt.Registry.Get(c.Capability)
			if !ok {
				continue
			}
			if err := capability.ValidateScope(cap, c.Scope, inv); err != nil {
				inv.Path = append(inv.Path, contract.Step{
					Depth:      cap.Level,
					Capability: c.Capability,
					Scope:      c.Scope.ID,
					DecidedBy:  "model:scope_validation",
					Err:        "scope rejected: " + err.Error(),
				})
				inv.IdentityGaps++
				continue
			}
			valid = append(valid, c)
			reads = append(reads, cap.Reads(c.Scope, inv.Facts)...)
		}
		if len(valid) == 0 {
			inv.StopReason = contract.StopDeadEnd
			break
		}

		reads = dedupReads(reads)
		s, err := e.opt.Source.Sample(ctx, reads, e.opt.Budget.SampleWindow)
		if err != nil {
			inv.StopReason = contract.StopError
			inv.Duration = e.opt.Clock().Sub(start)
			return inv, err
		}
		readBytes := s.T0.Bytes + s.T1.Bytes
		if e.opt.Budget.MaxBytes > 0 && inv.Spent.Bytes+readBytes > e.opt.Budget.MaxBytes {
			for _, c := range valid {
				c.Reason = "budget:bytes"
				inv.NotInvestigated = appendCandidateUnique(inv.NotInvestigated, c)
			}
			inv.StopReason = contract.StopBudgetBytes
			break
		}

		newEv := []contract.Evidence{}
		for _, c := range valid {
			cap, _ := e.opt.Registry.Get(c.Capability)
			ev, pe := cap.Parse(capability.ParseInput{
				Scope:  c.Scope,
				Facts:  inv.Facts,
				Sample: s,
				Window: s.Window,
				Prior:  inv.Evidence,
			})
			if pe != nil {
				inv.Path = append(inv.Path, contract.Step{
					Depth:      cap.Level,
					Capability: cap.ID,
					Scope:      c.Scope.ID,
					DecidedBy:  "engine",
					Err:        pe.Error(),
					Bytes:      readBytes,
				})
				continue
			}
			newEv = append(newEv, ev)
			inv.Path = append(inv.Path, contract.Step{
				Depth:      cap.Level,
				Capability: cap.ID,
				Scope:      c.Scope.ID,
				DecidedBy:  reasonFor(c),
				Reason:     c.Reason,
				Bytes:      readBytes,
				Duration:   s.Window,
			})
		}
		inv.Evidence = append(inv.Evidence, newEv...)
		for _, ev := range newEv {
			registerObserved(inv, ev)
		}
		inv.Spent.Depth = maxDepth(inv.Spent.Depth, newEv)
		inv.Spent.Steps += len(newEv)
		inv.Spent.Bytes += readBytes
		inv.Spent.Wall = e.opt.Clock().Sub(start)

		signals = rules.EvalAll(e.opt.Rules, inv.Evidence)
		legal := e.nextCandidates(valid, inv)
		if len(legal) == 0 {
			inv.StopReason = contract.StopSufficientEvidence
			break
		}
		state, _ = MarshalState(inv, signals, inv.Path)
		answers, err := e.opt.Decision.Ask(ctx, json.RawMessage(state), decision.NextQuestions(candidateKeys(legal)))
		inv.Spent.DecisionCalls++
		if err != nil {
			answers, _ = drules.New().Ask(ctx, json.RawMessage(state), decision.NextQuestions(candidateKeys(legal)))
		}
		if a, ok := answers["explains_anomaly"]; ok && a.Noul >= 0.75 && a.Confidence >= 0.5 {
			inv.StopReason = contract.StopSufficientEvidence
			break
		}
		if a, ok := answers["deeper_warranted"]; ok && a.Noul < 0.5 && a.Confidence >= 0.5 {
			inv.StopReason = contract.StopSufficientEvidence
			break
		}
		selected := "stop"
		if a, ok := answers["next_capability"]; ok && a.Choice != "" {
			selected = a.Choice
		}
		for _, c := range legal {
			if candidateKey(c) == selected && !visited[candidateKey(c)] {
				visited[candidateKey(c)] = true
				front = append(front, c)
				break
			}
		}
		if selected == "stop" || len(front) == 0 {
			inv.StopReason = contract.StopSufficientEvidence
			break
		}
	}

	if inv.StopReason == "" {
		inv.StopReason = contract.StopSufficientEvidence
	}
	inv.NotInvestigated = mergeUnvisited(inv.NotInvestigated, front)
	inv.Hypotheses = Synthesize(signals, inv.Evidence)
	inv.Spent.Wall = e.opt.Clock().Sub(start)
	inv.Duration = e.opt.Clock().Sub(start)
	return inv, nil
}

func (e *Engine) sweep(ctx context.Context, inv *contract.Investigation) ([]contract.Evidence, contract.Spend, error) {
	caps := e.opt.Registry.ForLevel(contract.L1Machine)
	reads := unionReads(caps, inv.Facts)
	s, err := e.opt.Source.Sample(ctx, reads, e.opt.Budget.SampleWindow)
	if err != nil {
		return nil, contract.Spend{}, err
	}
	out := make([]contract.Evidence, 0, len(caps))
	for _, c := range caps {
		ev, pe := c.Parse(capability.ParseInput{
			Scope:  contract.Entity{Kind: contract.EntityMachine, ID: "machine"},
			Facts:  inv.Facts,
			Sample: s,
			Window: s.Window,
			Prior:  out,
		})
		if pe != nil {
			e.opt.Logger.Warn("capability parse failed", "capability", c.ID, "error", pe)
			continue
		}
		out = append(out, ev)
	}
	return out, contract.Spend{
		Depth: 1,
		Steps: len(out),
		Bytes: s.T0.Bytes + s.T1.Bytes,
	}, nil
}

func registerObserved(inv *contract.Investigation, ev contract.Evidence) {
	if inv == nil || ev.Entity.ID == "" {
		return
	}
	for _, entity := range inv.ObservedEntities {
		if entity.Kind == ev.Entity.Kind && entity.ID == ev.Entity.ID && entity.ParentID == ev.Entity.ParentID {
			return
		}
	}
	inv.ObservedEntities = append(inv.ObservedEntities, ev.Entity)
	sort.SliceStable(inv.ObservedEntities, func(i, j int) bool {
		a, b := inv.ObservedEntities[i], inv.ObservedEntities[j]
		if a.Kind != b.Kind {
			return string(a.Kind) < string(b.Kind)
		}
		if a.ID != b.ID {
			return a.ID < b.ID
		}
		return a.ParentID < b.ParentID
	})
}

func unionReads(caps []capability.Capability, f contract.Facts) []source.Read {
	m := map[string]source.Read{}
	for _, c := range caps {
		for _, r := range c.Reads(contract.Entity{Kind: contract.EntityMachine, ID: "machine"}, f) {
			m[readKey(r)] = r
		}
	}
	return sortedReads(m)
}

func dedupReads(in []source.Read) []source.Read {
	m := map[string]source.Read{}
	for _, r := range in {
		m[readKey(r)] = r
	}
	return sortedReads(m)
}

func sortedReads(m map[string]source.Read) []source.Read {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]source.Read, 0, len(keys))
	for _, k := range keys {
		out = append(out, m[k])
	}
	return out
}

func readKey(r source.Read) string {
	return fmt.Sprintf("%d|%s|%s|%d|%t", r.Kind, r.Key, r.Path, r.MaxBytes, r.Optional)
}

func hasSevere(s []rules.Signal) bool {
	for _, x := range s {
		if x.Severity >= 2 {
			return true
		}
	}
	return false
}

func maxDepth(cur int, ev []contract.Evidence) int {
	for _, e := range ev {
		if int(e.Level) > cur {
			cur = int(e.Level)
		}
	}
	return cur
}

func reasonFor(c contract.Candidate) string {
	if c.Forced {
		return "rule:forced"
	}
	return "model:" + c.Capability
}

func candidateKey(c contract.Candidate) string {
	return c.Capability + "|" + string(c.Scope.Kind) + "|" + c.Scope.ID + "|" + c.Scope.ParentID
}

func candidateKeys(c []contract.Candidate) []string {
	out := make([]string, len(c))
	for i, x := range c {
		out[i] = candidateKey(x)
	}
	return out
}

func (e *Engine) initialFrontier(signals []rules.Signal, choice string, pin contract.Dimension) []contract.Candidate {
	out := []contract.Candidate{}
	seen := map[string]bool{}
	want := choice
	if pin != "" {
		want = string(pin)
	}
	for _, s := range signals {
		if !s.Force {
			continue
		}
		id := machineCap(s.Dimension)
		if id == "" {
			continue
		}
		c, ok := e.opt.Registry.Get(id)
		if !ok {
			continue
		}
		candidate := contract.Candidate{
			Capability: id,
			Scope:      contract.Entity{Kind: c.Accepts, ID: "machine"},
			Score:      float64(s.Severity) + 10,
			Forced:     true,
			Reason:     s.Statement,
		}
		out = append(out, candidate)
		seen[candidateKey(candidate)] = true
	}
	if want != "" && want != "none" {
		id := machineCap(contract.Dimension(want))
		if c, ok := e.opt.Registry.Get(id); ok && id != "" {
			candidate := contract.Candidate{Capability: id, Scope: contract.Entity{Kind: c.Accepts, ID: "machine"}, Score: 1}
			if !seen[candidateKey(candidate)] {
				out = append(out, candidate)
			}
		}
	}
	contract.NormalizeCandidates(out)
	return out
}

func machineCap(d contract.Dimension) string {
	switch d {
	case contract.DimensionCPU:
		return "machine.processes"
	case contract.DimensionMemory:
		return "machine.memory"
	case contract.DimensionIO:
		return "machine.io"
	case contract.DimensionNetwork:
		return "machine.network"
	case contract.DimensionLimits:
		return "machine.limits"
	default:
		return ""
	}
}

func (e *Engine) nextCandidates(done []contract.Candidate, inv *contract.Investigation) []contract.Candidate {
	out := []contract.Candidate{}
	seen := map[string]bool{}
	for _, parent := range done {
		for _, id := range e.opt.Registry.LegalNext(parent.Capability, inv.Facts) {
			c, ok := e.opt.Registry.Get(id)
			if !ok {
				continue
			}
			for _, scope := range e.deriveScopes(c.Accepts, parent, inv) {
				candidate := contract.Candidate{Capability: id, Scope: scope, Score: 1}
				key := candidateKey(candidate)
				if !seen[key] {
					seen[key] = true
					out = append(out, candidate)
				}
			}
		}
	}
	contract.NormalizeCandidates(out)
	return out
}

func (e *Engine) deriveScopes(kind contract.EntityKind, parent contract.Candidate, inv *contract.Investigation) []contract.Entity {
	if kind == contract.EntityMachine {
		return []contract.Entity{{Kind: kind, ID: "machine"}}
	}
	out := []contract.Entity{}
	seen := map[string]bool{}
	for _, ev := range inv.Evidence {
		switch {
		case kind == contract.EntityProcess && ev.Capability == "machine.processes":
			b, _ := json.Marshal(ev.Facts)
			var v struct {
				Rows []struct {
					PID int64
				}
			}
			if json.Unmarshal(b, &v) == nil {
				for _, row := range v.Rows {
					id := fmt.Sprintf("pid:%d", row.PID)
					if !seen[id] {
						seen[id] = true
						out = append(out, contract.Entity{Kind: kind, ID: id})
					}
				}
			}
		case kind == contract.EntityThread && ev.Capability == "process.cpu":
			b, _ := json.Marshal(ev.Facts)
			var v struct {
				ThreadIDs []string
			}
			if json.Unmarshal(b, &v) == nil {
				for _, id := range v.ThreadIDs {
					if !seen[id] {
						seen[id] = true
						out = append(out, contract.Entity{Kind: kind, ID: id, ParentID: parent.Scope.ID})
					}
				}
			}
		case kind == contract.EntityCgroup && ev.Entity.Service != nil:
			if path := strings.Trim(strings.TrimSpace(ev.Entity.Service.CgroupPath), "/"); path != "" {
				id := "cgroup:" + path
				if !seen[id] {
					seen[id] = true
					out = append(out, contract.Entity{Kind: kind, ID: id, ParentID: ev.Entity.ID})
				}
			}
		}
	}
	return out
}

func appendCandidateUnique(dst []contract.Candidate, c contract.Candidate) []contract.Candidate {
	for _, existing := range dst {
		if candidateKey(existing) == candidateKey(c) {
			return dst
		}
	}
	return append(dst, c)
}

func mergeUnvisited(dst, extra []contract.Candidate) []contract.Candidate {
	for _, c := range extra {
		dst = appendCandidateUnique(dst, c)
	}
	contract.NormalizeCandidates(dst)
	return dst
}
