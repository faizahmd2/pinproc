package main

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"

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

// runMonitor builds and runs the read-only pull trigger until ctx is cancelled.
func runMonitor(ctx context.Context, cfg *config.Config, reportDir string, mu *sync.Mutex) error {
	cores := runtime.NumCPU()
	sink := &captureSink{cfg: cfg, reportDir: reportDir, mu: mu}
	m := monitor.New(cfg.Monitor, pressure.NewReader("/proc", cores), contenders.NewSampler("/proc"), sink)
	logger.Info("pinproc self-trigger active", "mode", "pull", "calm", cfg.Monitor.CalmCadence, "armed", cfg.Monitor.ArmedCadence)
	return m.Run(ctx)
}
