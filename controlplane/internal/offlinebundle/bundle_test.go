package offlinebundle

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
	"errors"
	"testing"
	"time"
)

func TestImportSignedBundleAndOfflineLookup(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	body := makeOfflineBundle(t, priv, offlineBundleFixture{
		BundleID: "c1-bank-core",
		Version:  "2026.05.18",
		Sequence: 7,
		Now:      now,
		Records: []ipRecord{{
			CIDR:            "203.0.113.0/24",
			Country:         "Nigeria",
			CountryCode:     "NG",
			Region:          "Lagos",
			ASN:             "AS64500",
			ISP:             "Bank Partner",
			UsageType:       "partner",
			ReputationScore: 80,
			ThreatFeeds:     []ThreatFeedHit{{Feed: "offline-test", Severity: "high"}},
		}},
	})

	root := t.TempDir()
	receipt, err := Import(context.Background(), bytes.NewReader(body), ImportOptions{
		RootDir:   root,
		PublicKey: pub,
		Now:       now,
	})
	if err != nil {
		t.Fatalf("import bundle: %v", err)
	}
	if receipt.BundleID != "c1-bank-core" || receipt.Sequence != 7 || receipt.Status != "active" {
		t.Fatalf("unexpected receipt: %#v", receipt)
	}
	enriched, ok, err := LookupIP(root, "203.0.113.42")
	if err != nil || !ok {
		t.Fatalf("offline lookup ok=%v err=%v", ok, err)
	}
	if enriched.CountryCode != "NG" || enriched.ASN != "AS64500" || enriched.ReputationScore != 80 {
		t.Fatalf("unexpected enrichment: %#v", enriched)
	}
	if enriched.BundleID != "c1-bank-core" || enriched.BundleVersion != "2026.05.18" || enriched.Source != "offline_bundle" {
		t.Fatalf("missing bundle provenance: %#v", enriched)
	}
}

func TestVerifyBundleRejectsInvalidSignature(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	_, wrongPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate wrong key: %v", err)
	}
	body := makeOfflineBundle(t, wrongPriv, offlineBundleFixture{BundleID: "c1", Version: "1", Sequence: 1, Now: time.Now().UTC()})
	_, err = VerifyArchive(bytes.NewReader(body), ImportOptions{PublicKey: pub, Now: time.Now().UTC()})
	if !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("err = %v, want ErrInvalidSignature", err)
	}
}

func TestImportRejectsDowngradeAndExpiredBundle(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	downgrade := makeOfflineBundle(t, priv, offlineBundleFixture{BundleID: "c1", Version: "1", Sequence: 1, Now: now})
	_, err = Import(context.Background(), bytes.NewReader(downgrade), ImportOptions{
		RootDir:         t.TempDir(),
		PublicKey:       pub,
		Now:             now,
		CurrentSequence: 2,
	})
	if !errors.Is(err, ErrDowngrade) {
		t.Fatalf("downgrade err = %v, want ErrDowngrade", err)
	}

	expired := makeOfflineBundle(t, priv, offlineBundleFixture{BundleID: "c1", Version: "expired", Sequence: 3, Now: now, BundleExpired: true})
	_, err = Import(context.Background(), bytes.NewReader(expired), ImportOptions{RootDir: t.TempDir(), PublicKey: pub, Now: now})
	if !errors.Is(err, ErrExpired) {
		t.Fatalf("expired err = %v, want ErrExpired", err)
	}
}

func TestImportExpiredContentWarnsAndMarksOfflineLookupStale(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	root := t.TempDir()
	body := makeOfflineBundle(t, priv, offlineBundleFixture{BundleID: "c1", Version: "1", Sequence: 1, Now: now, ContentExpired: true})
	receipt, err := Import(context.Background(), bytes.NewReader(body), ImportOptions{RootDir: root, PublicKey: pub, Now: now})
	if err != nil {
		t.Fatalf("import expired content bundle: %v", err)
	}
	if len(receipt.Warnings) == 0 || !receipt.Contents[0].Stale {
		t.Fatalf("expected stale content warning, receipt=%#v", receipt)
	}
	enriched, ok, err := LookupIP(root, "203.0.113.10")
	if err != nil || !ok {
		t.Fatalf("lookup stale content ok=%v err=%v", ok, err)
	}
	if !enriched.Stale {
		t.Fatalf("lookup should carry stale provenance: %#v", enriched)
	}
}

func TestListStatusAtMarksContentThatExpiredAfterImport(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	root := t.TempDir()
	body := makeOfflineBundle(t, priv, offlineBundleFixture{BundleID: "c1", Version: "1", Sequence: 1, Now: now})
	if _, err := Import(context.Background(), bytes.NewReader(body), ImportOptions{RootDir: root, PublicKey: pub, Now: now}); err != nil {
		t.Fatalf("import bundle: %v", err)
	}
	receipts, err := ListStatusAt(root, now.Add(25*time.Hour))
	if err != nil {
		t.Fatalf("list status: %v", err)
	}
	if len(receipts) != 1 || len(receipts[0].Contents) != 1 {
		t.Fatalf("receipts = %#v, want one content receipt", receipts)
	}
	if !receipts[0].Contents[0].Stale {
		t.Fatalf("expected content to be stale after expiry, got %#v", receipts[0])
	}
	if len(receipts[0].Warnings) == 0 {
		t.Fatalf("expected stale content warning, got %#v", receipts[0])
	}
}

func TestActivateRollsBackActiveContent(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	root := t.TempDir()
	first := makeOfflineBundle(t, priv, offlineBundleFixture{BundleID: "c1", Version: "1", Sequence: 1, Now: now, Records: []ipRecord{{CIDR: "203.0.113.0/24", CountryCode: "NG"}}})
	second := makeOfflineBundle(t, priv, offlineBundleFixture{BundleID: "c1", Version: "2", Sequence: 2, Now: now, Records: []ipRecord{{CIDR: "203.0.113.0/24", CountryCode: "ZA"}}})
	if _, err := Import(context.Background(), bytes.NewReader(first), ImportOptions{RootDir: root, PublicKey: pub, Now: now}); err != nil {
		t.Fatalf("import first: %v", err)
	}
	if _, err := Import(context.Background(), bytes.NewReader(second), ImportOptions{RootDir: root, PublicKey: pub, Now: now, CurrentSequence: 1}); err != nil {
		t.Fatalf("import second: %v", err)
	}
	got, ok, err := LookupIP(root, "203.0.113.10")
	if err != nil || !ok || got.CountryCode != "ZA" {
		t.Fatalf("pre-rollback lookup = %#v ok=%v err=%v, want ZA", got, ok, err)
	}
	if _, err := Activate(root, "c1", 1); err != nil {
		t.Fatalf("activate rollback: %v", err)
	}
	got, ok, err = LookupIP(root, "203.0.113.10")
	if err != nil || !ok || got.CountryCode != "NG" {
		t.Fatalf("post-rollback lookup = %#v ok=%v err=%v, want NG", got, ok, err)
	}
}

type offlineBundleFixture struct {
	BundleID       string
	Version        string
	Sequence       int64
	Now            time.Time
	Records        []ipRecord
	BundleExpired  bool
	ContentExpired bool
}

func makeOfflineBundle(t *testing.T, priv ed25519.PrivateKey, f offlineBundleFixture) []byte {
	t.Helper()
	if f.BundleID == "" {
		f.BundleID = "c1"
	}
	if f.Version == "" {
		f.Version = "1"
	}
	if f.Sequence == 0 {
		f.Sequence = 1
	}
	if f.Now.IsZero() {
		f.Now = time.Now().UTC()
	}
	if len(f.Records) == 0 {
		f.Records = []ipRecord{{CIDR: "203.0.113.0/24", Country: "Nigeria", CountryCode: "NG", ASN: "AS64500", ReputationScore: 70}}
	}
	content, err := json.Marshal(ipDataset{Records: f.Records})
	if err != nil {
		t.Fatalf("marshal content: %v", err)
	}
	sum := sha256.Sum256(content)
	bundleExpires := f.Now.Add(24 * time.Hour)
	if f.BundleExpired {
		bundleExpires = f.Now.Add(-time.Hour)
	}
	contentExpires := f.Now.Add(24 * time.Hour)
	if f.ContentExpired {
		contentExpires = f.Now.Add(-time.Hour)
	}
	manifest := Manifest{
		SchemaVersion: 1,
		BundleID:      f.BundleID,
		Version:       f.Version,
		Sequence:      f.Sequence,
		IssuedAt:      f.Now.Add(-time.Hour),
		ExpiresAt:     bundleExpires,
		Contents: []ContentFile{{
			Type:      "ip_enrichment",
			Name:      "default",
			Version:   f.Version,
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
	addTarFile(t, tw, ManifestPath, manifestBytes)
	addTarFile(t, tw, SignaturePath, []byte(base64.StdEncoding.EncodeToString(sig)))
	addTarFile(t, tw, "content/ip-enrichment.json", content)
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return buf.Bytes()
}

func addTarFile(t *testing.T, tw *tar.Writer, name string, body []byte) {
	t.Helper()
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body))}); err != nil {
		t.Fatalf("write header: %v", err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatalf("write body: %v", err)
	}
}
