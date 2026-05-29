package server

import (
	"net/http"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

func (s *Server) handleAIInvestigations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleOperator, roleAdmin)
	if !ok {
		return
	}
	tenantID, err := tenantIDFromQuery(r, principal)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !s.requireTenantAccess(w, r, principal, tenantID, roleOperator, roleAdmin) {
		return
	}
	backend := s.aiOperatorBackend()
	if backend == nil {
		http.Error(w, "ai operator store unavailable", http.StatusServiceUnavailable)
		return
	}
	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	filter := storage.ListAIInvestigationsFilter{
		TenantID:         tenantID,
		Status:           storage.AIInvestigationStatus(strings.TrimSpace(r.URL.Query().Get("status"))),
		TriggerType:      strings.TrimSpace(r.URL.Query().Get("trigger_type")),
		TriggerEventType: strings.TrimSpace(r.URL.Query().Get("trigger_event_type")),
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("node_id")); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			http.Error(w, "invalid node_id", http.StatusBadRequest)
			return
		}
		filter.NodeID = parsed
	}
	items, total, err := backend.ListAIInvestigations(r.Context(), filter, limit, offset)
	if err != nil {
		s.logger.Warn("list ai investigations", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, paginatedResponse[storage.AIInvestigation]{
		Data:       items,
		Pagination: newPaginationMeta(total, limit, offset, len(items)),
	})
}

func (s *Server) handleAIOperatorProposals(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleOperator, roleAdmin)
	if !ok {
		return
	}
	tenantID, err := tenantIDFromQuery(r, principal)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !s.requireTenantAccess(w, r, principal, tenantID, roleOperator, roleAdmin) {
		return
	}
	backend := s.aiOperatorBackend()
	if backend == nil {
		http.Error(w, "ai operator store unavailable", http.StatusServiceUnavailable)
		return
	}
	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	filter := storage.ListAIOperatorProposalsFilter{
		TenantID: tenantID,
		Status:   storage.AIOperatorProposalStatus(strings.TrimSpace(r.URL.Query().Get("status"))),
		Action:   strings.TrimSpace(r.URL.Query().Get("action")),
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("node_id")); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			http.Error(w, "invalid node_id", http.StatusBadRequest)
			return
		}
		filter.NodeID = parsed
	}
	items, total, err := backend.ListAIOperatorProposals(r.Context(), filter, limit, offset)
	if err != nil {
		s.logger.Warn("list ai operator proposals", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, paginatedResponse[storage.AIOperatorProposal]{
		Data:       items,
		Pagination: newPaginationMeta(total, limit, offset, len(items)),
	})
}
