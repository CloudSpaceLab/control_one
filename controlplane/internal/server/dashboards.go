package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// Custom dashboards CRUD.
//
//   GET    /api/v1/dashboards?tenant_id=...
//   POST   /api/v1/dashboards
//   GET    /api/v1/dashboards/{id}
//   PATCH  /api/v1/dashboards/{id}
//   DELETE /api/v1/dashboards/{id}
//   POST   /api/v1/dashboards/{id}/widgets
//   PATCH  /api/v1/dashboards/{id}/widgets/{wid}
//   DELETE /api/v1/dashboards/{id}/widgets/{wid}
//
// Permissions: dashboards.read for GETs, dashboards.write for the rest.

type dashboardCreateRequest struct {
	TenantID    string `json:"tenant_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Shared      bool   `json:"shared"`
}

type dashboardUpdateRequest struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Shared      bool            `json:"shared"`
	Layout      json.RawMessage `json:"layout"`
}

type widgetUpsertRequest struct {
	Title          string          `json:"title"`
	WidgetType     string          `json:"widget_type"`
	Spec           json.RawMessage `json:"spec"`
	NodeIDs        []string        `json:"node_ids"`
	RefreshSeconds int             `json:"refresh_seconds"`
	SortOrder      int             `json:"sort_order"`
}

func (s *Server) handleDashboardsCollection(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "store unavailable", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authorize(w, r, roleViewer); !ok {
			return
		}
		userID, ok := s.currentUserID(w, r)
		if !ok {
			return
		}
		tenantID, err := requiredTenantID(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		list, err := s.store.ListDashboardsForUser(r.Context(), tenantID, userID)
		if err != nil {
			s.logger.Error("list dashboards", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, list)
	case http.MethodPost:
		if _, ok := s.authorize(w, r, roleOperator); !ok {
			return
		}
		userID, ok := s.currentUserID(w, r)
		if !ok {
			return
		}
		var body dashboardCreateRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		tenantID, err := uuid.Parse(body.TenantID)
		if err != nil {
			http.Error(w, "invalid tenant_id", http.StatusBadRequest)
			return
		}
		d, err := s.store.CreateDashboard(r.Context(), tenantID, userID, body.Name, body.Description, body.Shared)
		if err != nil {
			s.logger.Warn("create dashboard", zap.Error(err))
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusCreated, d)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleDashboardSubroutes(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "store unavailable", http.StatusServiceUnavailable)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/dashboards/")
	parts := strings.Split(rest, "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	dashboardID, err := uuid.Parse(parts[0])
	if err != nil {
		http.Error(w, "invalid dashboard id", http.StatusBadRequest)
		return
	}
	userID, ok := s.currentUserID(w, r)
	if !ok {
		return
	}

	// /dashboards/{id}/widgets[...]
	if len(parts) >= 2 && parts[1] == "widgets" {
		s.handleDashboardWidgets(w, r, dashboardID, userID, parts[2:])
		return
	}

	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authorize(w, r, roleViewer); !ok {
			return
		}
		d, err := s.store.GetDashboard(r.Context(), dashboardID, userID)
		if err != nil {
			s.logger.Error("get dashboard", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		if d == nil {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusOK, d)
	case http.MethodPatch:
		if _, ok := s.authorize(w, r, roleOperator); !ok {
			return
		}
		var body dashboardUpdateRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if err := s.store.UpdateDashboard(r.Context(), dashboardID, userID, body.Name, body.Description, body.Shared, body.Layout); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case http.MethodDelete:
		if _, ok := s.authorize(w, r, roleOperator); !ok {
			return
		}
		if err := s.store.DeleteDashboard(r.Context(), dashboardID, userID); err != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, PATCH, DELETE")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleDashboardWidgets(w http.ResponseWriter, r *http.Request, dashboardID, userID uuid.UUID, rest []string) {
	if _, ok := s.authorize(w, r, roleOperator); !ok {
		return
	}
	// /dashboards/{id}/widgets
	if len(rest) == 0 || rest[0] == "" {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		var body widgetUpsertRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		// Verify ownership.
		d, err := s.store.GetDashboard(r.Context(), dashboardID, userID)
		if err != nil || d == nil || d.OwnerID != userID {
			http.Error(w, "dashboard not owned by user", http.StatusForbidden)
			return
		}
		nodeIDs := make([]uuid.UUID, 0, len(body.NodeIDs))
		for _, sid := range body.NodeIDs {
			if u, err := uuid.Parse(sid); err == nil {
				nodeIDs = append(nodeIDs, u)
			}
		}
		widget, err := s.store.CreateWidget(r.Context(), storage.DashboardWidget{
			DashboardID:    dashboardID,
			Title:          body.Title,
			WidgetType:     body.WidgetType,
			Spec:           body.Spec,
			NodeIDs:        nodeIDs,
			RefreshSeconds: body.RefreshSeconds,
			SortOrder:      body.SortOrder,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusCreated, widget)
		return
	}

	// /dashboards/{id}/widgets/{wid}
	widgetID, err := uuid.Parse(rest[0])
	if err != nil {
		http.Error(w, "invalid widget id", http.StatusBadRequest)
		return
	}
	d, err := s.store.GetDashboard(r.Context(), dashboardID, userID)
	if err != nil || d == nil || d.OwnerID != userID {
		http.Error(w, "dashboard not owned by user", http.StatusForbidden)
		return
	}
	switch r.Method {
	case http.MethodPatch:
		var body widgetUpsertRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		nodeIDs := make([]uuid.UUID, 0, len(body.NodeIDs))
		for _, sid := range body.NodeIDs {
			if u, err := uuid.Parse(sid); err == nil {
				nodeIDs = append(nodeIDs, u)
			}
		}
		if err := s.store.UpdateWidget(r.Context(), storage.DashboardWidget{
			ID:             widgetID,
			DashboardID:    dashboardID,
			Title:          body.Title,
			WidgetType:     body.WidgetType,
			Spec:           body.Spec,
			NodeIDs:        nodeIDs,
			RefreshSeconds: body.RefreshSeconds,
			SortOrder:      body.SortOrder,
		}); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case http.MethodDelete:
		if err := s.store.DeleteWidget(r.Context(), widgetID); err != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "PATCH, DELETE")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

// currentUserID resolves the local user id from the session principal.
// Returns false when the caller isn't a session-backed user (agents,
// static tokens, OIDC) — those can't own dashboards.
func (s *Server) currentUserID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal.Type != "user" {
		http.Error(w, "user-session required", http.StatusForbidden)
		return uuid.Nil, false
	}
	id, err := uuid.Parse(principal.Subject)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return uuid.Nil, false
	}
	return id, true
}
