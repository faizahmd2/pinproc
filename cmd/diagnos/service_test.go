package main

import (
	"strings"
	"testing"
)

func TestRenderServiceUnit(t *testing.T) {
	unit := renderServiceUnit("/usr/local/bin/vm-native-pinproc", "pinproc", "pinproc", "/var/lib/vm-native-pinproc")
	for _, want := range []string{
		"ExecStart=/usr/local/bin/vm-native-pinproc service run --config /etc/vm-native-pinproc/app.yaml",
		"User=pinproc",
		"Group=pinproc",
		"Restart=on-failure",
		"WantedBy=multi-user.target",
	} {
		if !strings.Contains(unit, want) { t.Fatalf("service unit missing %q:\n%s", want, unit) }
	}
}
