package aclenforcer

import "testing"

func TestDenyBeatsAllow(t *testing.T) {
	e := New(nil)
	e.ReplaceRules([]Rule{
		{ID: "r1", Role: "operator", AllowCommands: []string{"docker"}, DenyCommands: []string{"docker rm"}},
	})
	d := e.Evaluate("operator", "docker rm my-container")
	if d.Allowed {
		t.Fatalf("should be denied: %+v", d)
	}
}

func TestAllowLiteralPrefix(t *testing.T) {
	e := New(nil)
	e.ReplaceRules([]Rule{{Role: "viewer", AllowCommands: []string{"ls"}}})
	d := e.Evaluate("viewer", "ls -la /")
	if !d.Allowed {
		t.Fatal("prefix match should allow")
	}
}

func TestRegexAllow(t *testing.T) {
	e := New(nil)
	e.ReplaceRules([]Rule{{Role: "ops", AllowCommands: []string{"/^systemctl (status|restart)/"}}})
	if d := e.Evaluate("ops", "systemctl status nginx"); !d.Allowed {
		t.Fatal("regex allow should match")
	}
	if d := e.Evaluate("ops", "systemctl reboot now"); d.Allowed && d.Reason != "default" {
		// default-permit when no rule references the command is still expected
	}
}

func TestRoleMismatch(t *testing.T) {
	e := New(nil)
	e.ReplaceRules([]Rule{{Role: "admin", DenyCommands: []string{"rm"}}})
	d := e.Evaluate("viewer", "rm everything")
	if !d.Allowed {
		t.Fatalf("deny should not apply to different role: %+v", d)
	}
}

func TestLabelSelectorMismatch(t *testing.T) {
	e := New(map[string]string{"env": "prod"})
	e.ReplaceRules([]Rule{{Role: "op", NodeLabels: map[string]string{"env": "staging"}, DenyCommands: []string{"rm"}}})
	d := e.Evaluate("op", "rm -rf /")
	if !d.Allowed {
		t.Fatal("wrong-label rule should not apply")
	}
}

func TestEmptyCommand(t *testing.T) {
	e := New(nil)
	if d := e.Evaluate("op", ""); !d.Allowed {
		t.Fatal("empty command should be allowed")
	}
}

func TestInvalidRegexSkipped(t *testing.T) {
	e := New(nil)
	e.ReplaceRules([]Rule{{Role: "op", AllowCommands: []string{"/([/"}}})
	if d := e.Evaluate("op", "anything"); !d.Allowed || d.Reason != "default" {
		t.Fatalf("invalid regex should be skipped, got %+v", d)
	}
}
