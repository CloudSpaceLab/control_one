package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// RBAC + permissions endpoints. CISO-admin-grade UI uses these to
// configure who has access to what.
//
//   GET  /api/v1/permissions                — catalog
//   GET  /api/v1/roles/permissions          — every role + its grants
//   POST /api/v1/roles/                     — create custom role (POST /api/v1/roles/)
//   PUT  /api/v1/roles/{id}/permissions     — replace grants
//   DELETE /api/v1/roles/{id}               — remove a custom role

func (s *Server) handlePermissions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}
	perms, err := s.store.ListPermissions(r.Context())
	if err != nil {
		s.logger.Error("list permissions", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, perms)
}

func (s *Server) handleRolesWithPermissions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}
	roles, err := s.store.ListRolesWithPermissions(r.Context())
	if err != nil {
		s.logger.Error("list roles with permissions", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, roles)
}

type roleCreateRequest struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
}

type rolePermsRequest struct {
	Permissions []string `json:"permissions"`
}

func (s *Server) handleRoleSubroutes(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/roles/")
	parts := strings.Split(rest, "/")

	// POST /api/v1/roles/  → create custom role
	if (len(parts) == 0 || parts[0] == "") && r.Method == http.MethodPost {
		if _, ok := s.authorize(w, r, roleAdmin); !ok {
			return
		}
		var body roleCreateRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		role, err := s.store.CreateCustomRole(r.Context(), body.Name, body.Description, body.Permissions)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusCreated, role)
		return
	}

	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	roleID, err := uuid.Parse(parts[0])
	if err != nil {
		http.Error(w, "invalid role id", http.StatusBadRequest)
		return
	}

	// /api/v1/roles/{id}/permissions
	if len(parts) >= 2 && parts[1] == "permissions" {
		if r.Method != http.MethodPut {
			w.Header().Set("Allow", http.MethodPut)
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		if _, ok := s.authorize(w, r, roleAdmin); !ok {
			return
		}
		var body rolePermsRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if err := s.store.SetRolePermissions(r.Context(), roleID, body.Permissions); err != nil {
			s.logger.Error("set role permissions", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// /api/v1/roles/{id}  DELETE
	if r.Method == http.MethodDelete {
		if _, ok := s.authorize(w, r, roleAdmin); !ok {
			return
		}
		if err := s.store.DeleteRoleByID(r.Context(), roleID); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	http.NotFound(w, r)
}
