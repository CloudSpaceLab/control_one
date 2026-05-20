package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

func TestAlertDispositionUpdatesContextAndState(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	alertID := uuid.New()
	store := &fakeStore{
		alerts: []storage.Alert{{
			ID:       alertID,
			TenantID: tenantID,
			Source:   "correlation",
			Severity: "high",
			Title:    "Suspicious authentication spike",
			State:    "open",
			Context:  map[string]any{"source_ip": "203.0.113.44"},
			OpenedAt: time.Now().UTC().Add(-time.Hour),
		}},
	}
	srv := &Server{store: store}

	body := bytes.NewBufferString(`{"disposition":"false_positive","reason":"approved scanner maintenance"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/"+alertID.String()+"/disposition", body)
	req = withPrincipal(req, &auth.Principal{Type: "user", Subject: "operator", Roles: []string{roleOperator}})
	rec := httptest.NewRecorder()

	srv.handleAlertSubroutes(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
	}
	var resp alertResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.State != "resolved" {
		t.Fatalf("state = %q, want resolved", resp.State)
	}
	if resp.Disposition == nil || resp.Disposition.Value != "false_positive" || resp.Disposition.Reason != "approved scanner maintenance" {
		t.Fatalf("unexpected disposition: %+v", resp.Disposition)
	}
	if store.alerts[0].State != "resolved" {
		t.Fatalf("stored state = %q, want resolved", store.alerts[0].State)
	}
	disposition := metadataMap(store.alerts[0].Context["disposition"])
	if disposition["value"] != "false_positive" {
		t.Fatalf("stored disposition = %#v", disposition)
	}
}

func TestAlertDispositionRejectsInvalidValue(t *testing.T) {
	t.Parallel()

	alertID := uuid.New()
	srv := &Server{store: &fakeStore{alerts: []storage.Alert{{ID: alertID, TenantID: uuid.New(), State: "open"}}}}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/"+alertID.String()+"/disposition", bytes.NewBufferString(`{"disposition":"maybe"}`))
	req = withPrincipal(req, &auth.Principal{Type: "user", Subject: "operator", Roles: []string{roleOperator}})
	rec := httptest.NewRecorder()

	srv.handleAlertSubroutes(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
	}
}

func TestAlertDispositionRequiresReasonAndFutureSuppressionWindow(t *testing.T) {
	t.Parallel()

	alertID := uuid.New()
	srv := &Server{store: &fakeStore{alerts: []storage.Alert{{ID: alertID, TenantID: uuid.New(), State: "open"}}}}

	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "missing reason", body: `{"disposition":"resolved"}`},
		{name: "suppressed without expiry", body: `{"disposition":"suppressed","reason":"maintenance scanner"}`},
		{name: "suppressed past expiry", body: `{"disposition":"suppressed","reason":"maintenance scanner","suppress_until":"2020-01-01T00:00:00Z"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/"+alertID.String()+"/disposition", bytes.NewBufferString(tc.body))
			req = withPrincipal(req, &auth.Principal{Type: "user", Subject: "operator", Roles: []string{roleOperator}})
			rec := httptest.NewRecorder()

			srv.handleAlertSubroutes(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
			}
		})
	}
}
