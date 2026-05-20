package main

import (
	"testing"

	"github.com/CloudSpaceLab/control_one/internal/webservercontrol"
)

func TestBindWebserverReceiptAddsJobContractMetadata(t *testing.T) {
	t.Parallel()

	receipt := webservercontrol.ConfigReceipt{
		ValidationStatus: "passed",
		ReloadStatus:     "reloaded",
		Metadata:         map[string]any{"health_status": "ok"},
	}
	detail := webserverActionDetail{
		ContractVersion:     webserverJobContract,
		IdempotencyKey:      "job:job-1",
		CorrelationID:       "webserver:job:job-1",
		WebserverInstanceID: "instance-1",
	}

	bindWebserverReceipt(&receipt, detail, webserverJobBlocklist, "job-1")

	if receipt.Action != webserverJobBlocklist {
		t.Fatalf("Action = %q, want %q", receipt.Action, webserverJobBlocklist)
	}
	for key, want := range map[string]string{
		"contract_version":      webserverJobContract,
		"job_id":                "job-1",
		"action":                webserverJobBlocklist,
		"idempotency_key":       "job:job-1",
		"correlation_id":        "webserver:job:job-1",
		"webserver_instance_id": "instance-1",
	} {
		if got, _ := receipt.Metadata[key].(string); got != want {
			t.Fatalf("Metadata[%s] = %q, want %q (metadata=%#v)", key, got, want, receipt.Metadata)
		}
	}
	if got, _ := receipt.Metadata["health_status"].(string); got != "ok" {
		t.Fatalf("existing metadata was not preserved: %#v", receipt.Metadata)
	}
}
