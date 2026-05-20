package server

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// circuitBreakerStateResponse is the wire shape for a breaker row.
type circuitBreakerStateResponse struct {
	TenantID      string  `json:"tenant_id"`
	RuleID        string  `json:"rule_id"`
	TrippedAt     string  `json:"tripped_at"`
	TrippedReason string  `json:"tripped_reason"`
	AckedAt       *string `json:"acked_at,omitempty"`
	AckedBy       *string `json:"acked_by,omitempty"`
}

// handleRemediationCircuitBreakerSubroutes routes
// /api/v1/remediation/circuit-breaker/:tenantID/:ruleID[/ack].
func (s *Server) handleRemediationCircuitBreakerSubroutes(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/v1/remediation/circuit-breaker/")
	trimmed = strings.Trim(trimmed, "/")
	if trimmed == "" {
		http.NotFound(w, r)
		return
	}
	segments := strings.Split(trimmed, "/")
	if len(segments) < 2 {
		http.Error(w, "missing rule id", http.StatusBadRequest)
		return
	}
	tenantID, err := uuid.Parse(segments[0])
	if err != nil {
		http.Error(w, "invalid tenant id", http.StatusBadRequest)
		return
	}
	ruleID := strings.TrimSpace(segments[1])
	if ruleID == "" {
		http.Error(w, "missing rule id", http.StatusBadRequest)
		return
	}

	switch {
	case len(segments) == 2:
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		principal, ok := s.authorize(w, r, roleViewer, roleOperator, roleAdmin)
		if !ok {
			return
		}
		if !s.requireTenantAccess(w, r, principal, tenantID, roleViewer, roleOperator, roleAdmin) {
			return
		}
		s.handleGetCircuitBreakerState(w, r, tenantID, ruleID)
	case len(segments) == 3 && segments[2] == "ack":
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		principal, ok := s.authorize(w, r, roleAdmin)
		if !ok {
			return
		}
		if !s.requireTenantAccess(w, r, principal, tenantID, roleAdmin) {
			return
		}
		s.handleAckCircuitBreaker(w, r, tenantID, ruleID, principal.Subject)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleGetCircuitBreakerState(w http.ResponseWriter, r *http.Request, tenantID uuid.UUID, ruleID string) {
	state, err := s.store.GetCircuitBreakerState(r.Context(), tenantID, ruleID)
	if err != nil {
		s.logger.Error("get circuit breaker state", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if state == nil {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, circuitBreakerStateToResponse(state))
}

func (s *Server) handleAckCircuitBreaker(w http.ResponseWriter, r *http.Request, tenantID uuid.UUID, ruleID, ackerSubject string) {
	ackerID, _ := uuid.Parse(strings.TrimSpace(ackerSubject))
	updated, err := s.store.AckCircuitBreaker(r.Context(), tenantID, ruleID, ackerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		s.logger.Error("ack circuit breaker", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	s.emitRemediationSafetyEvent(r.Context(), tenantID, EventRemediationCircuitBreakerAcked, map[string]any{
		"tenant_id": tenantID.String(),
		"rule_id":   ruleID,
		"acker_id":  ackerID.String(),
		"acked_at":  time.Now().UTC().Format(time.RFC3339),
	})

	writeJSON(w, http.StatusOK, circuitBreakerStateToResponse(updated))

	s.recordAudit(r.Context(), s.systemActor(), tenantID, "remediation.circuit_breaker_acked", "circuit_breaker", ruleID, map[string]any{
		"rule_id": ruleID,
	})
}

func circuitBreakerStateToResponse(s *storage.RemediationCircuitBreakerState) circuitBreakerStateResponse {
	resp := circuitBreakerStateResponse{
		TenantID:      s.TenantID.String(),
		RuleID:        s.RuleID,
		TrippedAt:     s.TrippedAt.UTC().Format(time.RFC3339),
		TrippedReason: s.TrippedReason,
	}
	if s.AckedAt != nil {
		t := s.AckedAt.UTC().Format(time.RFC3339)
		resp.AckedAt = &t
	}
	if s.AckedBy != nil {
		a := s.AckedBy.String()
		resp.AckedBy = &a
	}
	return resp
}
