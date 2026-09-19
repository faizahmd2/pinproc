package config

import (
	"fmt"
	"os/user"
)

func (c Config) ResolveTarget(name string) (TargetConfig, error) {
	target, ok := c.Targets[name]
	if !ok {
		target = TargetConfig{Host: name, User: c.SSH.User, Port: c.SSH.Port}
	}

	if target.Host == "" {
		return TargetConfig{}, fmt.Errorf(
			"target %q has no host configured",
			name,
		)
	}

	if target.User == "" {
		target.User = c.SSH.User
	}
	if target.User == "" {
		if current, err := user.Current(); err == nil {
			target.User = current.Username
		}
	}
	if target.User == "" {
		return TargetConfig{}, fmt.Errorf(
			"target %q has no user configured",
			name,
		)
	}
	if target.Port == 0 {
		target.Port = c.SSH.Port
	}
	if target.Port == 0 {
		target.Port = 22
	}

	return target, nil
}
