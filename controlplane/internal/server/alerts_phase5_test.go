package server

import "testing"

func TestAlertContextStringSkipsMissingAndNilValues(t *testing.T) {
	context := map[string]any{"event_type": nil, "event_type_filter": "ssh.authentication_failure"}
	if got := alertContextString(context, "event_type", "event_type_filter"); got != "ssh.authentication_failure" {
		t.Fatalf("alertContextString = %q", got)
	}
	if got := alertContextString(map[string]any{"event_type": "<nil>"}, "event_type"); got != "" {
		t.Fatalf("literal nil should be ignored, got %q", got)
	}
}
