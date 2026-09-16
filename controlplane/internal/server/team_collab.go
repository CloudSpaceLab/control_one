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

	suffix := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/notifications"), "/")
	switch r.Method {
	case http.MethodGet:
		switch suffix {
		case "":
			s.handleListNotifications(w, r, tenantID, recipientID)
		case "unread-count":
			s.handleUnreadNotificationsCount(w, r, tenantID, recipientID)
		default:
			s.notificationsMethodNotAllowed(w)
		}
	case http.MethodPost:
		switch suffix {
		case "read-all":
			if _, err := s.store.MarkAllNotificationsRead(r.Context(), tenantID, recipientID); err != nil {
				s.logger.Warn("mark all notifications read", zap.Error(err), zap.String("tenant_id", tenantID.String()), zap.String("recipient_id", recipientID.String()))
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, map[string]bool{"marked_read": true})
		default:
			if suffix == "" {
				s.notificationsMethodNotAllowed(w)
				return
			}
			parts := strings.Split(suffix, "/")
			if len(parts) == 2 && parts[1] != "read" || len(parts) > 2 {
				s.notificationsMethodNotAllowed(w)
				return
			}
			id, err := uuid.Parse(parts[0])
			if err != nil {
				http.Error(w, "invalid notification id", http.StatusBadRequest)
				return
			}
			if err := s.store.MarkNotificationRead(r.Context(), tenantID, recipientID, id); err != nil {
				s.logger.Warn("mark notification read", zap.Error(err), zap.String("tenant_id", tenantID.String()), zap.String("recipient_id", recipientID.String()), zap.String("notification_id", id.String()))
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, map[string]bool{"read": true})
		}
	default:
		s.notificationsMethodNotAllowed(w)
	}
}

func (s *Server) notificationsMethodNotAllowed(w http.ResponseWriter) {
	w.Header().Set("Allow", "GET, POST")
	http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
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

func (s *Server) hydrateNotificationActorNames(r *http.Request, items []storage.Notification) {
	ids := make(map[uuid.UUID]struct{})
	for _, item := range items {
		if item.ActorID != uuid.Nil {
			ids[item.ActorID] = struct{}{}
		}
	}
	for id := range ids {
		user, err := s.store.GetUser(r.Context(), id)
		if err != nil {
			s.logger.Warn("lookup notification actor", zap.Error(err), zap.String("actor_id", id.String()))
			continue
		}
		if user == nil {
			continue
		}
		name := strings.TrimSpace(user.DisplayName.String)
		if name == "" {
			name = strings.TrimSpace(user.Email.String)
			if at := strings.Index(name, "@"); at > 0 {
				name = name[:at]
			}
		}
		for i := range items {
			if items[i].ActorID == id && name != "" {
				items[i].ActorName = name
			}
		}
	}
}

func (s *Server) userIDForPrincipal(ctx context.Context, principal *auth.Principal) (uuid.UUID, bool) {
	if principal == nil || strings.TrimSpace(principal.Subject) == "" {
		return uuid.Nil, false
	}
	user, err := s.store.GetUserByExternalID(ctx, strings.TrimSpace(principal.Subject))
	if err != nil || user == nil {
		return uuid.Nil, false
	}
	return user.ID, true
}
