package server

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/offlinebundle"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

func TestOfflineBundleImportRecordsAuditAndEnablesOfflineEnrichment(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	root := t.TempDir()
	keyPath := writeOfflinePublicKey(t, pub)
	tenantID := uuid.New()
	userID := uuid.New()
	store := &offlineBundleFakeStore{fakeStore: &fakeStore{}, active: map[string]storage.OfflineContentBundle{}}
	s := &Server{
		logger:             zap.NewNop(),
		store:              store,
		cfg:                &config.Config{OfflineContent: config.OfflineContentConfig{Enabled: true, RootDir: root, PublicKeyFile: keyPath, MaxBundleBytes: 10 << 20}},
		offlineContentRoot: root,
	}
	body := makeServerOfflineBundle(t, priv, "c1-bank", "2026.05.18", 1, time.Now().UTC(), false)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/offline-bundles?tenant_id="+tenantID.String(), bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), auth.ContextKeyPrincipal, &auth.Principal{Subject: userID.String(), Roles: []string{roleAdmin}}))
	rec := httptest.NewRecorder()
	s.handleOfflineBundles(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if len(store.rows) != 1 || store.rows[0].BundleID != "c1-bank" {
		t.Fatalf("rows = %#v, want imported c1-bank", store.rows)
	}
	if len(store.audits) != 1 || store.audits[0].Status != "imported" {
		t.Fatalf("audits = %#v, want imported audit", store.audits)
	}
	enrich := s.lookupIPBehaviorEnrichment(context.Background(), "203.0.113.42", map[string]map[string]any{})
	if enrich["country_code"] != "NG" || enrich["asn"] != "AS64500" || enrich["content_bundle_id"] != "c1-bank" {
		t.Fatalf("offline enrichment = %#v, want NG/AS64500 provenance", enrich)
	}
}

func TestOfflineBundleImportRejectsInvalidSignatureAndAudits(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	_, wrongPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate wrong key: %v", err)
	}
	root := t.TempDir()
	keyPath := writeOfflinePublicKey(t, pub)
	tenantID := uuid.New()
	store := &offlineBundleFakeStore{fakeStore: &fakeStore{}, active: map[string]storage.OfflineContentBundle{}}
	s := &Server{
		logger:             zap.NewNop(),
		store:              store,
		cfg:                &config.Config{OfflineContent: config.OfflineContentConfig{Enabled: true, RootDir: root, PublicKeyFile: keyPath, MaxBundleBytes: 10 << 20}},
		offlineContentRoot: root,
	}
	body := makeServerOfflineBundle(t, wrongPriv, "c1-bank", "2026.05.18", 1, time.Now().UTC(), false)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/offline-bundles?tenant_id="+tenantID.String(), bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), auth.ContextKeyPrincipal, &auth.Principal{Subject: uuid.NewString(), Roles: []string{roleAdmin}}))
	rec := httptest.NewRecorder()
	s.handleOfflineBundles(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body=%s, want 401", rec.Code, rec.Body.String())
	}
	if len(store.rows) != 0 {
		t.Fatalf("invalid bundle should not be recorded: %#v", store.rows)
	}
	if len(store.audits) != 1 || store.audits[0].Status != "rejected" {
		t.Fatalf("audits = %#v, want rejected audit", store.audits)
	}
}

type offlineBundleFakeStore struct {
	*fakeStore
	rows   []storage.OfflineContentBundle
	audits []storage.OfflineContentBundleAuditParams
	active map[string]storage.OfflineContentBundle
}

func (f *offlineBundleFakeStore) ActiveOfflineContentBundle(_ context.Context, tenantID uuid.UUID, bundleID string) (*storage.OfflineContentBundle, error) {
	if f.active == nil {
		return nil, nil
	}
	row, ok := f.active[tenantID.String()+"|"+bundleID]
	if !ok {
		return nil, nil
	}
	return &row, nil
}

func (f *offlineBundleFakeStore) ListOfflineContentBundles(_ context.Context, filter storage.OfflineContentBundleFilter, limit, offset int) ([]storage.OfflineContentBundle, int, error) {
	out := make([]storage.OfflineContentBundle, 0, len(f.rows))
	for _, row := range f.rows {
		if filter.TenantID != uuid.Nil && row.TenantID != filter.TenantID {
			continue
		}
		if filter.BundleID != "" && row.BundleID != filter.BundleID {
			continue
		}
		if filter.Status != "" && row.Status != filter.Status {
			continue
		}
		out = append(out, row)
	}
	total := len(out)
	if limit <= 0 {
		limit = 50
	}
	if offset > len(out) {
		return nil, total, nil
	}
	end := offset + limit
	if end > len(out) {
		end = len(out)
	}
	return out[offset:end], total, nil
}

func (f *offlineBundleFakeStore) RecordOfflineContentBundle(_ context.Context, p storage.RecordOfflineContentBundleParams) (*storage.OfflineContentBundle, error) {
	row := storage.OfflineContentBundle{
		ID:                   uuid.New(),
		TenantID:             p.TenantID,
		BundleID:             p.BundleID,
		Version:              p.Version,
		Sequence:             p.Sequence,
		Status:               p.Status,
		PublicKeyFingerprint: p.PublicKeyFingerprint,
		Signature:            p.Signature,
		ManifestSHA256:       p.ManifestSHA256,
		StoragePath:          p.StoragePath,
		Manifest:             p.Manifest,
		Contents:             p.Contents,
		Warnings:             p.Warnings,
		ImportedAt:           p.ImportedAt,
		CreatedAt:            p.ImportedAt,
		UpdatedAt:            p.ImportedAt,
	}
	f.rows = append(f.rows, row)
	if f.active == nil {
		f.active = map[string]storage.OfflineContentBundle{}
	}
	f.active[p.TenantID.String()+"|"+p.BundleID] = row
	return &row, nil
}

func (f *offlineBundleFakeStore) RecordOfflineContentBundleAudit(_ context.Context, p storage.OfflineContentBundleAuditParams) error {
	f.audits = append(f.audits, p)
	return nil
}

func writeOfflinePublicKey(t *testing.T, pub ed25519.PublicKey) string {
	t.Helper()
	path := t.TempDir() + "/offline.pub"
	if err := os.WriteFile(path, pub, 0o600); err != nil {
		t.Fatalf("write public key: %v", err)
	}
	return path
}

func makeServerOfflineBundle(t *testing.T, priv ed25519.PrivateKey, bundleID, version string, sequence int64, now time.Time, expired bool) []byte {
	t.Helper()
	content := []byte(`{"records":[{"cidr":"203.0.113.0/24","country":"Nigeria","country_code":"NG","asn":"AS64500","isp":"Offline ISP","reputation_score":77}]}`)
	sum := sha256.Sum256(content)
	expiresAt := now.Add(24 * time.Hour)
	if expired {
		expiresAt = now.Add(-time.Hour)
	}
	contentExpires := now.Add(24 * time.Hour)
	manifest := offlinebundle.Manifest{
		SchemaVersion: 1,
		BundleID:      bundleID,
		Version:       version,
		Sequence:      sequence,
		IssuedAt:      now.Add(-time.Hour),
		ExpiresAt:     expiresAt,
		Contents: []offlinebundle.ContentFile{{
			Type:      "ip_enrichment",
			Name:      "default",
			Version:   version,
			Path:      "content/ip-enrichment.json",
			SHA256:    hex.EncodeToString(sum[:]),
			ExpiresAt: &contentExpires,
		}},
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	sig := ed25519.Sign(priv, manifestBytes)
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	addServerTarFile(t, tw, offlinebundle.ManifestPath, manifestBytes)
	addServerTarFile(t, tw, offlinebundle.SignaturePath, []byte(base64.StdEncoding.EncodeToString(sig)))
	addServerTarFile(t, tw, "content/ip-enrichment.json", content)
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return buf.Bytes()
}

func addServerTarFile(t *testing.T, tw *tar.Writer, name string, body []byte) {
	t.Helper()
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body))}); err != nil {
		t.Fatalf("write header: %v", err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatalf("write body: %v", err)
	}
}
