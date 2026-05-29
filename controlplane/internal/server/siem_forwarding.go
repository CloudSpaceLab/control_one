package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

type siemForwardingDestinationStore interface {
	UpsertSIEMForwardingDestination(context.Context, storage.UpsertSIEMForwardingDestinationParams) (*storage.SIEMForwardingDestination, error)
	ListSIEMForwardingDestinations(context.Context, uuid.UUID, string, int, int) ([]storage.SIEMForwardingDestination, int, error)
}

type siemForwardingDestinationRequest struct {
	Name   string         `json:"name"`
	Kind   string         `json:"kind"`
	Status string         `json:"status,omitempty"`
	URL    string         `json:"url"`
	Config map[string]any `json:"config,omitempty"`
}

type siemForwardingDestinationDTO struct {
	ID               string         `json:"id"`
	TenantID         string         `json:"tenant_id"`
	Name             string         `json:"name"`
	Kind             string         `json:"kind"`
	Status           string         `json:"status"`
	URL              string         `json:"url"`
	Config           map[string]any `json:"config,omitempty"`
	HasCredentialRef bool           `json:"has_credential_ref"`
	CreatedBySubject string         `json:"created_by_subject,omitempty"`
	UpdatedBySubject string         `json:"updated_by_subject,omitempty"`
	CreatedAt        string         `json:"created_at"`
	UpdatedAt        string         `json:"updated_at"`
}

func (s *Server) handleSIEMForwardingDestinations(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		principal, ok := s.authorize(w, r, roleViewer, roleOperator, roleAdmin)
		if !ok {
			return
		}
		s.handleListSIEMForwardingDestinations(w, r, principal)
	case http.MethodPost:
		principal, ok := s.authorize(w, r, roleOperator, roleAdmin)
		if !ok {
			return
		}
		s.handleUpsertSIEMForwardingDestination(w, r, principal)
	default:
		w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPost}, ", "))
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleListSIEMForwardingDestinations(w http.ResponseWriter, r *http.Request, principal *auth.Principal) {
	store, ok := s.siemForwardingDestinationStore(w)
	if !ok {
		return
	}
	tenantID, ok := s.requireTenantAccessFromQuery(w, r, principal, roleViewer, roleOperator, roleAdmin)
	if !ok {
		return
	}
	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	destinations, total, err := store.ListSIEMForwardingDestinations(r.Context(), tenantID, status, limit, offset)
	if err != nil {
		if siemForwardingValidationError(err) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.logger.Error("list SIEM forwarding destinations", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	items := make([]siemForwardingDestinationDTO, 0, len(destinations))
	for _, destination := range destinations {
		items = append(items, siemForwardingDestinationResponse(destination))
	}
	writeJSON(w, http.StatusOK, paginatedResponse[siemForwardingDestinationDTO]{
		Data:       items,
		Pagination: newPaginationMeta(total, limit, offset, len(items)),
	})
}

func (s *Server) handleUpsertSIEMForwardingDestination(w http.ResponseWriter, r *http.Request, principal *auth.Principal) {
	store, ok := s.siemForwardingDestinationStore(w)
	if !ok {
		return
	}
	tenantID, ok := s.requireTenantAccessFromQuery(w, r, principal, roleOperator, roleAdmin)
	if !ok {
		return
	}
	var req siemForwardingDestinationRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	destination, err := store.UpsertSIEMForwardingDestination(r.Context(), storage.UpsertSIEMForwardingDestinationParams{
		TenantID:         tenantID,
		Name:             req.Name,
		Kind:             req.Kind,
		Status:           req.Status,
		URL:              req.URL,
		Config:           req.Config,
		UpdatedBySubject: principalSubject(principal),
	})
	if err != nil {
		if siemForwardingValidationError(err) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.logger.Error("upsert SIEM forwarding destination", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	s.recordAudit(r.Context(), principal, tenantID, "siem_forwarding.destination.upserted", "siem_forwarding_destination", destination.ID.String(), map[string]any{
		"name":               destination.Name,
		"kind":               destination.Kind,
		"status":             destination.Status,
		"url":                destination.URL,
		"config_keys":        siemForwardingConfigKeys(destination.Config),
		"has_credential_ref": siemForwardingHasCredentialRef(destination.Config),
	})
	writeJSON(w, http.StatusOK, siemForwardingDestinationResponse(*destination))
}

func (s *Server) siemForwardingDestinationStore(w http.ResponseWriter) (siemForwardingDestinationStore, bool) {
	store, ok := s.store.(siemForwardingDestinationStore)
	if !ok || store == nil {
		http.Error(w, "SIEM forwarding destination store unavailable", http.StatusServiceUnavailable)
		return nil, false
	}
	return store, true
}

func siemForwardingDestinationResponse(destination storage.SIEMForwardingDestination) siemForwardingDestinationDTO {
	return siemForwardingDestinationDTO{
		ID:               destination.ID.String(),
		TenantID:         destination.TenantID.String(),
		Name:             destination.Name,
		Kind:             destination.Kind,
		Status:           destination.Status,
		URL:              destination.URL,
		Config:           redactSIEMForwardingConfig(destination.Config),
		HasCredentialRef: siemForwardingHasCredentialRef(destination.Config),
		CreatedBySubject: destination.CreatedBySubject,
		UpdatedBySubject: destination.UpdatedBySubject,
		CreatedAt:        formatTime(destination.CreatedAt),
		UpdatedAt:        formatTime(destination.UpdatedAt),
	}
}

func redactSIEMForwardingConfig(config map[string]any) map[string]any {
	if len(config) == 0 {
		return nil
	}
	out := make(map[string]any, len(config))
	for key, value := range config {
		if siemForwardingCredentialRefKey(key) {
			out[key] = "configured"
			continue
		}
		out[key] = value
	}
	return out
}

func siemForwardingHasCredentialRef(config map[string]any) bool {
	for key, value := range config {
		if !siemForwardingCredentialRefKey(key) {
			continue
		}
		if strings.TrimSpace(fmt.Sprint(value)) != "" && strings.TrimSpace(fmt.Sprint(value)) != "<nil>" {
			return true
		}
	}
	return false
}

func siemForwardingCredentialRefKey(key string) bool {
	switch strings.ToLower(strings.ReplaceAll(strings.TrimSpace(key), "-", "_")) {
	case "credential_ref", "secret_ref", "token_ref", "api_key_ref", "bearer_token_ref", "authorization_ref":
		return true
	default:
		return false
	}
}

func siemForwardingConfigKeys(config map[string]any) []string {
	if len(config) == 0 {
		return nil
	}
	keys := make([]string, 0, len(config))
	for key := range config {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func siemForwardingValidationError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, marker := range []string{
		"is required",
		"invalid",
		"non-negative",
		"unsupported siem forwarding",
		"must be",
		"requires credential_ref",
		"secret references",
		"not raw",
		"cannot be before",
	} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

func principalSubject(principal *auth.Principal) string {
	if principal == nil {
		return ""
	}
	return strings.TrimSpace(principal.Subject)
}
