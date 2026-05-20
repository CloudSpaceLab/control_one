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
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/ipintel"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/offlinebundle"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/threatintel"
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
	enrich := s.lookupIPBehaviorEnrichment(context.Background(), uuid.Nil, "203.0.113.42", map[string]map[string]any{})
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

func TestOfflineBundleImportUpsertsVulnerabilityFindingsFromSignedFeed(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	root := t.TempDir()
	keyPath := writeOfflinePublicKey(t, pub)
	tenantID := uuid.New()
	nodeID := uuid.New()
	arch := "amd64"
	store := &offlineBundleFakeStore{
		fakeStore: &fakeStore{
			nodes: []storage.Node{{ID: nodeID, TenantID: tenantID, Hostname: "app-01"}},
			nodePackages: map[uuid.UUID][]storage.NodePackage{
				nodeID: {{
					NodeID:  nodeID,
					Name:    "openssl",
					Version: "3.0.2-0ubuntu1.14",
					Source:  "apt",
					Arch:    &arch,
				}},
			},
		},
		active: map[string]storage.OfflineContentBundle{},
	}
	s := &Server{
		logger:             zap.NewNop(),
		store:              store,
		cfg:                &config.Config{OfflineContent: config.OfflineContentConfig{Enabled: true, RootDir: root, PublicKeyFile: keyPath, MaxBundleBytes: 10 << 20}},
		offlineContentRoot: root,
	}
	body := makeServerVulnerabilityBundle(t, priv, "ubuntu-vuln", "2026.05.19", 3, time.Now().UTC())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/offline-bundles?tenant_id="+tenantID.String(), bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), auth.ContextKeyPrincipal, &auth.Principal{Subject: uuid.NewString(), Roles: []string{roleAdmin}}))
	rec := httptest.NewRecorder()
	s.handleOfflineBundles(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if len(store.vulnerabilityFindings) != 1 {
		t.Fatalf("vulnerability findings = %#v, want one", store.vulnerabilityFindings)
	}
	finding := store.vulnerabilityFindings[0]
	if finding.CVEID != "CVE-2026-0001" || finding.PackageName != "openssl" || finding.FixedVersion != "3.0.2-0ubuntu1.15" {
		t.Fatalf("finding lost CVE/package/fixed version evidence: %+v", finding)
	}
	if finding.EvidenceSource != "offline_vulnerability_feed" || finding.Evidence["feed_bundle"] != "ubuntu-vuln" {
		t.Fatalf("finding missing signed feed provenance: %+v", finding)
	}
}

func TestOfflineBundleImportReconcilesVulnerabilitiesWhenNoMatches(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	root := t.TempDir()
	keyPath := writeOfflinePublicKey(t, pub)
	tenantID := uuid.New()
	nodeID := uuid.New()
	arch := "amd64"
	observedAt := time.Date(2026, 5, 19, 10, 0, 0, 0, time.UTC)
	store := &offlineBundleFakeStore{
		fakeStore: &fakeStore{
			nodes: []storage.Node{{ID: nodeID, TenantID: tenantID, Hostname: "app-01"}},
			nodePackages: map[uuid.UUID][]storage.NodePackage{
				nodeID: {{
					NodeID:  nodeID,
					Name:    "openssl",
					Version: "3.0.2-0ubuntu1.13",
					Source:  "apt",
					Arch:    &arch,
				}},
			},
			vulnerabilityFindings: []storage.VulnerabilityFinding{{
				ID:               uuid.New(),
				TenantID:         tenantID,
				NodeID:           nodeID,
				PackageName:      "openssl",
				InstalledVersion: "3.0.2-0ubuntu1.13",
				PackageSource:    "apt",
				Arch:             arch,
				CVEID:            "CVE-2026-STALE",
				Severity:         "high",
				EvidenceSource:   "offline_vulnerability_feed",
				FirstSeenAt:      observedAt.Add(-time.Hour),
				LastSeenAt:       observedAt.Add(-time.Hour),
			}},
		},
		active: map[string]storage.OfflineContentBundle{},
	}
	s := &Server{
		logger:             zap.NewNop(),
		store:              store,
		cfg:                &config.Config{OfflineContent: config.OfflineContentConfig{Enabled: true, RootDir: root, PublicKeyFile: keyPath, MaxBundleBytes: 10 << 20}},
		offlineContentRoot: root,
	}
	body := makeServerVulnerabilityBundle(t, priv, "ubuntu-vuln", "2026.05.19", 3, observedAt)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/offline-bundles?tenant_id="+tenantID.String(), bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), auth.ContextKeyPrincipal, &auth.Principal{Subject: uuid.NewString(), Roles: []string{roleAdmin}}))
	rec := httptest.NewRecorder()
	s.handleOfflineBundles(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if len(store.vulnerabilityFindings) != 1 {
		t.Fatalf("vulnerability findings = %#v, want stale row only", store.vulnerabilityFindings)
	}
	if store.vulnerabilityFindings[0].ResolvedAt == nil {
		t.Fatalf("expected empty match run to resolve stale scoped finding: %+v", store.vulnerabilityFindings[0])
	}
}

func TestIPBehaviorEnrichmentUsesLocalThreatIntelWithoutLiveLookup(t *testing.T) {
	tenantID := uuid.New()
	var providerCalls int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&providerCalls, 1)
		http.Error(w, "unexpected live lookup", http.StatusInternalServerError)
	}))
	defer upstream.Close()
	mgr := threatintel.New(threatintel.Config{
		RefreshInterval: time.Hour,
		HTTPTimeout:     time.Second,
		Sources: []threatintel.Source{staticThreatSource{indicators: []threatintel.Indicator{{
			TenantID:  tenantID.String(),
			IP:        "45.135.193.156",
			Feed:      "abuseipdb",
			Category:  "abuse",
			Score:     100,
			FirstSeen: time.Date(2026, 5, 18, 18, 0, 2, 0, time.UTC),
		}}}},
	}, zap.NewNop())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go mgr.Start(ctx)
	waitThreatIntelCurrent(t, mgr)

	s := &Server{
		logger:      zap.NewNop(),
		threatIntel: mgr,
		ipIntel: ipintel.New(config.IPIntelConfig{
			Enabled:        true,
			IpqueryBaseURL: upstream.URL,
			CacheTTL:       time.Minute,
			HTTPTimeout:    time.Second,
		}, ipintel.NewMemCache()),
	}
	enrich := s.lookupIPBehaviorEnrichment(context.Background(), tenantID, "45.135.193.156", map[string]map[string]any{})
	if got := atomic.LoadInt32(&providerCalls); got != 0 {
		t.Fatalf("request-path enrichment called live provider %d time(s)", got)
	}
	if enrich["reputation_score"] != 100 || firstThreatFeedName(enrich["threat_feeds"]) != "abuseipdb" {
		t.Fatalf("expected local blacklist enrichment, got %#v", enrich)
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

func makeServerVulnerabilityBundle(t *testing.T, priv ed25519.PrivateKey, bundleID, version string, sequence int64, now time.Time) []byte {
	t.Helper()
	feed := offlinebundle.VulnerabilityFeed{
		SchemaVersion: 1,
		Source:        "ubuntu-usn-offline",
		Advisories: []offlinebundle.VulnerabilityAdvisory{{
			CVEID:       "CVE-2026-0001",
			Severity:    "high",
			KEV:         true,
			AdvisoryURL: "https://vendor.example/advisories/CVE-2026-0001",
			References:  []string{"https://nvd.nist.gov/vuln/detail/CVE-2026-0001"},
			AffectedPackages: []offlinebundle.VulnerabilityAffectedPkg{{
				Name:              "openssl",
				Source:            "apt",
				Arch:              "amd64",
				InstalledVersions: []string{"3.0.2-0ubuntu1.14"},
				FixedVersion:      "3.0.2-0ubuntu1.15",
			}},
		}},
	}
	content, err := json.Marshal(feed)
	if err != nil {
		t.Fatalf("marshal vulnerability feed: %v", err)
	}
	sum := sha256.Sum256(content)
	contentExpires := now.Add(24 * time.Hour)
	manifest := offlinebundle.Manifest{
		SchemaVersion: 1,
		BundleID:      bundleID,
		Version:       version,
		Sequence:      sequence,
		IssuedAt:      now.Add(-time.Hour),
		ExpiresAt:     now.Add(24 * time.Hour),
		Contents: []offlinebundle.ContentFile{{
			Type:      offlinebundle.ContentTypeVulnerabilityFeed,
			Name:      "ubuntu",
			Version:   version,
			Path:      "content/vulnerability-feed.json",
			SHA256:    hex.EncodeToString(sum[:]),
			ExpiresAt: &contentExpires,
			Metadata: map[string]any{
				"import_modes": []string{"direct", "proxy", "airgapped"},
			},
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
	addServerTarFile(t, tw, "content/vulnerability-feed.json", content)
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
