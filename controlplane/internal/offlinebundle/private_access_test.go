package offlinebundle

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/privateaccess"
)

func TestLoadActivePrivateAccessProviderManifests(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	body := makePrivateAccessProviderBundle(t, priv, privateAccessProviderBundleFixture{Now: now})

	verified, err := VerifyArchive(bytes.NewReader(body), ImportOptions{PublicKey: priv.Public().(ed25519.PublicKey), Now: now})
	if err != nil {
		t.Fatalf("verify archive: %v", err)
	}
	root := t.TempDir()
	if _, err := InstallVerified(t.Context(), verified, ImportOptions{RootDir: root, Now: now}); err != nil {
		t.Fatalf("install verified: %v", err)
	}

	manifests, err := LoadActivePrivateAccessProviderManifests(root)
	if err != nil {
		t.Fatalf("load active private-access manifests: %v", err)
	}
	if len(manifests) != 1 {
		t.Fatalf("manifests = %d, want 1", len(manifests))
	}
	if manifests[0].Manifest.Provider != privateaccess.ProviderNetBird || manifests[0].Manifest.Name != "netbird-bank-prod" {
		t.Fatalf("unexpected manifest: %+v", manifests[0].Manifest)
	}
	if manifests[0].ContentReceipt.Type != ContentTypePrivateAccessProviderManifest || manifests[0].ContentReceipt.Name != "netbird-bank-prod" {
		t.Fatalf("missing receipt provenance: %+v", manifests[0].ContentReceipt)
	}
}

func TestVerifyArchiveRejectsInvalidPrivateAccessProviderManifest(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	body := makePrivateAccessProviderBundle(t, priv, privateAccessProviderBundleFixture{
		Now: now,
		Manifest: PrivateAccessProviderManifest{
			SchemaVersion: 1,
			Provider:      privateaccess.ProviderKind("unknown"),
			Name:          "bad",
			Version:       "2026.05.29",
			HTTPImport: PrivateAccessHTTPImportManifest{
				Endpoints: map[string]string{"peers": "/api/peers"},
			},
			Policy: PrivateAccessProviderPolicyManifest{
				Templates: []PrivateAccessPolicyTemplate{{ID: "no-all-to-all", Name: "Disable all-to-all"}},
			},
		},
	})

	_, err = VerifyArchive(bytes.NewReader(body), ImportOptions{PublicKey: priv.Public().(ed25519.PublicKey), Now: now})
	if err == nil || !strings.Contains(err.Error(), "unsupported private-access provider") {
		t.Fatalf("VerifyArchive error = %v, want unsupported provider", err)
	}
}

func TestDeployPrivateAccessProviderManifestsValidate(t *testing.T) {
	for _, provider := range []string{"netbird", "openziti", "headscale"} {
		t.Run(provider, func(t *testing.T) {
			path := filepath.Join("..", "..", "..", "deploy", "private-access", provider, "provider-manifest.json")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			var manifest PrivateAccessProviderManifest
			if err := json.Unmarshal(data, &manifest); err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			if err := ValidatePrivateAccessProviderManifest(manifest); err != nil {
				t.Fatalf("validate %s: %v", path, err)
			}
			if string(manifest.Provider) != provider {
				t.Fatalf("provider = %q, want %q", manifest.Provider, provider)
			}
		})
	}
}

type privateAccessProviderBundleFixture struct {
	Now      time.Time
	Manifest PrivateAccessProviderManifest
}

func makePrivateAccessProviderBundle(t *testing.T, priv ed25519.PrivateKey, f privateAccessProviderBundleFixture) []byte {
	t.Helper()
	if f.Now.IsZero() {
		f.Now = time.Now().UTC()
	}
	if f.Manifest.SchemaVersion == 0 {
		f.Manifest = PrivateAccessProviderManifest{
			SchemaVersion: 1,
			Provider:      privateaccess.ProviderNetBird,
			Name:          "netbird-bank-prod",
			DisplayName:   "Bank Production NetBird",
			Version:       "2026.05.29",
			HTTPImport: PrivateAccessHTTPImportManifest{
				Endpoints: map[string]string{
					"peers":        "/api/peers",
					"groups":       "/api/groups",
					"policies":     "/api/policies",
					"routes":       "/api/routes",
					"audit_events": "/api/events",
				},
				AuthorizationSchemes:   []string{"bearer_token"},
				DefaultIntervalSeconds: 900,
				TLSRequired:            true,
			},
			Policy: PrivateAccessProviderPolicyManifest{
				Guardrails: []string{"Disable default all-to-all before go-live."},
				Templates: []PrivateAccessPolicyTemplate{{
					ID:                "no-all-to-all",
					Name:              "Disable all-to-all",
					SourceGroups:      []string{"secops-admins"},
					DestinationGroups: []string{"prod-mgmt-acl"},
				}},
			},
		}
	}
	content, err := json.Marshal(f.Manifest)
	if err != nil {
		t.Fatalf("marshal private-access manifest: %v", err)
	}
	sum := sha256.Sum256(content)
	bundleManifest := Manifest{
		SchemaVersion: 1,
		BundleID:      "private-access-providers",
		Version:       "2026.05.29",
		Sequence:      1,
		IssuedAt:      f.Now.Add(-time.Hour),
		ExpiresAt:     f.Now.Add(90 * 24 * time.Hour),
		Contents: []ContentFile{{
			Type:    ContentTypePrivateAccessProviderManifest,
			Name:    "netbird-bank-prod",
			Version: "2026.05.29",
			Path:    "content/netbird-provider.json",
			SHA256:  hex.EncodeToString(sum[:]),
		}},
	}
	manifestBytes, err := json.Marshal(bundleManifest)
	if err != nil {
		t.Fatalf("marshal bundle manifest: %v", err)
	}
	sig := ed25519.Sign(priv, manifestBytes)

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	addTarFile(t, tw, ManifestPath, manifestBytes)
	addTarFile(t, tw, SignaturePath, []byte(base64.StdEncoding.EncodeToString(sig)))
	addTarFile(t, tw, "content/netbird-provider.json", content)
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return buf.Bytes()
}
