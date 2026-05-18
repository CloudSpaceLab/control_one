package server

import "testing"

func TestValidateIngestedEventContractForWebAndRemediationEvents(t *testing.T) {
	t.Parallel()

	valid := &IngestedEvent{
		Type:     "web.request",
		SrcIP:    "203.0.113.10",
		BytesOut: 120,
		Details:  map[string]any{"status_code": float64(401)},
		DedupKey: "web.request:test",
	}
	if err := validateIngestedEventContract(valid); err != nil {
		t.Fatalf("valid web.request rejected: %v", err)
	}
	if valid.CorrelationID != valid.DedupKey {
		t.Fatalf("correlation_id = %q, want dedup fallback", valid.CorrelationID)
	}

	invalidIP := &IngestedEvent{Type: "web.request", SrcIP: "not-an-ip"}
	if err := validateIngestedEventContract(invalidIP); err == nil {
		t.Fatal("invalid web.request source IP accepted")
	}

	invalidStatus := &IngestedEvent{
		Type:    "web.request",
		SrcIP:   "203.0.113.10",
		Details: map[string]any{"status_code": float64(799)},
	}
	if err := validateIngestedEventContract(invalidStatus); err == nil {
		t.Fatal("invalid web.request status accepted")
	}

	remediation := &IngestedEvent{Type: "remediation.webserver_block.applied"}
	if err := validateIngestedEventContract(remediation); err == nil {
		t.Fatal("remediation event without correlation accepted")
	}
}
