package main

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/faizahmd2/pinproc/internal/aggregate"
	"github.com/faizahmd2/pinproc/internal/callback"
	"github.com/faizahmd2/pinproc/internal/capability"
	"github.com/faizahmd2/pinproc/internal/config"
	"github.com/faizahmd2/pinproc/internal/contenders"
	"github.com/faizahmd2/pinproc/internal/contract"
	"github.com/faizahmd2/pinproc/internal/dots"
	"github.com/faizahmd2/pinproc/internal/engine"
	"github.com/faizahmd2/pinproc/internal/identity"
	"github.com/faizahmd2/pinproc/internal/monitor"
	"github.com/faizahmd2/pinproc/internal/narrator"
	"github.com/faizahmd2/pinproc/internal/netmap"
	"github.com/faizahmd2/pinproc/internal/pressure"
	"github.com/faizahmd2/pinproc/internal/report"
	"github.com/faizahmd2/pinproc/internal/rules"
	"github.com/faizahmd2/pinproc/internal/source"
)

// (enrich/abstract layer will build on this capture pipeline)

// captureSink turns a confirmed incident into a full, cross-dimension report. The
// armed dimension focuses the engine, but the engine still sweeps every resource,
// so a CPU trigger still surfaces memory/io owners. The pre-capture dots become the
// report's incident timeline.
type captureSink struct {
	cfg       *config.Config
	reportDir string
	mu        *sync.Mutex // shared so auto-capture and any manual run never overlap
}

func (s *captureSink) Capture(ctx context.Context, c monitor.Capture) {
	s.mu.Lock()
	defer s.mu.Unlock()

	src := source.NewLocalWithTimeout("/proc", "/sys", 8<<20, s.cfg.Source.ReadTimeout)
	defer src.Close()
	reg, err := capability.BuildBuiltin()
	if err != nil {
		logger.Error("capture: capability graph unavailable", "error", err)
		return
	}
	dec, err := makeDecisionProvider(s.cfg)
	if err != nil {
		logger.Error("capture: decision provider unavailable", "error", err)
		return
	}
	eng := engine.New(engine.Options{
		Source: src, Registry: reg, Rules: rules.Default(), Decision: dec,
		Identity: identity.New(src), Budget: contract.BudgetNormal(), ParallelWidth: 3,
		MaxFindings: s.cfg.Report.MaxFindings, DecisionNotice: decisionNotice(s.cfg), Logger: logger,
	})
	inv, err := eng.Run(ctx, engine.Request{
		ID:        fmt.Sprintf("inc-%d", c.FiredAt.UnixNano()),
		Host:      localHostName(),
		Trigger:   "auto:" + string(c.Dimension),
		Dimension: c.Dimension,
	})
	if err != nil {
		logger.Warn("capture investigation failed", "error", err)
		return
	}
	inv.Incident = toIncident(c)
	inv.Incident.Cause = buildCause(ctx, src, inv, c.Dimension)
	if s.cfg.Narrator.Enabled {
		if text, ne := narrator.NewRules().Narrate(ctx, inv); ne == nil && narrator.Validate(inv, text) == nil {
			inv.Narrative = text
		}
	}
	if err := report.Write(inv, s.reportDir); err != nil {
		logger.Error("capture: report write failed", "error", err)
		return
	}
	logger.Info("incident captured", "dimension", c.Dimension, "reason", c.Reason, "dots", len(c.Dots))

	if s.cfg.Callback.Enabled && strings.TrimSpace(s.cfg.Callback.URL) != "" {
		payload := *inv
		go func() {
			if err := callback.Post(context.Background(), s.cfg.Callback.URL, s.cfg.Callback.Timeout, &payload); err != nil {
				logger.Warn("report callback failed", "error", err)
			}
		}()
	}
}

// toIncident converts the monitor's dots into the stable report shape.
func toIncident(c monitor.Capture) *contract.Incident {
	inc := &contract.Incident{
		Dimension: c.Dimension, StartedAt: c.StartedAt, FiredAt: c.FiredAt, Reason: c.Reason,
		Timeline: make([]contract.IncidentPoint, 0, len(c.Dots)),
	}
	for _, d := range c.Dots {
		p := contract.IncidentPoint{
			At: d.At, ArmedLevel: d.Level, ArmedPSI: d.PSI,
			CPUUtil: d.Ctx.CPUUtil, CPUPSI: d.Ctx.CPUPSI,
			MemUsed: d.Ctx.MemUsed, MemPSI: d.Ctx.MemPSI, IOPSI: d.Ctx.IOPSI,
		}
		for _, t := range topContenders(d.Top, 3) {
			p.Top = append(p.Top, contract.IncidentContender{PID: t.PID, Comm: t.Comm, Value: t.Value, Unit: t.Unit})
		}
		inc.Timeline = append(inc.Timeline, p)
	}
	return inc
}

func topContenders(in []dots.Contender, n int) []dots.Contender {
	if len(in) > n {
		return in[:n]
	}
	return in
}

// buildCause aggregates the per-process table into the service responsible for the
// incident dimension — the definitive, app-level attribution.
func buildCause(ctx context.Context, src source.Source, inv *contract.Investigation, dim contract.Dimension) *contract.ServiceCause {
	if dim == contract.DimensionNetwork {
		return buildNetworkCause(ctx, src)
	}
	rows := processRowsFromEvidence(inv)
	if len(rows) == 0 {
		return nil
	}
	unit := causeUnit(dim)
	// Value per process for this dimension.
	val := func(r procRowView) float64 {
		switch dim {
		case contract.DimensionMemory:
			return r.RSS
		case contract.DimensionIO:
			return r.IOBPS
		default:
			return r.CPUPercent * 100 // fraction-of-core -> percent
		}
	}
	sort.Slice(rows, func(i, j int) bool { return val(rows[i]) > val(rows[j]) })
	if len(rows) > 20 {
		rows = rows[:20]
	}
	resolver := identity.New(src)
	procs := make([]aggregate.Proc, 0, len(rows))
	for _, r := range rows {
		v := val(r)
		if v <= 0 {
			continue
		}
		key, display := resolver.GroupOf(ctx, strconv.Itoa(int(r.PID)))
		procs = append(procs, aggregate.Proc{PID: int(r.PID), Comm: r.Comm, Key: key, Display: display, Value: v})
	}
	svcs := aggregate.Group(procs, 5)
	if len(svcs) == 0 || svcs[0].Value <= 0 {
		return nil
	}
	top := svcs[0]
	cause := &contract.ServiceCause{
		Service: top.Display, Key: top.Key, Dimension: dim, Procs: top.Procs, Value: top.Value, Unit: unit,
	}
	for _, c := range top.Components {
		cause.Components = append(cause.Components, contract.ServiceComponent{PID: c.PID, Comm: c.Comm, Value: c.Value, Pct: c.Pct})
	}
	return cause
}

// buildNetworkCause attributes open TCP connections to the owning service, naming
// the service with the most connections and the ports it listens on. Byte rate per
// service is not available natively, so this is connection-based (definitive) and
// the report pairs it with the machine's total bandwidth/retransmits.
func buildNetworkCause(ctx context.Context, src source.Source) *contract.ServiceCause {
	conns := netmap.Connections("/proc")
	if len(conns) == 0 {
		return nil
	}
	owners := netmap.SocketOwners("/proc")
	resolver := identity.New(src)

	type svcAgg struct {
		display string
		conns   int
		listen  map[int]bool
		comps   map[int]int // pid -> conn count
	}
	byKey := map[string]*svcAgg{}
	groupCache := map[int][2]string{} // pid -> {key, display}
	for _, c := range conns {
		pid, ok := owners[c.Inode]
		if !ok {
			continue
		}
		kd, cached := groupCache[pid]
		if !cached {
			key, disp := resolver.GroupOf(ctx, strconv.Itoa(pid))
			kd = [2]string{key, disp}
			groupCache[pid] = kd
		}
		a := byKey[kd[0]]
		if a == nil {
			a = &svcAgg{display: kd[1], listen: map[int]bool{}, comps: map[int]int{}}
			byKey[kd[0]] = a
		}
		a.conns++
		a.comps[pid]++
		if c.Listen {
			a.listen[c.LocalPort] = true
		}
	}
	// pick the service with the most connections
	var topKey string
	top := (*svcAgg)(nil)
	for k, a := range byKey {
		if top == nil || a.conns > top.conns {
			top, topKey = a, k
		}
	}
	if top == nil || top.conns == 0 {
		return nil
	}
	cause := &contract.ServiceCause{
		Service: top.display, Key: topKey, Dimension: contract.DimensionNetwork,
		Procs: len(top.comps), Value: float64(top.conns), Unit: "connections",
	}
	for pid, n := range top.comps {
		cause.Components = append(cause.Components, contract.ServiceComponent{
			PID: pid, Value: float64(n), Pct: float64(n) / float64(top.conns) * 100,
		})
	}
	sort.Slice(cause.Components, func(i, j int) bool { return cause.Components[i].Value > cause.Components[j].Value })
	if len(cause.Components) > 5 {
		cause.Components = cause.Components[:5]
	}
	for port := range top.listen {
		cause.Ports = append(cause.Ports, contract.Port{Proto: "tcp", Port: port})
	}
	sort.Slice(cause.Ports, func(i, j int) bool { return cause.Ports[i].Port < cause.Ports[j].Port })
	return cause
}

func causeUnit(dim contract.Dimension) string {
	switch dim {
	case contract.DimensionMemory:
		return "bytes"
	case contract.DimensionIO:
		return "bytes_per_sec"
	default:
		return "percent"
	}
}

type procRowView struct {
	PID        int64
	PPID       int64
	Comm       string
	RSS        float64
	CPUPercent float64
	IOBPS      float64
}

func processRowsFromEvidence(inv *contract.Investigation) []procRowView {
	for _, ev := range inv.Evidence {
		if ev.Capability != "machine.processes" {
			continue
		}
		b, _ := json.Marshal(ev.Facts)
		var v struct{ Rows []procRowView }
		if json.Unmarshal(b, &v) == nil {
			return v.Rows
		}
	}
	return nil
}

// runMonitor builds and runs the read-only pull trigger until ctx is cancelled.
func runMonitor(ctx context.Context, cfg *config.Config, reportDir string, mu *sync.Mutex) error {
	cores := runtime.NumCPU()
	sink := &captureSink{cfg: cfg, reportDir: reportDir, mu: mu}
	m := monitor.New(cfg.Monitor, pressure.NewReader("/proc", cores), contenders.NewSampler("/proc"), sink)
	logger.Info("pinproc self-trigger active", "mode", "pull", "calm", cfg.Monitor.CalmCadence, "armed", cfg.Monitor.ArmedCadence)
	return m.Run(ctx)
}
