package engine

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/faizahmd2/diagnos/internal/capability"
	"github.com/faizahmd2/diagnos/internal/contract"
	drules "github.com/faizahmd2/diagnos/internal/decision/rules"
	"github.com/faizahmd2/diagnos/internal/rules"
	"github.com/faizahmd2/diagnos/internal/source"
)

type sampleSource struct{ s source.Sample }

func (f sampleSource) Name() string { return "fixture" }
func (f sampleSource) Facts(context.Context) (contract.Facts, error) {
	return contract.Facts{Kernel: "fixture", Has: map[string]bool{"proc": true}}, nil
}
func (f sampleSource) Snapshot(_ context.Context, _ []source.Read) (source.Snapshot, error) {
	return f.s.T1, nil
}
func (f sampleSource) Sample(_ context.Context, _ []source.Read, _ time.Duration) (source.Sample, error) {
	return f.s, nil
}
func (f sampleSource) Close() error { return nil }

func procStat(user uint64) []byte {
	return []byte(fmt.Sprintf(
		"42 (fixture) S 1 42 42 0 0 0 0 0 %d %d 0 0 0 0 20 0 1 0 100 1048576 100 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0\n",
		user, user,
	))
}

func machineStat(user uint64) []byte {
	return []byte(fmt.Sprintf(
		"cpu %d 0 0 0 0 0 0 0 0 0\ncpu0 %d 0 0 0 0 0 0 0 0 0\nctxt 100\nprocesses 10\nprocs_running 4\nprocs_blocked 0\nbtime 1700000000\n",
		user, user,
	))
}

func TestAdaptiveDescentCPUToProcess(t *testing.T) {
	now := time.Now()
	t0 := source.Snapshot{
		At: now,
		Reads: map[string][]source.Raw{
			"proc.stat":    {{Key: "proc.stat", Path: "/proc/stat", Data: machineStat(100)}},
			"proc.loadavg": {{Key: "proc.loadavg", Path: "/proc/loadavg", Data: []byte("2.0 2.0 2.0 2/4 42\n")}},
			"pid.stat":     {{Key: "pid.stat", Path: "/proc/42/stat", Data: procStat(10)}},
			"pid.status":   {{Key: "pid.status", Path: "/proc/42/status", Data: []byte("Name:\tfixture\nUid:\t1000 1000 1000 1000\nThreads:\t1\n")}},
		},
	}
	t1 := source.Snapshot{
		At: now.Add(time.Second),
		Reads: map[string][]source.Raw{
			"proc.stat":    {{Key: "proc.stat", Path: "/proc/stat", Data: machineStat(1100)}},
			"proc.loadavg": {{Key: "proc.loadavg", Path: "/proc/loadavg", Data: []byte("2.0 2.0 2.0 2/4 42\n")}},
			"pid.stat":     {{Key: "pid.stat", Path: "/proc/42/stat", Data: procStat(110)}},
			"pid.status":   {{Key: "pid.status", Path: "/proc/42/status", Data: []byte("Name:\tfixture\nUid:\t1000 1000 1000 1000\nThreads:\t1\n")}},
		},
	}
	reg, err := capability.BuildBuiltin()
	if err != nil {
		t.Fatal(err)
	}
	e := New(Options{
		Source:   sampleSource{s: source.Sample{T0: t0, T1: t1, Window: time.Second}},
		Registry: reg,
		Rules:    rules.Default(),
		Decision: drules.New(),
		Budget:   contract.BudgetFast(),
	})
	inv, err := e.Run(context.Background(), Request{Host: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.Hypotheses) == 0 {
		t.Fatal("expected CPU hypothesis")
	}
	found := false
	for _, step := range inv.Path {
		if step.Capability == "machine.processes" && step.Scope == "machine" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected machine process attribution, path=%#v", inv.Path)
	}
}
