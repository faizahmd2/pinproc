package main

import (
	"strings"
	"testing"
)

func TestRenderServiceUnit(t *testing.T) {
	unit := renderServiceUnit("/usr/local/bin/pinproc", "/var/lib/pinproc")
	for _, want := range []string{
		"ExecStart=/usr/local/bin/pinproc service run --config /etc/pinproc/app.yaml",
		"Restart=on-failure",
		"WantedBy=multi-user.target",
	} {
		if !strings.Contains(unit, want) {
			t.Fatalf("service unit missing %q:\n%s", want, unit)
		}
	}
}

func TestRenderServiceUnitDoesNotManageIdentityOrCapabilities(t *testing.T) {
	unit := renderServiceUnit("/usr/local/bin/pinproc", "/var/lib/pinproc")
	for _, forbidden := range []string{
		"User=",
		"Group=",
		"CapabilityBoundingSet=",
		"AmbientCapabilities=",
		"ProtectSystem=",
		"ProtectHome=",
		"RestrictNamespaces=",
	} {
		if strings.Contains(unit, forbidden) {
			t.Fatalf("service unit must not manage %q:\n%s", forbidden, unit)
		}
	}
}
