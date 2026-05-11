package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
)

func TestAMLScreenRequiresOperatorOrAdmin(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("viewer", "viewer-token"),
	}
	srv := New(zap.NewNop(), cfg, &fakeStore{}, &stubQueue{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/aml/screen", strings.NewReader(`{"full_name":"Jane Doe"}`))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated: expected 401 got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/aml/screen", strings.NewReader(`{"full_name":"Jane Doe"}`))
	req.Header.Set("Authorization", "Bearer viewer-token")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer: expected 403 got %d", rec.Code)
	}
}

func TestAMLScreenProxiesToConfiguredAMLService(t *testing.T) {
	t.Parallel()

	var gotAuth string
	var gotReq map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("X-API-Key")
		if r.URL.Path != "/api/v1/screen" {
			t.Fatalf("unexpected upstream path %q", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Fatalf("decode upstream payload: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"request_id":"aml-123","risk_level":"high","overall_risk":0.91}`))
	}))
	t.Cleanup(upstream.Close)

	cfg := &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("operator", "operator-token"),
		AML: config.AMLConfig{
			BaseURL:       upstream.URL,
			APIKey:        "aml-secret",
			AllowInsecure: true,
		},
	}
	srv := New(zap.NewNop(), cfg, &fakeStore{}, &stubQueue{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/aml/screen", strings.NewReader(`{
		"full_name":"Jane Doe",
		"country":"NG",
		"dob":"1990-01-01",
		"bvn":"12345678901",
		"entity_type":"person"
	}`))
	req.Header.Set("Authorization", "Bearer operator-token")
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%q", rec.Code, rec.Body.String())
	}
	if gotAuth != "aml-secret" {
		t.Fatalf("upstream X-API-Key: got %q", gotAuth)
	}
	if gotReq["name"] != "Jane Doe" || gotReq["birth_date"] != "1990-01-01" || gotReq["id_number"] != "12345678901" {
		t.Fatalf("unexpected upstream request: %+v", gotReq)
	}
	if gotReq["include_sanctions"] != true || gotReq["include_pep"] != true || gotReq["include_adverse_media"] != true {
		t.Fatalf("default include flags not set: %+v", gotReq)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["risk_level"] != "high" || body["request_id"] != "aml-123" {
		t.Fatalf("unexpected response body: %+v", body)
	}
}

func TestAMLScreenFailsClosedWhenServiceUnconfigured(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("admin", "admin-token"),
	}
	srv := New(zap.NewNop(), cfg, &fakeStore{}, &stubQueue{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/aml/screen", strings.NewReader(`{"full_name":"Jane Doe"}`))
	req.Header.Set("Authorization", "Bearer admin-token")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when AML service is unconfigured got %d", rec.Code)
	}
}
