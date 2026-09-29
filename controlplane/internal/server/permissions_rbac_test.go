package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"go.uber.org/zap"
)

func TestRBACCatalogIsAdminOnly(t *testing.T) {
	srv := &Server{store: &fakeStore{}, logger: zap.NewNop()}

	tests := []struct {
		name  string
		roles []string
		want  int
	}{
		{name: "admin", roles: []string{roleAdmin}, want: http.StatusOK},
		{name: "ciso", roles: []string{roleCISO}, want: http.StatusForbidden},
		{name: "operator", roles: []string{roleOperator}, want: http.StatusForbidden},
		{name: "viewer", roles: []string{roleViewer}, want: http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name+" permissions", func(t *testing.T) {
			req := withPrincipal(httptest.NewRequest(http.MethodGet, "/api/v1/permissions", nil), &auth.Principal{
				Type: "user", Subject: tt.name, Roles: tt.roles,
			})
			rec := httptest.NewRecorder()
			srv.handlePermissions(rec, req)
			if rec.Code != tt.want {
				t.Fatalf("permissions status = %d, want %d", rec.Code, tt.want)
			}
		})

		t.Run(tt.name+" role catalog", func(t *testing.T) {
			req := withPrincipal(httptest.NewRequest(http.MethodGet, "/api/v1/roles/permissions", nil), &auth.Principal{
				Type: "user", Subject: tt.name, Roles: tt.roles,
			})
			rec := httptest.NewRecorder()
			srv.handleRolesWithPermissions(rec, req)
			if rec.Code != tt.want {
				t.Fatalf("role catalog status = %d, want %d", rec.Code, tt.want)
			}
		})
	}
}
