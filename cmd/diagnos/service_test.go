package main

import (
	"strings"
	"testing"
)

func TestServiceCommandOnlyExposesRuntimeEntrypoint(t *testing.T) {
	cmd := newServiceCmd()
	if !cmd.Hidden && cmd.Use != "service" {
		t.Fatalf("unexpected command: %#v", cmd)
	}
	children := cmd.Commands()
	if len(children) != 1 {
		t.Fatalf("expected one child command, got %d", len(children))
	}
	if children[0].Name() != "run" {
		t.Fatalf("unexpected child: %s", children[0].Name())
	}
	if !children[0].Hidden {
		t.Fatal("service run must remain hidden")
	}
	if strings.Contains(children[0].UsageString(), "install") || strings.Contains(children[0].UsageString(), "uninstall") {
		t.Fatal("service lifecycle must be owned by the package manager")
	}
}
