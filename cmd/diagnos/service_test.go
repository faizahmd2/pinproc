package main

import (
	"strings"
	"testing"
)

func TestRenderServiceUnit(t *testing.T) {
	unit := renderServiceUnit("/usr/local/bin/vm-native-diagnos", "diagnos", "diagnos", "/var/lib/vm-native-diagnos")
	for _, want := range []string{
		"ExecStart=/usr/local/bin/vm-native-diagnos service run --config /etc/vm-native-diagnos/app.yaml",
		"User=diagnos",
		"Group=diagnos",
		"Restart=on-failure",
		"WantedBy=multi-user.target",
	} {
		if !strings.Contains(unit, want) { t.Fatalf("service unit missing %q:\n%s", want, unit) }
	}
}
