package main

import (
	"context"
	"fmt"

	"github.com/faizahmd2/diagnos/internal/config"
	"github.com/faizahmd2/diagnos/internal/contract"
	"github.com/faizahmd2/diagnos/internal/source"
	"github.com/faizahmd2/diagnos/internal/transport"
	"strings"
)

func targetSource(ctx context.Context, host string) (source.Source, func(), *config.Config, error) {
	if localHost(host) {
		s := source.NewLocal("/proc", "/sys", 8<<20)
		return s, func() { _ = s.Close() }, nil, nil
	}
	cfg, e := config.Load(cfgPath)
	if e != nil {
		return nil, nil, nil, e
	}
	tc, e := cfg.ResolveTarget(host)
	if e != nil {
		return nil, nil, nil, e
	}
	x, e := transport.Dial(ctx, transport.Config{Host: tc.Host, User: tc.User, Port: tc.Port, KeyPath: cfg.SSH.KeyPath, KnownHosts: cfg.SSH.KnownHosts, HostKeyPolicy: transport.HostKeyPolicy(cfg.SSH.HostKeyPolicy), JumpHosts: cfg.SSH.JumpHosts, ConnectTimeout: cfg.SSH.ConnectTimeout, CommandTimeout: cfg.SSH.CommandTimeout, MaxParallel: cfg.SSH.MaxParallel, MaxOutputBytes: cfg.SSH.MaxOutputBytes, Logger: logger})
	if e != nil {
		return nil, nil, nil, e
	}
	s := source.NewSSH(x, tc.Host, cfg.SSH.MaxOutputBytes*8)
	return s, func() { _ = s.Close(); _ = x.Close() }, cfg, nil
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
