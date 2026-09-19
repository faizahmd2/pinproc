package transport

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestLocalExecutorPreservesNonZeroExitAsData(t *testing.T) {
	exec := &LocalExecutor{}
	result := exec.Run(context.Background(), Command{Key: "test.false", Argv: "false"})
	if result.Err != nil || result.ExitCode != 1 {
		t.Fatalf("expected ExitCode=1 and Err=nil, got %#v", result)
	}
}

func TestLocalExecutorCapsOutput(t *testing.T) {
	exec := &LocalExecutor{}
	result := exec.Run(context.Background(), Command{Key: "test.output", Argv: "yes x | head -c 4096", MaxBytes: 64})
	if !result.Truncated || len(result.Stdout) != 64 {
		t.Fatalf("expected capped output, got truncated=%v bytes=%d", result.Truncated, len(result.Stdout))
	}
}

func TestLocalExecutorTimesOut(t *testing.T) {
	exec := &LocalExecutor{}
	started := time.Now()
	result := exec.Run(context.Background(), Command{Key: "test.timeout", Argv: "sleep 2", Timeout: 20 * time.Millisecond})
	if !result.TimedOut || result.ExitCode != 124 || time.Since(started) > time.Second {
		t.Fatalf("expected prompt timeout result, got %#v", result)
	}
}

func TestParseFacts(t *testing.T) {
	exec := &FakeExecutor{Results: map[string]Result{"bootstrap.facts": {Stdout: "ID=ubuntu\nID_LIKE=debian\nVERSION_ID=24.04\n---\n6.8\n---\n/usr/bin/systemctl\n/usr/bin/ss\n"}}}
	facts, err := probeFacts(context.Background(), exec)
	if err != nil || facts.Family != FamilyDebian || !facts.Has["systemctl"] || !strings.Contains(facts.Kernel, "6.8") {
		t.Fatalf("unexpected facts %#v, %v", facts, err)
	}
}
