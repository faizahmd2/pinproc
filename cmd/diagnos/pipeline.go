package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/faizahmd2/pinproc/internal/capability"
	"github.com/faizahmd2/pinproc/internal/config"
	"github.com/faizahmd2/pinproc/internal/contract"
	drules "github.com/faizahmd2/pinproc/internal/decision/rules"
	"github.com/faizahmd2/pinproc/internal/deliver"
	"github.com/faizahmd2/pinproc/internal/doc"
	"github.com/faizahmd2/pinproc/internal/engine"
	"github.com/faizahmd2/pinproc/internal/identity"
	"github.com/faizahmd2/pinproc/internal/inventory"
	"github.com/faizahmd2/pinproc/internal/monitor"
	"github.com/faizahmd2/pinproc/internal/render"
	"github.com/faizahmd2/pinproc/internal/report"
	"github.com/faizahmd2/pinproc/internal/rules"
	"github.com/faizahmd2/pinproc/internal/source"
)

// The pipeline is: collect (engine + inventory, in parallel) -> Document (JSON)
// -> save -> deliver. Everything after the Document is a pure function of it, so
// rendering and network delivery never sit on the sampling path.

// runEngine runs the attribution engine once (findings, cause evidence, text).
func runEngine(ctx context.Context, cfg *config.Config, req engine.Request) (*contract.Investigation, source.Source, func(), error) {
	src := source.NewLocalWithTimeout("/proc", "/sys", 8<<20, cfg.Source.ReadTimeout)
	if err := src.StartupCheck(); err != nil {
		src.Close()
		return nil, nil, func() {}, err
	}
	reg, err := capability.BuildBuiltin()
	if err != nil {
		src.Close()
		return nil, nil, func() {}, err
	}
	eng := engine.New(engine.Options{
		Source: src, Registry: reg, Rules: rules.Default(), Decision: drules.New(),
		Identity: identity.New(src), Budget: contract.BudgetNormal(), ParallelWidth: 3,
		MaxFindings: cfg.Report.MaxFindings, Logger: logger,
	})
	inv, err := eng.Run(ctx, req)
	return inv, src, func() { src.Close() }, err
}

// collectBoth runs the engine and the inventory collector concurrently. The
// inventory is best-effort: an engine failure fails the run, an inventory failure
// only leaves the machine sections empty.
func collectBoth(ctx context.Context, cfg *config.Config, req engine.Request, opt inventory.Options, after func(*contract.Investigation, source.Source)) (*contract.Investigation, *inventory.Snapshot, error) {
	var (
		wg   sync.WaitGroup
		snap *inventory.Snapshot
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		s, err := inventory.New(opt).Collect(ctx)
		if err != nil {
			logger.Warn("inventory collection failed", "error", err)
			return
		}
		snap = s
	}()
	inv, src, closeSrc, err := runEngine(ctx, cfg, req)
	defer closeSrc()
	if err == nil && after != nil {
		after(inv, src)
	}
	wg.Wait()
	return inv, snap, err
}

// buildDocument turns a finished run into the Document every output renders from.
func buildDocument(cfg *config.Config, kind doc.Kind, inv *contract.Investigation, snap *inventory.Snapshot) *doc.Document {
	d := doc.New(kind, inv, snap)
	d.Tag = strings.TrimSpace(cfg.Report.Tag)
	if kind == doc.KindReport {
		d.Headline = "Detailed machine report" // findings keep their own section
	}
	return d
}

// writeOutputs saves name.json and name.html (and the classic text report for
// alerts) into dir, returning the HTML and JSON paths.
func writeOutputs(cfg *config.Config, dir, name string, d *doc.Document) (htmlPath, jsonPath string, err error) {
	jsonPath, err = doc.Save(dir, name, d)
	if err != nil {
		return "", "", err
	}
	page, err := render.HTML(d, deliver.Options(cfg))
	if err != nil {
		return "", jsonPath, err
	}
	htmlPath = filepath.Join(dir, name+".html")
	if err := doc.WriteFile(htmlPath, page); err != nil {
		return "", jsonPath, err
	}
	if d.Kind == doc.KindAlert && d.Text != "" && name == "alert" {
		_ = report.WriteText(dir, d.Text) // keeps `pinproc report --last` and old readers working
	}
	return htmlPath, jsonPath, nil
}

// outputDir picks where an on-demand run writes: the explicit flag, the data
// directory when writable (root / service user), else the user's state directory.
func outputDir(flag string) (string, error) {
	if flag != "" {
		return config.ResolveOutputDirectory(flag)
	}
	if tryWritable(config.DataDirectory) == nil {
		return config.DataDirectory, nil
	}
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "pinproc"), nil
}

// triggerDetail states exactly which kernel signal crossed which threshold. A
// threshold set to 0 is disabled, so it is left out.
func triggerDetail(cfg monitor.Config, c monitor.Capture) string {
	var parts []string
	add := func(on bool, format string, a ...any) {
		if on {
			parts = append(parts, fmt.Sprintf(format, a...))
		}
	}
	if n := len(c.Dots); n > 0 {
		last := c.Dots[n-1]
		switch c.Dimension {
		case contract.DimensionMemory:
			add(true, "/proc/pressure/memory some avg10=%.2f%%"+limit(cfg.Mem.CapPSI, "%"), last.PSI)
			add(true, "memory used %.0f%%"+limit(cfg.Mem.CapUtil, "%"), last.Level)
		case contract.DimensionIO:
			add(true, "/proc/pressure/io some avg10=%.2f%%"+limit(cfg.IO.CapPSI, "%"), last.PSI)
			add(true, "disk busy %.0f%%"+limit(cfg.IO.CapUtil, "%"), last.Level)
		case contract.DimensionNetwork:
			add(true, "tcp retransmits %.0f/s"+limit(cfg.Net.RetransCap, "/s"), last.Ctx.Retrans)
			add(true, "TIME_WAIT %d · conntrack %.0f%%", last.Ctx.TimeWait, last.Ctx.Conntrack)
		default:
			add(true, "/proc/pressure/cpu some avg10=%.2f%%"+limit(cfg.CPU.CapPSI, "%"), last.PSI)
			add(true, "cpu busy %.0f%%"+limit(cfg.CPU.CapUtil, "%"), last.Level)
		}
	}
	reason := c.Reason
	if d := c.FiredAt.Sub(c.StartedAt); d > 0 {
		reason += " after " + d.Round(time.Second).String()
	}
	return strings.Join(append(parts, reason), " · ")
}

// limit renders " (capture ≥ N<unit>)", or nothing when the threshold is off.
func limit(v float64, unit string) string {
	if v <= 0 {
		return ""
	}
	return strings.ReplaceAll(fmt.Sprintf(" (capture ≥ %.0f%s)", v, unit), "%", "%%")
}
