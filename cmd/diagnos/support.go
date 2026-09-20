package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/faizahmd2/vm-native-diagnos/internal/contract"
	"github.com/faizahmd2/vm-native-diagnos/internal/source"
)

func targetSource(ctx context.Context, host string) (source.Source, func(), error) {
	if !localHost(host) {
		return nil, nil, fmt.Errorf("vm-native-diagnos is local-only; target %q is not supported", host)
	}
	s := source.NewLocal("/proc", "/sys", 8<<20)
	if err := s.StartupCheck(); err != nil { _ = s.Close(); return nil, nil, err }
	return s, func() { _ = s.Close() }, nil
}

func localHost(host string) bool {
	return host == "" || strings.EqualFold(host, "localhost") || host == "127.0.0.1"
}

func budget(name string) (contract.Budget, error) {
	switch name {
	case "fast":
		return contract.BudgetFast(), nil
	case "normal", "":
		return contract.BudgetNormal(), nil
	case "deep":
		return contract.BudgetDeep(), nil
	default:
		return contract.Budget{}, fmt.Errorf("unknown budget %q", name)
	}
}
