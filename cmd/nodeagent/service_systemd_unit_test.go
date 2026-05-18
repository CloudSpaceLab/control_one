package main

import (
	"strings"
	"testing"
)

func TestSystemdUnitRestartsAfterSuccessfulSelfUpdate(t *testing.T) {
	t.Parallel()

	unit := systemdUnit("/usr/local/bin/controlone-agent", "/etc/control-one/nodeagent.yaml")

	if !strings.Contains(unit, "ExecStart=/usr/local/bin/controlone-agent --config /etc/control-one/nodeagent.yaml") {
		t.Fatalf("unit missing expected ExecStart:\n%s", unit)
	}
	if !strings.Contains(unit, "\nRestart=always\n") {
		t.Fatalf("unit must restart after the agent exits cleanly for self-update:\n%s", unit)
	}
	if strings.Contains(unit, "Restart=on-failure") {
		t.Fatalf("unit must not use Restart=on-failure because successful self-update exits 0:\n%s", unit)
	}
}
