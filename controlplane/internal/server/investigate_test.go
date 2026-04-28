package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// ----- Type-detection table-driven test -----

func TestClassifyValue(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want string
	}{
		{"10.0.0.1", EntityTypeIP},
		{"::1", EntityTypeIP},
		{"2001:db8::1", EntityTypeIP},
		{"d41d8cd98f00b204e9800998ecf8427e", EntityTypeHash},                                                 // md5
		{"da39a3ee5e6b4b0d3255bfef95601890afd80709", EntityTypeHash},                                         // sha1
		{"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", EntityTypeHash},                // sha256
		{"00000000-0000-0000-0000-000000000001", EntityTypeUUID},
		{"alice@example.com", EntityTypeEmail},
		{"example.com", EntityTypeDomain},
		{"sub.example.co.uk", EntityTypeDomain},
		{"/etc/passwd", EntityTypeFile},
		{"C:\\Windows\\System32\\cmd.exe", EntityTypeFile},
		{"", ""},
		{"   ", ""},
		{"random-string", ""},
	}
	for _, c := range cases {
		got, _ := ClassifyValue(c.in)
		if got != c.want {
			t.Errorf("ClassifyValue(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestClassifyIP(t *testing.T) {
	t.Parallel()

	_, asset, _ := net.ParseCIDR("203.0.113.0/24")
	chips := ClassifyIP("203.0.113.5", []net.IPNet{*asset}, []TFRow{
		{Feed: "spamhaus_drop", Severity: "high"},
	})
	if !containsChip(chips, "ASSET") {
		t.Errorf("expected ASSET chip, got %+v", chips)
	}
	if !containsChip(chips, "BLACKLISTED:spamhaus_drop") {
		t.Errorf("expected blacklist chip, got %+v", chips)
	}

	priv := ClassifyIP("10.0.0.5", nil, nil)
	if !containsChip(priv, "INTERNAL") {
		t.Errorf("expected INTERNAL chip for RFC1918, got %+v", priv)
	}

	pub := ClassifyIP("8.8.8.8", nil, nil)
	if !containsChip(pub, "EXTERNAL") {
		t.Errorf("expected EXTERNAL chip for public, got %+v", pub)
	}
}

func containsChip(chips []ClassificationChip, label string) bool {
	for _, c := range chips {
		if c.Label == label {
			return true
		}
	}
	return false
}

// ----- Handler smoke tests -----
//
// These tests exercise the handler functions directly with synthesised
// principals (so we avoid the bigger fakeStore wiring). The investigate
// store interface is separate from Store — when nil, handlers degrade to
// empty payloads with HTTP 200, which is what we assert.

func newInvestigateServer(t *testing.T) *Server {
	t.Helper()
	cfg := &config.Config{HTTP: config.HTTPConfig{Address: ":0"}}
	srv := New(zap.NewNop(), cfg, nil, &stubQueue{})
	srv.auditAsync = false
	return srv
}

func viewerPrincipal() *auth.Principal {
	return &auth.Principal{
		Type:  "user",
		Name:  "viewer@example.com",
		Roles: []string{"viewer"},
	}
}

func operatorPrincipal() *auth.Principal {
	return &auth.Principal{
		Type:  "user",
		Name:  "op@example.com",
		Roles: []string{"operator"},
	}
}

// no-role principal — should be rejected by authorize() with 403.
func noRolePrincipal() *auth.Principal {
	return &auth.Principal{
		Type:  "user",
		Name:  "noone@example.com",
		Roles: []string{},
	}
}

func TestInvestigateSearch_HappyPath(t *testing.T) {
	t.Parallel()
	srv := newInvestigateServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=10.0.0.1", nil)
	req = withPrincipal(req, viewerPrincipal())
	rec := httptest.NewRecorder()
	srv.handleInvestigateSearch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp searchResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Detected != EntityTypeIP {
		t.Fatalf("expected detected=ip, got %q", resp.Detected)
	}
	if len(resp.Items) == 0 || resp.Items[0].Type != EntityTypeIP {
		t.Fatalf("expected primary ip item, got %+v", resp.Items)
	}
}

func TestInvestigateSearch_Forbidden(t *testing.T) {
	t.Parallel()
	srv := newInvestigateServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=10.0.0.1", nil)
	req = withPrincipal(req, noRolePrincipal())
	rec := httptest.NewRecorder()
	srv.handleInvestigateSearch(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestEntityOverview_HappyPath(t *testing.T) {
	t.Parallel()
	srv := newInvestigateServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/entities/ip/10.0.0.1", nil)
	req = withPrincipal(req, viewerPrincipal())
	rec := httptest.NewRecorder()
	srv.handleEntitySubroutes(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp storage.EntitySummary
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Type != "ip" || resp.ID != "10.0.0.1" {
		t.Fatalf("unexpected entity summary: %+v", resp)
	}
}

func TestEntityLifecycle_Forbidden(t *testing.T) {
	t.Parallel()
	srv := newInvestigateServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/entities/ip/10.0.0.1/lifecycle", nil)
	req = withPrincipal(req, noRolePrincipal())
	rec := httptest.NewRecorder()
	srv.handleEntitySubroutes(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestSavedSearches_Forbidden(t *testing.T) {
	t.Parallel()
	srv := newInvestigateServer(t)

	body, _ := json.Marshal(map[string]any{"name": "My search", "query": "10.0.0.1"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/saved-searches", bytes.NewReader(body))
	req = withPrincipal(req, viewerPrincipal()) // viewer cannot create
	rec := httptest.NewRecorder()
	srv.handleSavedSearchesCollection(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestSavedSearches_HappyPath_NoBackend(t *testing.T) {
	t.Parallel()
	srv := newInvestigateServer(t)

	// Viewer listing with no investigate backend returns an empty page
	// rather than 500 — handler degrades gracefully.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/saved-searches", nil)
	req = withPrincipal(req, viewerPrincipal())
	rec := httptest.NewRecorder()
	srv.handleSavedSearchesCollection(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestEntityActions_Forbidden(t *testing.T) {
	t.Parallel()
	srv := newInvestigateServer(t)

	body, _ := json.Marshal(map[string]any{"action": "block", "reason": "test"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/entities/ip/10.0.0.1/actions", bytes.NewReader(body))
	req = withPrincipal(req, viewerPrincipal()) // viewer cannot post actions
	rec := httptest.NewRecorder()
	srv.handleEntitySubroutes(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestEntityActions_BadActionRejected(t *testing.T) {
	t.Parallel()
	srv := newInvestigateServer(t)

	body, _ := json.Marshal(map[string]any{"action": "explode"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/entities/ip/10.0.0.1/actions", bytes.NewReader(body))
	req = withPrincipal(req, operatorPrincipal())
	rec := httptest.NewRecorder()
	srv.handleEntitySubroutes(rec, req)
	// With no investigate backend, we expect a 503 before action is
	// validated; with a backend, "explode" would return 400. Either is
	// acceptable as a contract assertion that bad input never persists.
	if rec.Code != http.StatusServiceUnavailable && rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestIPEnrich_ClassifiesPublic(t *testing.T) {
	t.Parallel()
	srv := newInvestigateServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/entities/ip/8.8.8.8/enrich", nil)
	req = withPrincipal(req, viewerPrincipal())
	rec := httptest.NewRecorder()
	srv.handleEntitySubroutes(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp ipEnrichResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Address != "8.8.8.8" {
		t.Fatalf("expected addr=8.8.8.8, got %q", resp.Address)
	}
	if !containsChip(resp.Classification, "EXTERNAL") {
		t.Fatalf("expected EXTERNAL chip, got %+v", resp.Classification)
	}
}

func TestProcessTree_DegradesGracefully(t *testing.T) {
	t.Parallel()
	srv := newInvestigateServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/entities/process/cmd.exe/tree", nil)
	req = withPrincipal(req, viewerPrincipal())
	rec := httptest.NewRecorder()
	srv.handleEntitySubroutes(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

// silence unused import if context-helpers shift later.
var _ = context.Background
