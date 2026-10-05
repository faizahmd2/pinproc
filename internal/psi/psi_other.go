//go:build !linux

package psi

import "context"

// Available reports false on non-Linux platforms; PSI is Linux-only.
func Available() bool { return false }

// Trigger is a non-functional placeholder on non-Linux platforms.
type Trigger struct{}

// Arm always returns ErrUnsupported off Linux.
func Arm(cfg Config) (*Trigger, error) { return nil, ErrUnsupported }

// Resource returns an empty resource off Linux.
func (t *Trigger) Resource() Resource { return "" }

// Wait always returns ErrUnsupported off Linux.
func (t *Trigger) Wait(ctx context.Context) error { return ErrUnsupported }

// Close is a no-op off Linux.
func (t *Trigger) Close() error { return nil }
