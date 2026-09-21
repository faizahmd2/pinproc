package engine

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/faizahmd2/pinproc/internal/capability"
	"github.com/faizahmd2/pinproc/internal/contract"
	"github.com/faizahmd2/pinproc/internal/decision"
	drules "github.com/faizahmd2/pinproc/internal/decision/rules"
	"github.com/faizahmd2/pinproc/internal/enrich"
	"github.com/faizahmd2/pinproc/internal/identity"
	"github.com/faizahmd2/pinproc/internal/rules"
	"github.com/faizahmd2/pinproc/internal/source"
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
	MaxFindings    int
	DecisionNotice string
	Progress       func(string)
}

// Request starts one investigation.
type Request struct {
	ID        string
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
	if o.MaxFindings < 1 {
		o.MaxFindings = 5
	}
	return &Engine{opt: o}
}

// Run executes the complete bounded descent.
func (e *Engine) Run(ctx context.Context, req Request) (*contract.Investigation, error) {
	wall := e.opt.Budget.MaxWall
	if wall <= 0 || wall > absoluteMaxWall {
		wall = absoluteMaxWall
	}
	ctx, cancel := context.WithTimeout(ctx, wall)
	defer cancel()
	if e.opt.Source == nil || e.opt.Registry == nil || e.opt.Decision == nil {
		return nil, fmt.Errorf("engine requires source, registry and decision")
	}
	start := e.opt.Clock()
	e.progress("started")
	e.opt.Logger.Info("investigation started", "id", req.ID, "host", req.Host, "budget", e.opt.Budget.MaxWall)
	defer func() { e.opt.Logger.Info("investigation finished", "id", req.ID, "elapsed", e.opt.Clock().Sub(start)) }()
	facts, err := e.opt.Source.Facts(ctx)
	if err != nil {
		return nil, err
	}
	inv := &contract.Investigation{
		SchemaVersion: contract.SchemaVersion,
		ID:            req.ID,
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
	if inv.ID == "" {
		inv.ID = fmt.Sprintf("inv-%d", start.UnixNano())
	}
	if e.opt.DecisionNotice != "" {
		addNotice(inv, "decision", e.opt.DecisionNotice)
	}
	if e.opt.Identity != nil {
		if machine, ie := e.opt.Identity.ResolveMachine(ctx); ie == nil {
			inv.Machine = machine
			if isLocalHost(req.Host) && machine.Hostname != "" {
				inv.Host = machine.Hostname
			}
		} else {
			inv.Limitations = append(inv.Limitations, "machine identity unavailable: "+ie.Error())
		}
	}

	e.progress("machine_sweep")
	sweep, spent, err := e.sweep(ctx, inv)
	inv.Evidence = append(inv.Evidence, sweep...)
	for i := range inv.Evidence {
		registerObserved(inv, inv.Evidence[i])
	}
	spent.Wall = e.opt.Clock().Sub(start)
	inv.Spent = spent
	inv.MachineSnapshot = buildMachineSnapshot(inv)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			inv.StopReason = contract.StopBudgetTime
			inv.Hypotheses = Synthesize(rules.EvalAll(e.opt.Rules, inv.Evidence), inv.Evidence, e.opt.MaxFindings)
			inv.Duration = e.opt.Clock().Sub(start)
			return inv, nil
		}
		inv.StopReason = contract.StopError
		inv.Duration = e.opt.Clock().Sub(start)
		return inv, err
	}
	if stop := ShouldStop(e.opt.Clock(), start, inv.Spent, e.opt.Budget); stop != "" {
		inv.StopReason = stop
		inv.Duration = e.opt.Clock().Sub(start)
		inv.Hypotheses = Synthesize(rules.EvalAll(e.opt.Rules, inv.Evidence), inv.Evidence, e.opt.MaxFindings)
		_ = enrich.Attach(ctx, e.opt.Source, inv.Hypotheses)
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

	e.progress("decision")
	state, _ := MarshalState(inv, signals, inv.Path)
	ans, err := e.opt.Decision.Ask(ctx, json.RawMessage(state), decision.AssessQuestions())
	inv.Spent.DecisionCalls++
	decidedBy := "model:" + e.opt.Decision.Name()
	if err != nil {
		e.opt.Logger.Warn("decision provider failed; falling back", "error", err)
		ans, _ = drules.New().Ask(ctx, json.RawMessage(state), decision.AssessQuestions())
		decidedBy = "rules:jev_unavailable"
		addNotice(inv, "decision", "AI decision provider unavailable — "+truncate(err.Error(), 150)+"; using deterministic rules.")
	}
	choice := ""
	if a, ok := ans["primary_dimension"]; ok {
		choice = a.Choice
	}
	inv.Path = append(inv.Path, contract.Step{
		Depth:      contract.L1Machine,
		Capability: "machine.sweep",
		Scope:      "machine",
		DecidedBy:  decidedBy,
		Reason:     "primary=" + choice,
		Bytes:      inv.Spent.Bytes,
		Duration:   e.opt.Clock().Sub(start),
	})

	e.progress("deep_investigation")
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
			if missing := missingRequirement(cap, inv.Facts); missing != "" {
				valid = valid[:len(valid)-1]
				ev := unavailableEvidence(cap, c.Scope, missing)
				inv.Evidence = append(inv.Evidence, ev)
				registerObserved(inv, ev)
				addNotice(inv, cap.ID, missing)
				inv.Path = append(inv.Path, contract.Step{Depth: cap.Level, Capability: cap.ID, Scope: c.Scope.ID, DecidedBy: "engine", Reason: "unavailable", Err: missing})
				continue
			}
			capReads, re := safeReads(cap, c.Scope, inv.Facts)
			if re != nil {
				ev := failureEvidence(cap, c.Scope, "panic", re.Error())
				inv.Evidence = append(inv.Evidence, ev)
				registerObserved(inv, ev)
				addNotice(inv, cap.ID, "capability hit an internal error (bug, not your system) — please report this with code "+panicCode(cap.ID))
				inv.Path = append(inv.Path, contract.Step{Depth: cap.Level, Capability: cap.ID, Scope: c.Scope.ID, DecidedBy: "engine", Err: re.Error()})
				valid = valid[:len(valid)-1]
				continue
			}
			reads = append(reads, capReads...)
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
			if class, detail := sampledFailure(cap, c, s); class != "" {
				ev := failureEvidence(cap, c.Scope, class, detail)
				newEv = append(newEv, ev)
				addNotice(inv, cap.ID, noticeForFailure(cap.ID, c.Scope, class, detail))
				inv.Path = append(inv.Path, contract.Step{Depth: cap.Level, Capability: cap.ID, Scope: c.Scope.ID, DecidedBy: "engine", Err: detail, Bytes: readBytes})
				continue
			}
			ev, pe := safeParse(cap, capability.ParseInput{
				Scope:  c.Scope,
				Facts:  inv.Facts,
				Sample: s,
				Window: s.Window,
				Prior:  inv.Evidence,
			})
			if pe != nil {
				class := "error"
				if strings.HasPrefix(pe.Error(), "panic:") {
					class = "panic"
					addNotice(inv, cap.ID, "capability hit an internal error (bug, not your system) — please report this with code "+panicCode(cap.ID))
				} else {
					addNotice(inv, cap.ID, "capability failed — "+truncate(pe.Error(), 150))
				}
				newEv = append(newEv, failureEvidence(cap, c.Scope, class, pe.Error()))
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
		startNew := len(inv.Evidence)
		inv.Evidence = append(inv.Evidence, newEv...)
		for _, ev := range newEv {
			registerObserved(inv, ev)
		}
		if e.opt.Identity != nil {
			for i := startNew; i < len(inv.Evidence); i++ {
				ev := &inv.Evidence[i]
				if ev.Entity.Kind != contract.EntityProcess {
					continue
				}
				pid := strings.TrimPrefix(ev.Entity.ID, "pid:")
				svc, ie := e.opt.Identity.Resolve(ctx, pid)
				if ie != nil {
					inv.IdentityGaps++
					continue
				}
				ev.Entity.Display = svc.Name
				ev.Entity.Service = &svc
				registerObserved(inv, *ev)
			}
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
			e.opt.Logger.Warn("decision provider failed; falling back", "error", err)
			answers, _ = drules.New().Ask(ctx, json.RawMessage(state), decision.NextQuestions(candidateKeys(legal)))
			inv.Path = append(inv.Path, contract.Step{
				Depth:      contract.L1Machine,
				Capability: "decision.next",
				Scope:      "machine",
				DecidedBy:  "rules:jev_unavailable",
				Err:        err.Error(),
			})
		}
		if a, ok := answers["explains_anomaly"]; ok && a.Noul >= 0.75 {
			inv.StopReason = contract.StopSufficientEvidence
			break
		}
		if a, ok := answers["deeper_warranted"]; ok && a.Noul < 0.5 {
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
	e.progress("reporting")
	inv.Hypotheses = Synthesize(signals, inv.Evidence, e.opt.MaxFindings)
	if notices := enrich.Attach(ctx, e.opt.Source, inv.Hypotheses); len(notices) > 0 {
		for _, n := range notices {
			inv.Notices = append(inv.Notices, n)
		}
	}
	inv.Spent.Wall = e.opt.Clock().Sub(start)
	inv.Duration = e.opt.Clock().Sub(start)
	return inv, nil
}

func (e *Engine) sweep(ctx context.Context, inv *contract.Investigation) ([]contract.Evidence, contract.Spend, error) {
	caps := e.opt.Registry.ForLevel(contract.L1Machine)
	var reads []source.Read
	for _, cap := range caps {
		rr, re := safeReads(cap, contract.Entity{Kind: contract.EntityMachine, ID: "machine"}, inv.Facts)
		if re != nil {
			addNotice(inv, cap.ID, "capability hit an internal error (bug, not your system) — please report this with code "+panicCode(cap.ID))
			continue
		}
		reads = append(reads, rr...)
	}
	reads = dedupReads(reads)
	s, err := e.opt.Source.Sample(ctx, reads, e.opt.Budget.SampleWindow)
	if err != nil {
		return nil, contract.Spend{}, err
	}
	out := make([]contract.Evidence, 0, len(caps))
	for _, c := range caps {
		ev, pe := safeParse(c, capability.ParseInput{
			Scope:  contract.Entity{Kind: contract.EntityMachine, ID: "machine"},
			Facts:  inv.Facts,
			Sample: s,
			Window: s.Window,
			Prior:  out,
		})
		if pe != nil {
			e.opt.Logger.Warn("capability parse failed", "capability", c.ID, "error", pe)
			if strings.HasPrefix(pe.Error(), "panic:") {
				addNotice(inv, c.ID, "capability hit an internal error (bug, not your system) — please report this with code "+panicCode(c.ID))
			} else {
				addNotice(inv, c.ID, "capability failed — "+truncate(pe.Error(), 150))
			}
			out = append(out, failureEvidence(c, contract.Entity{Kind: contract.EntityMachine, ID: "machine"}, "error", pe.Error()))
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
	register := func(entity contract.Entity) {
		for _, x := range inv.ObservedEntities {
			if x.Kind == entity.Kind && x.ID == entity.ID && x.ParentID == entity.ParentID {
				return
			}
		}
		inv.ObservedEntities = append(inv.ObservedEntities, entity)
	}
	if ev.Capability == "machine.filesystem" {
		b, _ := json.Marshal(ev.Facts)
		var f struct{ Mounts []struct{ Path string } }
		if json.Unmarshal(b, &f) == nil {
			for _, m := range f.Mounts {
				if m.Path != "" {
					register(contract.Entity{Kind: contract.EntityMount, ID: "mount:" + m.Path, Display: m.Path})
				}
			}
		}
	}
	register(ev.Entity)
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
	case contract.DimensionFilesystem:
		return "machine.filesystem"
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
		case kind == contract.EntityMount && ev.Capability == "machine.filesystem":
			b, _ := json.Marshal(ev.Facts)
			var v struct{ Mounts []struct{ Path string } }
			if json.Unmarshal(b, &v) == nil {
				for _, m := range v.Mounts {
					id := "mount:" + m.Path
					if m.Path != "" && !seen[id] {
						seen[id] = true
						out = append(out, contract.Entity{Kind: kind, ID: id, Display: m.Path})
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

const absoluteMaxWall = 10 * time.Minute

func safeReads(cap capability.Capability, scope contract.Entity, facts contract.Facts) (reads []source.Read, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return cap.Reads(scope, facts), nil
}

func safeParse(cap capability.Capability, in capability.ParseInput) (ev contract.Evidence, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return cap.Parse(in)
}

func missingRequirement(cap capability.Capability, facts contract.Facts) string {
	for _, req := range cap.Requires {
		switch req {
		case "root_or_ptrace":
			if !facts.Root {
				return "requires root or CAP_SYS_PTRACE to read another user's process state. Run diagnos as root, or: setcap cap_sys_ptrace=ep /usr/local/bin/diagnos"
			}
		case "syslog_or_root":
			if !facts.Root {
				return "requires CAP_SYSLOG or root to read the kernel log. Run diagnos as root to enable OOM evidence."
			}
		}
	}
	return ""
}

func unavailableEvidence(cap capability.Capability, scope contract.Entity, msg string) contract.Evidence {
	return contract.Evidence{ID: "ev-unavailable-" + strings.ReplaceAll(cap.ID+":"+scope.ID, ":", "-"), Capability: cap.ID, Entity: scope, Dimension: cap.Dimension, Level: cap.Level, CollectedAt: time.Now(), Unavailable: msg}
}

func addNotice(inv *contract.Investigation, capabilityID, msg string) {
	if inv == nil || capabilityID == "" || msg == "" {
		return
	}
	for i := range inv.Notices {
		if inv.Notices[i].Capability == capabilityID {
			inv.Notices[i].Count++
			return
		}
	}
	inv.Notices = append(inv.Notices, contract.Notice{Capability: capabilityID, Message: msg, Count: 1})
}

func sampledFailure(cap capability.Capability, c contract.Candidate, s source.Sample) (string, string) {
	reads, _ := safeReads(cap, c.Scope, contract.Facts{})
	for _, rr := range reads {
		for _, raw := range append(s.T0.Reads[rr.Key], s.T1.Reads[rr.Key]...) {
			if raw.Err == nil {
				continue
			}
			if strings.Contains(raw.Err.Error(), "timed out after") {
				return "timed_out", raw.Err.Error()
			}
			if rr.Optional || errors.Is(raw.Err, os.ErrNotExist) || errors.Is(raw.Err, syscall.ESRCH) {
				continue
			}
			return "error", raw.Err.Error()
		}
	}
	return "", ""
}

func failureEvidence(cap capability.Capability, scope contract.Entity, class, detail string) contract.Evidence {
	ev := contract.Evidence{ID: "ev-failure-" + strings.ReplaceAll(cap.ID+":"+scope.ID, ":", "-"), Capability: cap.ID, Entity: scope, Dimension: cap.Dimension, Level: cap.Level, CollectedAt: time.Now(), Err: detail}
	if class == "timed_out" {
		ev.TimedOut = true
	}
	return ev
}

func noticeForFailure(capID string, scope contract.Entity, class, detail string) string {
	if class == "timed_out" {
		return fmt.Sprintf("%s(%s) timed out after 2s — the target may be on a hung filesystem or wedged process.", capID, scope.ID)
	}
	return fmt.Sprintf("%s(%s) failed — %s", capID, scope.ID, truncate(detail, 150))
}

func panicCode(capID string) string {
	sum := sha256.Sum256([]byte(capID))
	return fmt.Sprintf("%x", sum[:4])
}
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}

func (e *Engine) progress(stage string) {
	if e.opt.Progress != nil {
		e.opt.Progress(stage)
	}
}

func isLocalHost(host string) bool {
	return host == "" || strings.EqualFold(host, "localhost") || host == "127.0.0.1"
}

func buildMachineSnapshot(inv *contract.Investigation) contract.MachineSnapshot {
	s := contract.MachineSnapshot{RootDiskPath: "/"}
	if inv == nil {
		return s
	}

	s.CPUs = inv.Machine.CPUs
	s.MemoryTotalBytes = inv.Machine.MemTotal

	for _, ev := range inv.Evidence {
		for _, obs := range ev.Observations {
			switch obs.Key {
			case "cpu.utilization":
				s.CPUUtilizationPct = obs.Value
			case "load.one_per_core":
				if s.CPUs > 0 {
					s.Load1 = obs.Value * float64(s.CPUs)
				}
			case "mem.available_pct":
				if s.MemoryTotalBytes > 0 {
					s.MemoryAvailableBytes = uint64(float64(s.MemoryTotalBytes) * obs.Value / 100)
					s.MemoryUsedBytes = s.MemoryTotalBytes - s.MemoryAvailableBytes
				}
				s.MemoryUsedPct = 100 - obs.Value
			case "mem.swap_used_pct":
				s.SwapUsedPct = obs.Value
			}
		}
	}

	var fs syscall.Statfs_t
	if err := syscall.Statfs("/", &fs); err == nil {
		bsize := uint64(fs.Bsize)
		s.RootDiskTotalBytes = fs.Blocks * bsize
		s.RootDiskFreeBytes = fs.Bavail * bsize
		if s.RootDiskTotalBytes >= s.RootDiskFreeBytes {
			s.RootDiskUsedBytes = s.RootDiskTotalBytes - s.RootDiskFreeBytes
			if s.RootDiskTotalBytes > 0 {
				s.RootDiskUsedPct = float64(s.RootDiskUsedBytes) / float64(s.RootDiskTotalBytes) * 100
			}
		}
	}
	return s
}
