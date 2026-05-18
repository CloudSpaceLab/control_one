package offlinebundle

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	ManifestPath  = "manifest.json"
	SignaturePath = "manifest.sig"
)

type Manifest struct {
	SchemaVersion int           `json:"schema_version"`
	BundleID      string        `json:"bundle_id"`
	Version       string        `json:"version"`
	Sequence      int64         `json:"sequence"`
	IssuedAt      time.Time     `json:"issued_at"`
	ExpiresAt     time.Time     `json:"expires_at"`
	Contents      []ContentFile `json:"contents"`
}

type ContentFile struct {
	Type      string         `json:"type"`
	Name      string         `json:"name"`
	Version   string         `json:"version"`
	Path      string         `json:"path"`
	SHA256    string         `json:"sha256"`
	ExpiresAt *time.Time     `json:"expires_at,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type Receipt struct {
	BundleID             string            `json:"bundle_id"`
	Version              string            `json:"version"`
	Sequence             int64             `json:"sequence"`
	Status               string            `json:"status"`
	StoragePath          string            `json:"storage_path"`
	PublicKeyFingerprint string            `json:"public_key_fingerprint"`
	Signature            string            `json:"signature"`
	ManifestSHA256       string            `json:"manifest_sha256"`
	ImportedAt           time.Time         `json:"imported_at"`
	IssuedAt             time.Time         `json:"issued_at"`
	ExpiresAt            time.Time         `json:"expires_at"`
	Contents             []ContentReceipt  `json:"contents"`
	Warnings             []string          `json:"warnings,omitempty"`
	Metadata             map[string]string `json:"metadata,omitempty"`
}

type ContentReceipt struct {
	Type           string     `json:"type"`
	Name           string     `json:"name"`
	Version        string     `json:"version"`
	BundleID       string     `json:"bundle_id"`
	BundleVersion  string     `json:"bundle_version"`
	BundleSequence int64      `json:"bundle_sequence"`
	Path           string     `json:"path"`
	Active         string     `json:"active_path"`
	SHA256         string     `json:"sha256"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	Stale          bool       `json:"stale"`
}

type ImportOptions struct {
	RootDir         string
	PublicKey       ed25519.PublicKey
	Now             time.Time
	CurrentSequence int64
	MaxBytes        int64
}

type VerifiedArchive struct {
	Manifest       Manifest
	ManifestBytes  []byte
	Files          map[string][]byte
	Signature      []byte
	SignatureB64   string
	ManifestSHA256 string
	Fingerprint    string
}

var (
	ErrUnsignedBundle   = errors.New("offline bundle signature missing")
	ErrInvalidSignature = errors.New("offline bundle signature verification failed")
	ErrDowngrade        = errors.New("offline bundle sequence is older than active bundle")
	ErrExpired          = errors.New("offline bundle expired")
)

func Import(ctx context.Context, r io.Reader, opts ImportOptions) (*Receipt, error) {
	verified, err := VerifyArchive(r, opts)
	if err != nil {
		return nil, err
	}
	return InstallVerified(ctx, verified, opts)
}

func InstallVerified(ctx context.Context, verified *VerifiedArchive, opts ImportOptions) (*Receipt, error) {
	if verified == nil {
		return nil, errors.New("verified bundle required")
	}
	root := strings.TrimSpace(opts.RootDir)
	if root == "" {
		return nil, errors.New("offline bundle root dir required")
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now().UTC()
	}
	installDir := filepath.Join(root, "bundles", cleanName(verified.Manifest.BundleID), fmt.Sprintf("%020d", verified.Manifest.Sequence))
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		return nil, fmt.Errorf("create bundle dir: %w", err)
	}
	receipt := &Receipt{
		BundleID:             verified.Manifest.BundleID,
		Version:              verified.Manifest.Version,
		Sequence:             verified.Manifest.Sequence,
		Status:               "active",
		StoragePath:          installDir,
		PublicKeyFingerprint: verified.Fingerprint,
		Signature:            verified.SignatureB64,
		ManifestSHA256:       verified.ManifestSHA256,
		ImportedAt:           opts.Now.UTC(),
		IssuedAt:             verified.Manifest.IssuedAt.UTC(),
		ExpiresAt:            verified.Manifest.ExpiresAt.UTC(),
		Metadata:             map[string]string{"schema_version": fmt.Sprint(verified.Manifest.SchemaVersion)},
	}
	for _, content := range verified.Manifest.Contents {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		data := verified.Files[content.Path]
		installedPath := filepath.Join(installDir, filepath.FromSlash(content.Path))
		if err := os.MkdirAll(filepath.Dir(installedPath), 0o755); err != nil {
			return nil, fmt.Errorf("create content dir: %w", err)
		}
		if err := os.WriteFile(installedPath, data, 0o644); err != nil {
			return nil, fmt.Errorf("write content %s: %w", content.Path, err)
		}
		activePath := filepath.Join(root, "active", cleanName(content.Type), cleanName(content.Name)+filepath.Ext(content.Path))
		if err := os.MkdirAll(filepath.Dir(activePath), 0o755); err != nil {
			return nil, fmt.Errorf("create active content dir: %w", err)
		}
		if err := os.WriteFile(activePath, data, 0o644); err != nil {
			return nil, fmt.Errorf("write active content %s: %w", content.Path, err)
		}
		stale := contentExpired(content, opts.Now)
		if stale {
			receipt.Warnings = append(receipt.Warnings, fmt.Sprintf("%s/%s content is expired", content.Type, content.Name))
		}
		contentReceipt := ContentReceipt{
			Type:           content.Type,
			Name:           content.Name,
			Version:        content.Version,
			BundleID:       verified.Manifest.BundleID,
			BundleVersion:  verified.Manifest.Version,
			BundleSequence: verified.Manifest.Sequence,
			Path:           installedPath,
			Active:         activePath,
			SHA256:         strings.ToLower(content.SHA256),
			ExpiresAt:      content.ExpiresAt,
			Stale:          stale,
		}
		receipt.Contents = append(receipt.Contents, contentReceipt)
		if err := writeJSONFile(activePath+".receipt.json", contentReceipt); err != nil {
			return nil, err
		}
	}
	if err := writeJSONFile(filepath.Join(installDir, "receipt.json"), receipt); err != nil {
		return nil, err
	}
	if err := writeJSONFile(filepath.Join(root, "active", cleanName(verified.Manifest.BundleID)+".receipt.json"), receipt); err != nil {
		return nil, err
	}
	return receipt, nil
}

func VerifyArchive(r io.Reader, opts ImportOptions) (*VerifiedArchive, error) {
	if len(opts.PublicKey) != ed25519.PublicKeySize {
		return nil, errors.New("ed25519 public key required")
	}
	if opts.MaxBytes > 0 {
		r = io.LimitReader(r, opts.MaxBytes)
	}
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("open bundle gzip: %w", err)
	}
	defer func() { _ = gz.Close() }()

	files := map[string][]byte{}
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read bundle tar: %w", err)
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		name, ok := cleanArchivePath(header.Name)
		if !ok {
			return nil, fmt.Errorf("unsafe bundle path %q", header.Name)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("read bundle file %s: %w", name, err)
		}
		files[name] = data
	}
	manifestBytes := files[ManifestPath]
	if len(manifestBytes) == 0 {
		return nil, errors.New("offline bundle missing manifest.json")
	}
	signature, sigB64, err := decodeSignature(files[SignaturePath])
	if err != nil {
		return nil, err
	}
	if !ed25519.Verify(opts.PublicKey, manifestBytes, signature) {
		return nil, ErrInvalidSignature
	}
	var manifest Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	if err := validateManifest(manifest, opts); err != nil {
		return nil, err
	}
	for _, content := range manifest.Contents {
		path, ok := cleanArchivePath(content.Path)
		if !ok {
			return nil, fmt.Errorf("unsafe content path %q", content.Path)
		}
		data, ok := files[path]
		if !ok {
			return nil, fmt.Errorf("content file %q missing", content.Path)
		}
		sum := sha256.Sum256(data)
		got := hex.EncodeToString(sum[:])
		if !strings.EqualFold(got, strings.TrimSpace(content.SHA256)) {
			return nil, fmt.Errorf("content %s sha256 mismatch", content.Path)
		}
	}
	sum := sha256.Sum256(manifestBytes)
	return &VerifiedArchive{
		Manifest:       manifest,
		ManifestBytes:  manifestBytes,
		Files:          files,
		Signature:      signature,
		SignatureB64:   sigB64,
		ManifestSHA256: hex.EncodeToString(sum[:]),
		Fingerprint:    PublicKeyFingerprint(opts.PublicKey),
	}, nil
}

func LoadPublicKeyFile(path string) (ed25519.PublicKey, error) {
	data, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		return nil, err
	}
	return ParsePublicKey(data)
}

func ParsePublicKey(data []byte) (ed25519.PublicKey, error) {
	block, _ := pem.Decode(data)
	if block != nil {
		data = block.Bytes
	}
	if len(data) == ed25519.PublicKeySize {
		return ed25519.PublicKey(data), nil
	}
	pub, err := x509.ParsePKIXPublicKey(data)
	if err != nil {
		return nil, fmt.Errorf("parse ed25519 public key: %w", err)
	}
	key, ok := pub.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("public key is not ed25519")
	}
	return key, nil
}

func PublicKeyFingerprint(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:])
}

type IPEnrichment struct {
	Addr            string          `json:"addr"`
	Country         string          `json:"country,omitempty"`
	CountryCode     string          `json:"country_code,omitempty"`
	Region          string          `json:"region,omitempty"`
	ASN             string          `json:"asn,omitempty"`
	ISP             string          `json:"isp,omitempty"`
	UsageType       string          `json:"usage_type,omitempty"`
	IsTor           bool            `json:"is_tor,omitempty"`
	ReputationScore int             `json:"reputation_score,omitempty"`
	ThreatFeeds     []ThreatFeedHit `json:"threat_feeds,omitempty"`
	BundleID        string          `json:"bundle_id,omitempty"`
	BundleVersion   string          `json:"bundle_version,omitempty"`
	ContentVersion  string          `json:"content_version,omitempty"`
	Source          string          `json:"source,omitempty"`
	Stale           bool            `json:"stale,omitempty"`
}

type ThreatFeedHit struct {
	Feed     string `json:"feed"`
	Severity string `json:"severity,omitempty"`
}

type ipDataset struct {
	Records []ipRecord `json:"records"`
}

type ipRecord struct {
	IP              string          `json:"ip,omitempty"`
	CIDR            string          `json:"cidr,omitempty"`
	Country         string          `json:"country,omitempty"`
	CountryCode     string          `json:"country_code,omitempty"`
	Region          string          `json:"region,omitempty"`
	ASN             string          `json:"asn,omitempty"`
	ISP             string          `json:"isp,omitempty"`
	UsageType       string          `json:"usage_type,omitempty"`
	IsTor           bool            `json:"is_tor,omitempty"`
	ReputationScore int             `json:"reputation_score,omitempty"`
	ThreatFeeds     []ThreatFeedHit `json:"threat_feeds,omitempty"`
}

func LookupIP(rootDir, ipValue string) (*IPEnrichment, bool, error) {
	ip := net.ParseIP(strings.TrimSpace(ipValue))
	if ip == nil {
		return nil, false, errors.New("invalid ip")
	}
	activeRoot := filepath.Join(strings.TrimSpace(rootDir), "active")
	patterns := []string{
		filepath.Join(activeRoot, "ip_enrichment", "*.json"),
		filepath.Join(activeRoot, "geoip", "*.json"),
		filepath.Join(activeRoot, "threat_feed", "*.json"),
	}
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, false, err
		}
		sort.Strings(matches)
		for _, path := range matches {
			if strings.HasSuffix(path, ".receipt.json") {
				continue
			}
			enriched, ok, err := lookupIPInDataset(path, ip)
			if err != nil || !ok {
				if err != nil {
					return nil, false, err
				}
				continue
			}
			applyContentReceipt(path+".receipt.json", enriched)
			return enriched, true, nil
		}
	}
	return nil, false, nil
}

func lookupIPInDataset(path string, ip net.IP) (*IPEnrichment, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false, err
	}
	var ds ipDataset
	if err := json.Unmarshal(data, &ds); err != nil {
		return nil, false, fmt.Errorf("parse offline ip dataset %s: %w", path, err)
	}
	for _, row := range ds.Records {
		if !ipRecordMatches(row, ip) {
			continue
		}
		return &IPEnrichment{
			Addr:            ip.String(),
			Country:         row.Country,
			CountryCode:     strings.ToUpper(row.CountryCode),
			Region:          row.Region,
			ASN:             row.ASN,
			ISP:             row.ISP,
			UsageType:       row.UsageType,
			IsTor:           row.IsTor,
			ReputationScore: row.ReputationScore,
			ThreatFeeds:     row.ThreatFeeds,
			Source:          "offline_bundle",
		}, true, nil
	}
	return nil, false, nil
}

func ipRecordMatches(row ipRecord, ip net.IP) bool {
	if candidate := net.ParseIP(strings.TrimSpace(row.IP)); candidate != nil && candidate.Equal(ip) {
		return true
	}
	if _, network, err := net.ParseCIDR(strings.TrimSpace(row.CIDR)); err == nil && network.Contains(ip) {
		return true
	}
	return false
}

func applyContentReceipt(path string, enriched *IPEnrichment) {
	if enriched == nil {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var receipt ContentReceipt
	if err := json.Unmarshal(data, &receipt); err != nil {
		return
	}
	enriched.ContentVersion = receipt.Version
	enriched.Stale = receipt.Stale
	enriched.BundleID = receipt.BundleID
	enriched.BundleVersion = receipt.BundleVersion
}

func ListStatus(rootDir string) ([]Receipt, error) {
	pattern := filepath.Join(strings.TrimSpace(rootDir), "active", "*.receipt.json")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	out := make([]Receipt, 0, len(matches))
	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var receipt Receipt
		if err := json.Unmarshal(data, &receipt); err != nil {
			return nil, err
		}
		out = append(out, receipt)
	}
	return out, nil
}

func Activate(rootDir, bundleID string, sequence int64) (*Receipt, error) {
	if sequence <= 0 {
		return nil, errors.New("positive sequence required")
	}
	root := strings.TrimSpace(rootDir)
	receiptPath := filepath.Join(root, "bundles", cleanName(bundleID), fmt.Sprintf("%020d", sequence), "receipt.json")
	data, err := os.ReadFile(receiptPath)
	if err != nil {
		return nil, err
	}
	var receipt Receipt
	if err := json.Unmarshal(data, &receipt); err != nil {
		return nil, err
	}
	for i := range receipt.Contents {
		content := &receipt.Contents[i]
		data, err := os.ReadFile(content.Path)
		if err != nil {
			return nil, fmt.Errorf("read rollback content %s: %w", content.Path, err)
		}
		if err := os.MkdirAll(filepath.Dir(content.Active), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(content.Active, data, 0o644); err != nil {
			return nil, fmt.Errorf("activate content %s: %w", content.Active, err)
		}
		if err := writeJSONFile(content.Active+".receipt.json", content); err != nil {
			return nil, err
		}
	}
	receipt.Status = "active"
	if err := writeJSONFile(filepath.Join(root, "active", cleanName(receipt.BundleID)+".receipt.json"), receipt); err != nil {
		return nil, err
	}
	return &receipt, nil
}

func validateManifest(manifest Manifest, opts ImportOptions) error {
	if opts.Now.IsZero() {
		opts.Now = time.Now().UTC()
	}
	if manifest.SchemaVersion != 1 {
		return fmt.Errorf("unsupported schema_version %d", manifest.SchemaVersion)
	}
	if strings.TrimSpace(manifest.BundleID) == "" || strings.TrimSpace(manifest.Version) == "" {
		return errors.New("bundle_id and version required")
	}
	if manifest.Sequence <= 0 {
		return errors.New("sequence must be positive")
	}
	if opts.CurrentSequence > 0 && manifest.Sequence < opts.CurrentSequence {
		return ErrDowngrade
	}
	if !manifest.ExpiresAt.IsZero() && opts.Now.After(manifest.ExpiresAt) {
		return ErrExpired
	}
	if len(manifest.Contents) == 0 {
		return errors.New("manifest contents required")
	}
	seen := map[string]struct{}{}
	for _, content := range manifest.Contents {
		if strings.TrimSpace(content.Type) == "" || strings.TrimSpace(content.Name) == "" || strings.TrimSpace(content.Version) == "" {
			return errors.New("content type, name, and version required")
		}
		path, ok := cleanArchivePath(content.Path)
		if !ok {
			return fmt.Errorf("unsafe content path %q", content.Path)
		}
		if path == ManifestPath || path == SignaturePath {
			return fmt.Errorf("content path %q is reserved", content.Path)
		}
		if strings.TrimSpace(content.SHA256) == "" {
			return fmt.Errorf("content %s missing sha256", content.Path)
		}
		key := content.Type + "|" + content.Name
		if _, ok := seen[key]; ok {
			return fmt.Errorf("duplicate content target %s", key)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func contentExpired(content ContentFile, now time.Time) bool {
	if content.ExpiresAt == nil || content.ExpiresAt.IsZero() {
		return false
	}
	return now.After(content.ExpiresAt.UTC())
}

func decodeSignature(data []byte) ([]byte, string, error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil, "", ErrUnsignedBundle
	}
	raw, err := base64.StdEncoding.DecodeString(trimmed)
	if err != nil {
		raw = data
		trimmed = base64.StdEncoding.EncodeToString(raw)
	}
	if len(raw) != ed25519.SignatureSize {
		return nil, "", fmt.Errorf("signature has length %d, want %d", len(raw), ed25519.SignatureSize)
	}
	return raw, trimmed, nil
}

func writeJSONFile(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func cleanArchivePath(raw string) (string, bool) {
	raw = filepath.ToSlash(strings.TrimSpace(raw))
	raw = strings.TrimPrefix(raw, "./")
	if raw == "" || strings.HasPrefix(raw, "/") || strings.Contains(raw, "\x00") {
		return "", false
	}
	cleaned := filepath.ToSlash(filepath.Clean(raw))
	if cleaned == "." || strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		return "", false
	}
	return cleaned, true
}

func cleanName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-.")
	if out == "" {
		return "default"
	}
	return out
}
