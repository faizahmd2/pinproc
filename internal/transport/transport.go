// Package transport provides the connection-scoped command transport used by
// collectors. It deliberately distinguishes a remote command failure from a
// failure to reach or operate the transport.
package transport

import (
	"context"
	"time"
)

type Command struct {
	Key      string
	Argv     string
	Timeout  time.Duration
	Sudo     bool
	MaxBytes int64
}

type Result struct {
	Key       string
	Stdout    string
	Stderr    string
	ExitCode  int
	Duration  time.Duration
	Truncated bool
	TimedOut  bool
	Err       error // transport failure only; a non-zero ExitCode is data.
}

type Family string

const (
	FamilyDebian  Family = "debian"
	FamilyRHEL    Family = "rhel"
	FamilyUnknown Family = "unknown"
)

type Facts struct {
	OSID     string
	OSIDLike []string
	Family   Family
	Version  string
	Kernel   string
	Has      map[string]bool
}

type Executor interface {
	Run(context.Context, Command) Result
	RunAll(context.Context, []Command) []Result
	Facts(context.Context) (Facts, error)
	RunScript(context.Context, string, time.Duration, int64) Result
	Close() error
}
