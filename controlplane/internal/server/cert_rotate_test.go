package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// writeTestCA writes a self-signed CA certificate + key into dir and returns
// their absolute file paths. The CA is used by handleRotateCert via
// generateCASignedClientCert so tests can issue real PEM certs without needing
// an external CA.
func writeTestCA(t *testing.T, dir string) (certPath, keyPath string) {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate CA key: %v", err)
	}

	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 62))
	tmpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "controlone-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		IsCA:                  true,
		BasicConstraintsValid: true,
	}

	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create CA cert: %v", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal CA key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	certPath = filepath.Join(dir, "ca.pem")
	keyPath = filepath.Join(dir, "ca.key")
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatalf("write CA cert: %v", err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatalf("write CA key: %v", err)
	}
	return certPath, keyPath
}

// newRotateCertServer wires up a Server whose enrollment CA + fake store are
// pre-seeded with a single node so the rotation path has a real NODE row to
// update.
func newRotateCertServer(t *testing.T) (*Server, *fakeStore, uuid.UUID, uuid.UUID) {
	t.Helper()
	dir := t.TempDir()
	caCert, caKey := writeTestCA(t, dir)

	tenantID := uuid.New()
	nodeID := uuid.New()
	store := &fakeStore{
		tenants: []storage.Tenant{{ID: tenantID, Name: "rotate-tenant", CreatedAt: time.Now()}},
		nodes: []storage.Node{{
			ID:        nodeID,
			TenantID:  tenantID,
			Hostname:  "rotate-host",
			State:     storage.NodeStateActive,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}},
	}
	cfg := &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		Enrollment: config.EnrollmentConfig{
			CACertFile: caCert,
			CAKeyFile:  caKey,
		},
	}
	srv := New(zap.NewNop(), cfg, store, &stubQueue{})
	return srv, store, tenantID, nodeID
}

// agentPrincipal returns a *auth.Principal shaped like an mTLS-authenticated
// agent whose cert Common Name equals nodeID.
func agentPrincipal(nodeID uuid.UUID) *auth.Principal {
	return &auth.Principal{
		Type:  "agent",
		Name:  nodeID.String(),
		Roles: []string{"agent"},
	}
}

// withPrincipal attaches the supplied principal to r's context so the handler
// sees it as if the auth middleware had already run.
func withPrincipal(r *http.Request, p *auth.Principal) *http.Request {
	ctx := context.WithValue(r.Context(), auth.ContextKeyPrincipal, p)
	return r.WithContext(ctx)
}

func TestRotateCertRejectsNonAgent(t *testing.T) {
	t.Parallel()

	srv, _, _, nodeID := newRotateCertServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/nodes/"+nodeID.String()+"/rotate-cert", nil)
	req = withPrincipal(req, &auth.Principal{
		Type:  "user",
		Name:  "admin@example.com",
		Roles: []string{"admin"},
	})
	rec := httptest.NewRecorder()
	srv.handleRotateCert(rec, req, nodeID)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body = %s", rec.Code, rec.Body.String())
	}
}

func TestRotateCertRejectsCNMismatch(t *testing.T) {
	t.Parallel()

	srv, _, _, nodeID := newRotateCertServer(t)
	other := uuid.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/nodes/"+nodeID.String()+"/rotate-cert", nil)
	req = withPrincipal(req, agentPrincipal(other))
	rec := httptest.NewRecorder()
	srv.handleRotateCert(rec, req, nodeID)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body = %s", rec.Code, rec.Body.String())
	}
}

func TestRotateCertIssuesAndUpdatesHistory(t *testing.T) {
	t.Parallel()

	srv, store, _, nodeID := newRotateCertServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/nodes/"+nodeID.String()+"/rotate-cert", nil)
	req = withPrincipal(req, agentPrincipal(nodeID))
	rec := httptest.NewRecorder()
	srv.handleRotateCert(rec, req, nodeID)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	var resp rotateCertResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.NodeID != nodeID.String() {
		t.Fatalf("node_id = %q, want %q", resp.NodeID, nodeID.String())
	}
	if resp.CertPEM == "" || resp.KeyPEM == "" {
		t.Fatalf("expected cert + key PEMs in response")
	}
	// Cert is CA-signed and its CN should match node hostname.
	block, _ := pem.Decode([]byte(resp.CertPEM))
	if block == nil {
		t.Fatalf("response cert not PEM-encoded")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse response cert: %v", err)
	}
	if cert.Subject.CommonName != "rotate-host" {
		t.Fatalf("subject CN = %q, want rotate-host", cert.Subject.CommonName)
	}

	// fakeStore should have exactly one history row with the response's serial.
	history, err := store.GetNodeCertHistory(context.Background(), nodeID)
	if err != nil {
		t.Fatalf("get history: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("history rows = %d, want 1", len(history))
	}
	if history[0].Serial != resp.Serial {
		t.Fatalf("history serial = %q, response serial = %q", history[0].Serial, resp.Serial)
	}

	// Node row should carry the updated cert_serial.
	for _, n := range store.nodes {
		if n.ID == nodeID {
			if !n.CertSerial.Valid || n.CertSerial.String != resp.Serial {
				t.Fatalf("node.cert_serial = %+v, want %q", n.CertSerial, resp.Serial)
			}
			if !n.CertRotatedAt.Valid {
				t.Fatalf("node.cert_rotated_at should be populated")
			}
		}
	}
}

func TestRotateCertChainsOnSecondInvocation(t *testing.T) {
	t.Parallel()

	srv, store, _, nodeID := newRotateCertServer(t)

	do := func() rotateCertResponse {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/nodes/"+nodeID.String()+"/rotate-cert", nil)
		req = withPrincipal(req, agentPrincipal(nodeID))
		rec := httptest.NewRecorder()
		srv.handleRotateCert(rec, req, nodeID)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
		}
		var resp rotateCertResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return resp
	}

	first := do()
	second := do()

	if first.HistoryID == second.HistoryID {
		t.Fatalf("history ids must differ between rotations")
	}
	if second.ReplacedBy != first.HistoryID {
		t.Fatalf("second.ReplacedBy = %q, want %q", second.ReplacedBy, first.HistoryID)
	}

	history, err := store.GetNodeCertHistory(context.Background(), nodeID)
	if err != nil {
		t.Fatalf("get history: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("history rows = %d, want 2", len(history))
	}

	// First row now points at second row and is marked revoked.
	if !history[0].ReplacedBy.Valid || history[0].ReplacedBy.UUID.String() != second.HistoryID {
		t.Fatalf("first row.replaced_by = %+v, want %q", history[0].ReplacedBy, second.HistoryID)
	}
	if !history[0].RevokedAt.Valid {
		t.Fatalf("first row should be revoked after chaining")
	}
}

func TestRotateCertUnknownNodeReturns404(t *testing.T) {
	t.Parallel()

	srv, _, _, _ := newRotateCertServer(t)
	phantom := uuid.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/nodes/"+phantom.String()+"/rotate-cert", nil)
	req = withPrincipal(req, agentPrincipal(phantom))
	rec := httptest.NewRecorder()
	srv.handleRotateCert(rec, req, phantom)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", rec.Code, rec.Body.String())
	}
}

func TestRotateCertOnlyAcceptsPost(t *testing.T) {
	t.Parallel()

	srv, _, _, nodeID := newRotateCertServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/"+nodeID.String()+"/rotate-cert", nil)
	req = withPrincipal(req, agentPrincipal(nodeID))
	rec := httptest.NewRecorder()
	srv.handleRotateCert(rec, req, nodeID)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}
