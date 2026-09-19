package executor

import (
	"context"
	"fmt"
	"time"

	"github.com/faizahmd2/vm-native-diagnos/internal/transport"
)

// NativeExecutor keeps the small Run(string, timeout) surface used by the
// existing collectors while delegating every remote operation to native SSH.
type NativeExecutor struct{ Transport transport.Executor }

func NewNative(exec transport.Executor) *NativeExecutor { return &NativeExecutor{Transport: exec} }
func (e *NativeExecutor) Run(command string, timeout time.Duration) (string, error) {
	if e == nil || e.Transport == nil {
		return "", fmt.Errorf("native executor is nil")
	}
	r := e.Transport.Run(context.Background(), transport.Command{Key: "collector.fixed", Argv: command, Timeout: timeout})
	if r.Err != nil {
		return r.Stdout, r.Err
	}
	if r.TimedOut {
		return r.Stdout, fmt.Errorf("command timed out after %s", timeout)
	}
	if r.ExitCode != 0 {
		return r.Stdout, fmt.Errorf("remote command exited %d: %s", r.ExitCode, r.Stderr)
	}
	return r.Stdout, nil
}
func (e *NativeExecutor) CheckTarget() error {
	r := e.Transport.Run(context.Background(), transport.Command{Key: "transport.check", Argv: "true", Timeout: 10 * time.Second})
	if r.Err != nil {
		return r.Err
	}
	if r.ExitCode != 0 {
		return fmt.Errorf("remote target check exited %d", r.ExitCode)
	}
	return nil
}
func (e *NativeExecutor) Close() error {
	if e == nil || e.Transport == nil {
		return nil
	}
	return e.Transport.Close()
}

// Supports uses the connection-cached OS probe to avoid launching a command
// that cannot exist on this Linux family (for example journalctl on SysV).
func (e *NativeExecutor) Supports(name string) bool {
	if e == nil || e.Transport == nil {
		return false
	}
	facts, err := e.Transport.Facts(context.Background())
	return err == nil && facts.Has[name]
}
