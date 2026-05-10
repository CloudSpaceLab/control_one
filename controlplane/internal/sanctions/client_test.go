package sanctions

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestNewClient_RejectsEmpty asserts the empty-config branch is hit before
// any URL parsing — we want operators to see "tenant has not configured a
// sanctions provider" rather than a generic parse error.
func TestNewClient_RejectsEmpty(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"", "   ", "\t\n"} {
		_, err := NewClient(Config{TenantID: "t1", BaseURL: raw}, nil)
		if !errors.Is(err, ErrEmptyBaseURL) {
			t.Errorf("BaseURL=%q: expected ErrEmptyBaseURL, got %v", raw, err)
		}
	}
}

// TestNewClient_RejectsPlainHTTP locks in the headline P0 fix: the legacy
// plaintext IP URL must fail loud at construction time. Without AllowInsecure,
// http:// is refused even on a hostname.
func TestNewClient_RejectsPlainHTTP(t *testing.T) {
	t.Parallel()
	cases := []string{
		"http://178.79.176.19/moov-watchman-aml", // the literal legacy URL
		"http://watchman.example.com/v1",          // hostname but plain HTTP
		"http://watchman.example.com:8080/",
	}
	for _, raw := range cases {
		_, err := NewClient(Config{TenantID: "t1", BaseURL: raw}, nil)
		if !errors.Is(err, ErrInsecureURL) {
			t.Errorf("BaseURL=%q: expected ErrInsecureURL, got %v", raw, err)
		}
	}
}

// TestNewClient_RejectsIPLiteralOverHTTPS — even when the tenant flips to
// https, a bare IP literal still fails because cert pinning is not meaningful
// on raw IPs and DNS-based provider rotation is impossible. The override seam
// is AllowInsecure (covered separately).
func TestNewClient_RejectsIPLiteralOverHTTPS(t *testing.T) {
	t.Parallel()
	_, err := NewClient(Config{TenantID: "t1", BaseURL: "https://178.79.176.19/moov-watchman-aml"}, nil)
	if !errors.Is(err, ErrIPLiteralURL) {
		t.Fatalf("expected ErrIPLiteralURL for bare-IP HTTPS URL, got %v", err)
	}
}

// TestNewClient_AcceptsHTTPS confirms the happy path: a tenant-configured
// https://hostname/path URL builds a Client whose base URL round-trips
// unchanged and whose Timeout falls back to DefaultTimeout when zero.
func TestNewClient_AcceptsHTTPS(t *testing.T) {
	t.Parallel()
	const want = "https://watchman.example.com/moov-watchman-aml"
	c, err := NewClient(Config{TenantID: "tenant-A", BaseURL: want}, nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if got := c.BaseURL().String(); got != want {
		t.Errorf("BaseURL round-trip: got %q want %q", got, want)
	}
	if c.TenantID() != "tenant-A" {
		t.Errorf("TenantID round-trip: got %q", c.TenantID())
	}
	if c.HTTPClient().Timeout != DefaultTimeout {
		t.Errorf("Timeout fallback: got %v want %v", c.HTTPClient().Timeout, DefaultTimeout)
	}
}

// TestNewClient_TLSVerifyOnByDefault asserts the constructor plants a TLS
// config that verifies certificates and pins TLS 1.2+. This is the single most
// important property of the package — break this test and you've reintroduced
// the P0 bug under a new name.
func TestNewClient_TLSVerifyOnByDefault(t *testing.T) {
	t.Parallel()
	c, err := NewClient(Config{TenantID: "t1", BaseURL: "https://watchman.example.com/"}, nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	tr, ok := c.HTTPClient().Transport.(*http.Transport)
	if !ok || tr.TLSClientConfig == nil {
		t.Fatalf("expected *http.Transport with TLSClientConfig, got %T", c.HTTPClient().Transport)
	}
	if tr.TLSClientConfig.InsecureSkipVerify {
		t.Error("InsecureSkipVerify must be false by default")
	}
	if tr.TLSClientConfig.MinVersion < tls.VersionTLS12 {
		t.Errorf("MinVersion: got %#x, want >= TLS 1.2 (%#x)", tr.TLSClientConfig.MinVersion, tls.VersionTLS12)
	}
}

// TestNewClient_AllowInsecureOptIn covers the deliberately-narrow override
// path. Sprint 4 ships HTTPS-by-default, but tenants in environments without
// a real CA (early-pilot deployments) need the option. We require the flag to
// be explicitly true.
func TestNewClient_AllowInsecureOptIn(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		cfg  Config
	}{
		{"plain http hostname", Config{TenantID: "t1", BaseURL: "http://watchman.staging/", AllowInsecure: true}},
		{"https IP literal", Config{TenantID: "t1", BaseURL: "https://10.0.0.5/", AllowInsecure: true}},
		{"plain http IP literal", Config{TenantID: "t1", BaseURL: "http://178.79.176.19/", AllowInsecure: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := NewClient(tc.cfg, nil)
			if err != nil {
				t.Fatalf("AllowInsecure should permit %q, got %v", tc.cfg.BaseURL, err)
			}
			if c.BaseURL() == nil {
				t.Error("expected non-nil BaseURL after construction")
			}
		})
	}
}

// TestNewClient_InsecureSkipVerifyHonoured confirms the opt-in plumbing
// reaches the http.Transport. Off by default, on when the tenant flag is set.
func TestNewClient_InsecureSkipVerifyHonoured(t *testing.T) {
	t.Parallel()
	c, err := NewClient(Config{
		TenantID:           "t1",
		BaseURL:            "https://watchman.example.com/",
		InsecureSkipVerify: true,
	}, nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	tr := c.HTTPClient().Transport.(*http.Transport)
	if !tr.TLSClientConfig.InsecureSkipVerify {
		t.Error("InsecureSkipVerify=true on Config should reach Transport")
	}
}

// TestNewClient_RejectsBadScheme guards against a tenant typoing "ftp://..."
// or pasting a URL without a scheme.
func TestNewClient_RejectsBadScheme(t *testing.T) {
	t.Parallel()
	cases := []string{
		"ftp://watchman.example.com/",
		"watchman.example.com/path", // not absolute
	}
	for _, raw := range cases {
		_, err := NewClient(Config{TenantID: "t1", BaseURL: raw}, nil)
		if err == nil {
			t.Errorf("BaseURL=%q: expected error, got nil", raw)
			continue
		}
		if errors.Is(err, ErrEmptyBaseURL) || errors.Is(err, ErrInsecureURL) || errors.Is(err, ErrIPLiteralURL) {
			t.Errorf("BaseURL=%q: should be a generic schema error, got %v", raw, err)
		}
	}
}

// TestClient_Resolve_HappyPath exercises the URL-join helper that follow-up
// screening methods will sit on top of. The base URL contains a path prefix
// (`/v1`), and resolve is expected to merge correctly.
func TestClient_Resolve_HappyPath(t *testing.T) {
	t.Parallel()
	c, err := NewClient(Config{TenantID: "t1", BaseURL: "https://watchman.example.com/v1/"}, nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	got, err := c.resolve("search?q=acme")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	const want = "https://watchman.example.com/v1/search?q=acme"
	if got.String() != want {
		t.Errorf("resolve: got %q want %q", got.String(), want)
	}
}

// TestClient_NewRequest_RoundTrip confirms the request seam works against a
// real (test) HTTPS server, end-to-end. This catches accidental mutations of
// the Transport (e.g. somebody re-introducing InsecureSkipVerify without the
// guard) since httptest.NewTLSServer uses a self-signed cert that *must* fail
// without explicit opt-in.
func TestClient_NewRequest_RoundTrip(t *testing.T) {
	t.Parallel()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.UserAgent(), "control_one-sanctions/") {
			t.Errorf("unexpected user-agent %q", r.UserAgent())
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	// First: TLS-verify-on must reject the self-signed cert. We pass
	// AllowInsecure=true purely to get past the IP-literal guard (httptest
	// listens on 127.0.0.1) — InsecureSkipVerify stays at its default
	// (false), so the request itself must still fail on cert verification.
	c, err := NewClient(Config{TenantID: "t1", BaseURL: srv.URL + "/", AllowInsecure: true}, nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := c.newRequest(ctx, http.MethodGet, "ping")
	if err != nil {
		t.Fatalf("newRequest: %v", err)
	}
	if _, err := c.HTTPClient().Do(req); err == nil {
		t.Error("expected TLS verification failure against httptest TLS server, got nil")
	}

	// Second: opt-in InsecureSkipVerify must let the request through.
	c2, err := NewClient(Config{
		TenantID:           "t1",
		BaseURL:            srv.URL + "/",
		AllowInsecure:      true, // because httptest URL is an IP literal
		InsecureSkipVerify: true,
	}, nil)
	if err != nil {
		t.Fatalf("NewClient (insecure opt-in): %v", err)
	}
	req2, err := c2.newRequest(ctx, http.MethodGet, "ping")
	if err != nil {
		t.Fatalf("newRequest (insecure): %v", err)
	}
	resp, err := c2.HTTPClient().Do(req2)
	if err != nil {
		t.Fatalf("Do (insecure): %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d want 200", resp.StatusCode)
	}
}
