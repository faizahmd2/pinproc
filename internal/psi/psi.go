// Package psi arms Linux Pressure Stall Information (PSI) triggers and blocks
// until the kernel reports that a resource (cpu, memory or io) has stalled past a
// configured threshold. It is the kernel-push trigger that lets pinproc sleep at
// ~0% CPU and wake only when a resource is genuinely under pressure — no polling
// loop, no listening socket.
//
// PSI triggers require Linux >= 5.2 with CONFIG_PSI=y. On any other platform, or
// an older/unconfigured kernel, Available reports false and Arm returns
// ErrUnsupported so callers can fall back to a lightweight sampler.
package psi

import (
	"errors"
	"fmt"
	"time"
)

// ErrUnsupported is returned when PSI triggers are not available on this host.
var ErrUnsupported = errors.New("psi: pressure triggers are not supported on this kernel")

// Resource is a PSI-tracked resource.
type Resource string

const (
	CPU    Resource = "cpu"
	Memory Resource = "memory"
	IO     Resource = "io"
)

// Kind selects which PSI line a trigger watches.
//
//	Some: time at least one task was stalled on the resource.
//	Full: time all non-idle tasks were stalled (not available for cpu).
type Kind string

const (
	Some Kind = "some"
	Full Kind = "full"
)

// Config describes a PSI trigger: "wake me if <Resource> was stalled (<Kind>) for
// at least Stall out of every Window". The kernel requires Window in [500ms, 10s]
// and 0 < Stall <= Window.
type Config struct {
	Resource Resource
	Kind     Kind
	Stall    time.Duration
	Window   time.Duration
}

const (
	minWindow = 500 * time.Millisecond
	maxWindow = 10 * time.Second
)

// spec renders and validates the trigger line the kernel expects, e.g.
// "some 150000 1000000" (microseconds).
func (c Config) spec() (string, error) {
	switch c.Resource {
	case CPU, Memory, IO:
	default:
		return "", fmt.Errorf("psi: invalid resource %q", c.Resource)
	}
	kind := c.Kind
	if kind == "" {
		kind = Some
	}
	if kind != Some && kind != Full {
		return "", fmt.Errorf("psi: invalid kind %q", kind)
	}
	if kind == Full && c.Resource == CPU {
		return "", errors.New("psi: cpu has no 'full' line")
	}
	win := c.Window
	if win == 0 {
		win = time.Second
	}
	if win < minWindow || win > maxWindow {
		return "", fmt.Errorf("psi: window %s out of range [500ms,10s]", win)
	}
	stall := c.Stall
	if stall <= 0 || stall > win {
		return "", fmt.Errorf("psi: stall %s must be >0 and <= window %s", stall, win)
	}
	return fmt.Sprintf("%s %d %d", kind, stall.Microseconds(), win.Microseconds()), nil
}
