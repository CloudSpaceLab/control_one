package server

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

type auditLogResponse struct {
	ID           string         `json:"id"`
	TenantID     *string        `json:"tenant_id,omitempty"`
	ActorID      *string        `json:"actor_id,omitempty"`
	ActorType    string         `json:"actor_type"`
	Action       string         `json:"action"`
	ResourceType string         `json:"resource_type"`
	ResourceID   *string        `json:"resource_id,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	CreatedAt    string         `json:"created_at"`
}

func (s *Server) handleAuditCollection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if s.store == nil {
		http.Error(w, "audit store unavailable", http.StatusServiceUnavailable)
		return
	}

	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}

	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	filter := storage.AuditLogFilter{
		ActorType:    strings.TrimSpace(r.URL.Query().Get("actor_type")),
		Action:       strings.TrimSpace(r.URL.Query().Get("action")),
		ResourceType: strings.TrimSpace(r.URL.Query().Get("resource_type")),
		ResourceID:   strings.TrimSpace(r.URL.Query().Get("resource_id")),
	}
	if tenantVal := strings.TrimSpace(r.URL.Query().Get("tenant_id")); tenantVal != "" {
		tenantID, parseErr := uuid.Parse(tenantVal)
		if parseErr != nil {
			http.Error(w, "invalid tenant_id", http.StatusBadRequest)
			return
		}
		filter.TenantID = tenantID
	}
	if sinceVal := strings.TrimSpace(r.URL.Query().Get("since")); sinceVal != "" {
		ts, parseErr := time.Parse(time.RFC3339, sinceVal)
		if parseErr != nil {
			http.Error(w, "invalid since timestamp", http.StatusBadRequest)
			return
		}
		filter.Since = &ts
	}
	if untilVal := strings.TrimSpace(r.URL.Query().Get("until")); untilVal != "" {
		ts, parseErr := time.Parse(time.RFC3339, untilVal)
		if parseErr != nil {
			http.Error(w, "invalid until timestamp", http.StatusBadRequest)
			return
		}
		filter.Until = &ts
	}

	entries, total, err := s.store.ListAuditLogs(r.Context(), filter, limit, offset)
	if err != nil {
		s.logger.Error("list audit logs", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	respItems := make([]auditLogResponse, 0, len(entries))
	for _, entry := range entries {
		respItems = append(respItems, newAuditResponse(entry))
	}

	resp := paginatedResponse[auditLogResponse]{
		Data:       respItems,
		Pagination: newPaginationMeta(total, limit, offset, len(respItems)),
	}
	writeJSON(w, http.StatusOK, resp)
}

func newAuditResponse(entry storage.AuditLog) auditLogResponse {
	resp := auditLogResponse{
		ID:           entry.ID.String(),
		ActorType:    entry.ActorType,
		Action:       entry.Action,
		ResourceType: entry.ResourceType,
		CreatedAt:    entry.CreatedAt.UTC().Format(time.RFC3339),
	}
	if entry.TenantID != uuid.Nil {
		tenant := entry.TenantID.String()
		resp.TenantID = &tenant
	}
	if entry.ActorID != uuid.Nil {
		actor := entry.ActorID.String()
		resp.ActorID = &actor
	}
	if entry.ResourceID != nil && strings.TrimSpace(*entry.ResourceID) != "" {
		resp.ResourceID = entry.ResourceID
	}
	if len(entry.Metadata) > 0 {
		resp.Metadata = entry.Metadata
	}
	return resp
}

func (s *Server) recordAudit(ctx context.Context, principal *auth.Principal, tenantID uuid.UUID, action, resourceType, resourceID string, metadata map[string]any) {
	if s.store == nil {
		return
	}
	action = strings.TrimSpace(action)
	resourceType = strings.TrimSpace(resourceType)
	if action == "" || resourceType == "" {
		return
	}
	if metadata == nil {
		metadata = map[string]any{}
	}

	record := func() {
		entry := &storage.AuditLog{
			TenantID:     tenantID,
			Action:       action,
			ResourceType: resourceType,
			Metadata:     metadata,
		}
		if resourceID = strings.TrimSpace(resourceID); resourceID != "" {
			entry.ResourceID = &resourceID
		}
		if principal != nil {
			entry.ActorType = principal.Type
			if strings.TrimSpace(principal.Subject) != "" {
				entry.ActorType = "user"
				if user, err := s.store.GetUserByExternalID(ctx, principal.Subject); err == nil && user != nil {
					entry.ActorID = user.ID
				}
			}
		}
		if _, err := s.store.CreateAuditLog(ctx, entry); err != nil {
			s.logger.Warn("record audit log failed",
				zap.Error(err),
				zap.String("action", action),
				zap.String("resource_type", resourceType),
			)
		}
	}

	if s.auditAsync {
		go record()
	} else {
		record()
	}
}
