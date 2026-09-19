package transport

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// FakeExecutor allows collectors to be tested without a network connection.
type FakeExecutor struct {
	Results  map[string]Result
	FactData Facts
	FactErr  error
	Runs     []Command
	mu       sync.Mutex
}

// Run records and returns a fake command result.
func (e *FakeExecutor) Run(_ context.Context, cmd Command) Result {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.Runs = append(e.Runs, cmd)
	r, ok := e.Results[cmd.Key]
	if !ok {
		return Result{Key: cmd.Key, Err: fmt.Errorf("no fake result for %q", cmd.Key)}
	}
	r.Key = cmd.Key
	return r
}

// RunAll executes fake commands in order.
func (e *FakeExecutor) RunAll(ctx context.Context, cmds []Command) []Result {
	out := make([]Result, len(cmds))
	for i, cmd := range cmds {
		out[i] = e.Run(ctx, cmd)
	}
	return out
}

// RunScript records one scripted transport call.
func (e *FakeExecutor) RunScript(ctx context.Context, script string, timeout time.Duration, maxBytes int64) Result {
	return e.Run(ctx, Command{Key: "script", Argv: script, Timeout: timeout, MaxBytes: maxBytes})
}

// Facts returns configured fake facts.
func (e *FakeExecutor) Facts(context.Context) (Facts, error) { return e.FactData, e.FactErr }

// Close closes the fake.
func (e *FakeExecutor) Close() error { return nil }
