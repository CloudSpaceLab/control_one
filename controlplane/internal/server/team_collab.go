package server

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

type teamMemberResponse struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
}

func (s *Server) handleTeamUsers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleInvestigator, roleOperator, roleAdmin)
	if !ok {
		return
	}
	tenantID, ok := s.requireTeamTenantFromQuery(w, r, principal)
	if !ok {
		return
	}

	users, err := s.store.ListTenantUsers(r.Context(), tenantID, strings.TrimSpace(r.URL.Query().Get("q")), 25)
	if err != nil {
		s.logger.Warn("list team users", zap.Error(err), zap.String("tenant_id", tenantID.String()))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	response := make([]teamMemberResponse, 0, len(users))
	for _, user := range users {
		response = append(response, teamMemberResponse{ID: user.ID.String(), Name: user.Name, Email: user.Email})
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": response})
}

func (s *Server) serveNotifications(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authorize(w, r, roleInvestigator, roleOperator, roleAdmin)
	if !ok {
		return
	}
	tenantID, ok := s.requireTeamTenantFromQuery(w, r, principal)
	if !ok {
		return
	}
	recipientID, ok := s.userIDForPrincipal(r.Context(), principal)
	if !ok {
		http.Error(w, "principal has no local user account", http.StatusForbidden)
		return
	}

	const notificationPath = "/api/v1/notifications"
	switch r.URL.Path {
	case notificationPath:
		if r.Method != http.MethodGet {
			s.notificationsMethodNotAllowed(w)
			return
		}
		s.handleListNotifications(w, r, tenantID, recipientID)
		return
	case notificationPath + "/unread-count":
		if r.Method != http.MethodGet {
			s.notificationsMethodNotAllowed(w)
			return
		}
		s.handleUnreadNotificationsCount(w, r, tenantID, recipientID)
		return
	case notificationPath + "/read-all":
		if r.Method != http.MethodPost {
			s.notificationsMethodNotAllowed(w)
			return
		}
		if _, err := s.store.MarkAllNotificationsRead(r.Context(), tenantID, recipientID); err != nil {
			s.logger.Warn("mark all notifications read", zap.Error(err), zap.String("tenant_id", tenantID.String()), zap.String("recipient_id", recipientID.String()))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"marked_read": true})
		return
	}

	if !strings.HasPrefix(r.URL.Path, notificationPath+"/") {
		s.notificationsMethodNotAllowed(w)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, notificationPath+"/"), "/")
	if len(parts) == 2 && parts[1] == "read" {
		parts = parts[:1]
	} else if len(parts) != 1 || parts[0] == "" {
		s.notificationsMethodNotAllowed(w)
		return
	}
	id, err := uuid.Parse(parts[0])
	if err != nil {
		http.Error(w, "invalid notification id", http.StatusBadRequest)
		return
	}
	if r.Method != http.MethodPost {
		s.notificationsMethodNotAllowed(w)
		return
	}
	if err := s.store.MarkNotificationRead(r.Context(), tenantID, recipientID, id); err != nil {
		s.logger.Warn("mark notification read", zap.Error(err), zap.String("tenant_id", tenantID.String()), zap.String("recipient_id", recipientID.String()), zap.String("notification_id", id.String()))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"read": true})
}

func (s *Server) notificationsMethodNotAllowed(w http.ResponseWriter) {
	w.Header().Set("Allow", "GET, POST")
	http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
}

func notificationPathGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isCanonicalNotificationPath(r) {
			http.Error(w, "invalid notification path", http.StatusBadRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isCanonicalNotificationPath(r *http.Request) bool {
	if r == nil || r.URL == nil {
		return true
	}
	const notificationPath = "/api/v1/notifications"
	path := r.URL.Path
	rawPath := r.URL.EscapedPath()
	lowerRawPath := strings.ToLower(rawPath)
	if !strings.HasPrefix(path, notificationPath) && !strings.HasPrefix(rawPath, notificationPath) && !strings.HasPrefix(lowerRawPath, notificationPath+"%2f") {
		return true
	}
	if path != notificationPath && !strings.HasPrefix(path, notificationPath+"/") && rawPath != notificationPath && !strings.HasPrefix(rawPath, notificationPath+"/") && !strings.HasPrefix(lowerRawPath, notificationPath+"%2f") {
		return true
	}
	if rawPath != path {
		return false
	}
	switch path {
	case notificationPath, notificationPath + "/unread-count", notificationPath + "/read-all":
		return true
	}
	if !strings.HasPrefix(path, notificationPath+"/") {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(path, notificationPath+"/"), "/")
	if len(parts) == 2 && parts[1] == "read" {
		parts = parts[:1]
	}
	if len(parts) != 1 || parts[0] == "" {
		return false
	}
	id, err := uuid.Parse(parts[0])
	return err == nil && id != uuid.Nil
}

func (s *Server) handleListNotifications(w http.ResponseWriter, r *http.Request, tenantID, recipientID uuid.UUID) {
	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	unreadOnly := false
	if raw := strings.TrimSpace(r.URL.Query().Get("unread_only")); raw != "" {
		unreadOnly, err = strconv.ParseBool(raw)
		if err != nil {
			http.Error(w, "invalid unread_only", http.StatusBadRequest)
			return
		}
	}

	items, total, err := s.store.ListNotifications(r.Context(), storage.NotificationFilter{
		TenantID: tenantID, RecipientID: recipientID, UnreadOnly: unreadOnly,
	}, limit, offset)
	if err != nil {
		s.logger.Warn("list notifications", zap.Error(err), zap.String("tenant_id", tenantID.String()), zap.String("recipient_id", recipientID.String()))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	s.hydrateNotificationActorNames(r, items)
	writeJSON(w, http.StatusOK, paginatedResponse[storage.Notification]{
		Data:       items,
		Pagination: newPaginationMeta(total, limit, offset, len(items)),
	})
}

func (s *Server) handleUnreadNotificationsCount(w http.ResponseWriter, r *http.Request, tenantID, recipientID uuid.UUID) {
	count, err := s.store.CountUnreadNotifications(r.Context(), tenantID, recipientID)
	if err != nil {
		s.logger.Warn("count unread notifications", zap.Error(err), zap.String("tenant_id", tenantID.String()), zap.String("recipient_id", recipientID.String()))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"unread": count})
}

// hydrateNotificationActorNames intentionally does not perform per-row user
// lookups. ListNotifications joins actor display names in its storage query;
// the server Store contract does not expose a batch lookup for blank values.
func (s *Server) hydrateNotificationActorNames(_ *http.Request, _ []storage.Notification) {}

func (s *Server) userIDForPrincipal(ctx context.Context, principal *auth.Principal) (uuid.UUID, bool) {
	if s == nil || s.store == nil || principal == nil || strings.TrimSpace(principal.Subject) == "" {
		return uuid.Nil, false
	}
	subject := strings.TrimSpace(principal.Subject)
	if id, err := uuid.Parse(subject); err == nil && id != uuid.Nil {
		if user, err := s.store.GetUser(ctx, id); err == nil && user != nil {
			return user.ID, true
		}
	}
	user, err := s.store.GetUserByExternalID(ctx, subject)
	if err != nil || user == nil {
		return uuid.Nil, false
	}
	return user.ID, true
}
