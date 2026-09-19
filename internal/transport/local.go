package transport

import (
	"context"
	"errors"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// LocalExecutor is useful for development and tests. It has the same result
// semantics as SSHExecutor, without requiring an SSH server.
type LocalExecutor struct {
	DefaultTimeout time.Duration
	MaxBytes       int64
	Parallel       int
}

// Run executes a local shell command with bounded output and process-group timeout cleanup.
func (e *LocalExecutor) Run(ctx context.Context, cmd Command) Result {
	start := time.Now()
	result := Result{Key: cmd.Key, ExitCode: -1}
	timeout := cmd.Timeout
	if timeout <= 0 {
		timeout = e.DefaultTimeout
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	process := exec.CommandContext(ctx, "sh", "-c", cmd.Argv)
	process.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, stderr := newCappedBuffer(limitFor(cmd.MaxBytes, e.MaxBytes)), newCappedBuffer(limitFor(cmd.MaxBytes, e.MaxBytes))
	process.Stdout, process.Stderr = stdout, stderr
	err := process.Start()
	if err == nil {
		done := make(chan error, 1)
		go func() { done <- process.Wait() }()
		select {
		case err = <-done:
		case <-ctx.Done():
			_ = syscall.Kill(-process.Process.Pid, syscall.SIGKILL)
			err = <-done
		}
	} else {
		err = process.Run()
	}
	result.Duration, result.Stdout, result.Stderr = time.Since(start), stdout.String(), stderr.String()
	result.Truncated = stdout.Truncated() || stderr.Truncated()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		result.TimedOut, result.ExitCode = true, 124
		return result
	}
	if err == nil {
		result.ExitCode = 0
		return result
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		return result
	}
	result.Err = err
	return result
}

// RunAll executes local commands in bounded parallelism.
func (e *LocalExecutor) RunAll(ctx context.Context, cmds []Command) []Result {
	results := make([]Result, len(cmds))
	n := e.Parallel
	if n <= 0 {
		n = 4
	}
	if n > 8 {
		n = 8
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	for worker := 0; worker < n; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				results[i] = e.Run(ctx, cmds[i])
			}
		}()
	}
	for i := range cmds {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	return results
}

// RunScript executes a script through the local executor.
func (e *LocalExecutor) RunScript(ctx context.Context, script string, timeout time.Duration, maxBytes int64) Result {
	return e.Run(ctx, Command{Key: "script", Argv: script, Timeout: timeout, MaxBytes: maxBytes})
}

// Facts returns local facts.
func (e *LocalExecutor) Facts(ctx context.Context) (Facts, error) { return probeFacts(ctx, e) }

// Close closes the local executor.
func (e *LocalExecutor) Close() error { return nil }
