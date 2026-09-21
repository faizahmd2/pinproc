package main

import (
	"strings"
	"testing"
)

func TestRenderServiceUnit(t *testing.T) {
	unit := renderServiceUnit("/usr/local/bin/pinproc", "pinproc", "pinproc", "/var/lib/pinproc")
	for _, want := range []string{
		"ExecStart=/usr/local/bin/pinproc service run --config /etc/pinproc/app.yaml",
		"User=pinproc",
		"Group=pinproc",
		"Restart=on-failure",
		"WantedBy=multi-user.target",
	} {
		if !strings.Contains(unit, want) {
			t.Fatalf("service unit missing %q:\n%s", want, unit)
		}
	}
}
