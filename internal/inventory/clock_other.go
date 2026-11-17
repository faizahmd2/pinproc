//go:build !linux

package inventory

func checkClock() Check { return Check{} }
