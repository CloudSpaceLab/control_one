package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// TestAMLRoutesRequireAuth pins the P0 invariant from bugs §4 #1: every AML
// PII-bearing route returns 401 without a valid tenant session, regardless of
// HTTP method or payload shape. The legacy FraudSniper handlers exposed these
// routes anonymously; the controlone Go re-implementation must not.
//
// The test is intentionally exhaustive — it covers each (route, method) pair
// that touches PII so a future refactor cannot quietly drop the auth check on
// one of them. If a new method/path is added to aml.go, add the (method,
// path) tuple here too.
func TestAMLRoutesRequireAuth(t *testing.T) {
	t.Parallel()

	logger := zap.NewNop()
	cfg := &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("viewer", "valid-token"),
	}

	srv := New(logger, cfg, &fakeStore{userRoles: map[uuid.UUID][]string{}}, &stubQueue{})
	handler := srv.Handler()

	piiBody := []byte(`{
		"full_name": "Subject Under Test",
		"bvn": "12345678901",
		"nin": "98765432109",
		"dob": "1990-01-01",
		"address": "1 Test Street, Lagos"
	}`)

	cases := []struct {
		name   string
		method string
		path   string
		body   []byte
	}{
		{"screen_post_with_pii", http.MethodPost, "/api/v1/aml/screen", piiBody},
		{"screen_post_empty", http.MethodPost, "/api/v1/aml/screen", []byte(`{}`)},
		{"verdicts_get", http.MethodGet, "/api/v1/aml/verdicts/", nil},
		{"verdicts_get_with_id", http.MethodGet, "/api/v1/aml/verdicts/some-id", nil},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name+"_unauthenticated", func(t *testing.T) {
			t.Parallel()

			var body *bytes.Reader
			if tc.body != nil {
				body = bytes.NewReader(tc.body)
			} else {
				body = bytes.NewReader(nil)
			}
			req := httptest.NewRequest(tc.method, tc.path, body)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("%s %s without session: expected 401 got %d (body=%q)",
					tc.method, tc.path, rec.Code, rec.Body.String())
			}
		})

		t.Run(tc.name+"_bogus_token", func(t *testing.T) {
			t.Parallel()

			var body *bytes.Reader
			if tc.body != nil {
				body = bytes.NewReader(tc.body)
			} else {
				body = bytes.NewReader(nil)
			}
			req := httptest.NewRequest(tc.method, tc.path, body)
			req.Header.Set("Authorization", "Bearer this-token-does-not-exist")
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("%s %s with bogus token: expected 401 got %d",
					tc.method, tc.path, rec.Code)
			}
		})
	}
}

// TestAMLRoutesRequireOperatorOrAdmin ensures viewers cannot trigger AML
// scans or read verdicts. The verdict response itself reveals sanctions / PEP
// status which is privileged.
func TestAMLRoutesRequireOperatorOrAdmin(t *testing.T) {
	t.Parallel()

	logger := zap.NewNop()
	cfg := &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		// Default-role viewer; the static token resolves to a viewer principal.
		Auth: authWithTokens("viewer", "viewer-token"),
	}

	srv := New(logger, cfg, &fakeStore{userRoles: map[uuid.UUID][]string{}}, &stubQueue{})
	handler := srv.Handler()

	cases := []struct {
		name   string
		method string
		path   string
		body   []byte
	}{
		{"screen_post", http.MethodPost, "/api/v1/aml/screen", []byte(`{"full_name":"x","bvn":"12345678901"}`)},
		{"verdicts_get", http.MethodGet, "/api/v1/aml/verdicts/", nil},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var body *bytes.Reader
			if tc.body != nil {
				body = bytes.NewReader(tc.body)
			} else {
				body = bytes.NewReader(nil)
			}
			req := httptest.NewRequest(tc.method, tc.path, body)
			req.Header.Set("Authorization", "Bearer viewer-token")
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusForbidden {
				t.Fatalf("%s %s as viewer: expected 403 got %d (body=%q)",
					tc.method, tc.path, rec.Code, rec.Body.String())
			}
		})
	}
}

// TestAMLScreenSuccessShape covers the auth-passes path. Until the outbound
// screening client lands, the handler returns 501 — but it MUST do so AFTER
// the auth check, never before. Pinning the success-flow status here means a
// future PR wiring the screening client cannot accidentally regress to a
// "open route, never reached the auth check" shape.
func TestAMLScreenAuthorizedReachesHandler(t *testing.T) {
	t.Parallel()

	logger := zap.NewNop()
	cfg := &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		// admin-role default so the static token authorizes for AML routes.
		Auth: authWithTokens("admin", "admin-token"),
	}

	srv := New(logger, cfg, &fakeStore{userRoles: map[uuid.UUID][]string{}}, &stubQueue{})
	handler := srv.Handler()

	t.Run("screen_with_full_payload_returns_501_not_401", func(t *testing.T) {
		t.Parallel()

		body := []byte(`{
			"full_name": "Test Person",
			"bvn": "12345678901",
			"dob": "1990-01-01",
			"address": "1 Lagos St"
		}`)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/aml/screen", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer admin-token")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		// 501 = auth passed and the handler reached the not-yet-wired
		// outbound integration. Anything else (especially 401 or 200)
		// would mean the auth contract or the scaffold contract regressed.
		if rec.Code != http.StatusNotImplemented {
			t.Fatalf("expected 501 (auth passed, handler reached) got %d body=%q",
				rec.Code, rec.Body.String())
		}
	})

	t.Run("screen_with_empty_payload_returns_400_not_401", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodPost, "/api/v1/aml/screen", bytes.NewReader([]byte(`{}`)))
		req.Header.Set("Authorization", "Bearer admin-token")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		// 400 = auth passed and validate() rejected the empty payload.
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 (auth passed, validation failed) got %d body=%q",
				rec.Code, rec.Body.String())
		}
	})

	t.Run("verdicts_get_returns_200_with_empty_list", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/aml/verdicts/", nil)
		req.Header.Set("Authorization", "Bearer admin-token")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 got %d body=%q", rec.Code, rec.Body.String())
		}
		if !bytes.Contains(rec.Body.Bytes(), []byte(`"data"`)) {
			t.Fatalf("expected data envelope, got %q", rec.Body.String())
		}
	})

	t.Run("screen_get_method_not_allowed_after_auth", func(t *testing.T) {
		t.Parallel()

		// Method check happens before auth in the handler, but the route is
		// still auth-protected — a GET without a token is 401, not 405.
		req := httptest.NewRequest(http.MethodGet, "/api/v1/aml/screen", nil)
		req.Header.Set("Authorization", "Bearer admin-token")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405 got %d", rec.Code)
		}
	})
}

// TestAMLValidateRequest covers the input-shape contract independently of the
// HTTP layer. validate() must reject empty full_name and must refuse a
// name-only payload (mirroring the d-003 sanctions DOB-fallback hardening).
func TestAMLValidateRequest(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		req  amlScreenRequest
		ok   bool
	}{
		{"empty", amlScreenRequest{}, false},
		{"name_only", amlScreenRequest{FullName: "X"}, false},
		{"name_and_dob_only", amlScreenRequest{FullName: "X", DOB: "1990-01-01"}, false},
		{"name_and_address_only", amlScreenRequest{FullName: "X", Address: "1 Lagos"}, false},
		{"name_dob_address", amlScreenRequest{FullName: "X", DOB: "1990-01-01", Address: "1 Lagos"}, true},
		{"name_bvn", amlScreenRequest{FullName: "X", BVN: "12345678901"}, true},
		{"name_nin", amlScreenRequest{FullName: "X", NIN: "98765432109"}, true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.req.validate()
			if tc.ok && err != nil {
				t.Fatalf("expected ok, got %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatalf("expected validation error, got nil")
			}
		})
	}
}

// guard against unused storage import drift
var _ = storage.Tenant{}
