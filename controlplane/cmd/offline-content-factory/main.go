package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/offlinebundle"
)

type factoryOptions struct {
	manifestPath   string
	contentRoot    string
	privateKeyPath string
	outputPath     string
	printPublicKey bool
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("offline-content-factory", flag.ContinueOnError)
	fs.SetOutput(stderr)
	opts := factoryOptions{}
	fs.StringVar(&opts.manifestPath, "manifest", "", "path to unsigned offline content manifest.json")
	fs.StringVar(&opts.contentRoot, "content-root", ".", "directory containing files referenced by manifest contents")
	fs.StringVar(&opts.privateKeyPath, "private-key", "", "Ed25519 private key PEM, raw key, or raw seed")
	fs.StringVar(&opts.outputPath, "out", "", "output .tar.gz path")
	fs.BoolVar(&opts.printPublicKey, "print-public-key", false, "print derived Ed25519 public key PEM")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := buildSignedOfflineContentBundle(opts, stdout); err != nil {
		return err
	}
	return nil
}

func buildSignedOfflineContentBundle(opts factoryOptions, stdout io.Writer) error {
	if strings.TrimSpace(opts.manifestPath) == "" {
		return errors.New("--manifest is required")
	}
	if strings.TrimSpace(opts.privateKeyPath) == "" {
		return errors.New("--private-key is required")
	}
	if strings.TrimSpace(opts.outputPath) == "" {
		return errors.New("--out is required")
	}
	if strings.TrimSpace(opts.contentRoot) == "" {
		opts.contentRoot = "."
	}

	priv, err := loadEd25519PrivateKeyFile(opts.privateKeyPath)
	if err != nil {
		return err
	}
	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok || len(pub) != ed25519.PublicKeySize {
		return errors.New("private key did not produce an Ed25519 public key")
	}

	manifestBytes, err := os.ReadFile(opts.manifestPath)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}
	var manifest offlinebundle.Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return fmt.Errorf("parse manifest: %w", err)
	}

	files := make(map[string][]byte, len(manifest.Contents))
	for i, content := range manifest.Contents {
		cleanPath, ok := cleanFactoryArchivePath(content.Path)
		if !ok {
			return fmt.Errorf("unsafe content path %q", content.Path)
		}
		if cleanPath == offlinebundle.ManifestPath || cleanPath == offlinebundle.SignaturePath {
			return fmt.Errorf("content path %q is reserved", content.Path)
		}
		data, err := os.ReadFile(filepath.Join(opts.contentRoot, filepath.FromSlash(cleanPath)))
		if err != nil {
			return fmt.Errorf("read content %s: %w", content.Path, err)
		}
		sum := sha256Bytes(data)
		manifest.Contents[i].Path = cleanPath
		manifest.Contents[i].SHA256 = hex.EncodeToString(sum)
		files[cleanPath] = data
	}

	signedManifest, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal signed manifest: %w", err)
	}
	signature := ed25519.Sign(priv, signedManifest)

	if err := os.MkdirAll(filepath.Dir(opts.outputPath), 0o755); err != nil && filepath.Dir(opts.outputPath) != "." {
		return fmt.Errorf("create output dir: %w", err)
	}
	if err := writeBundleArchive(opts.outputPath, signedManifest, signature, manifest.Contents, files); err != nil {
		return err
	}

	verifyFile, err := os.Open(opts.outputPath)
	if err != nil {
		return fmt.Errorf("reopen output for verification: %w", err)
	}
	defer func() { _ = verifyFile.Close() }()
	if _, err := offlinebundle.VerifyArchive(verifyFile, offlinebundle.ImportOptions{PublicKey: pub, Now: time.Now().UTC()}); err != nil {
		return fmt.Errorf("self-verify signed bundle: %w", err)
	}

	if opts.printPublicKey {
		pemBytes, err := encodeEd25519PublicKeyPEM(pub)
		if err != nil {
			return err
		}
		fmt.Fprint(stdout, string(pemBytes))
	}
	fmt.Fprintf(stdout, "wrote %s\n", opts.outputPath)
	fmt.Fprintf(stdout, "public_key_fingerprint %s\n", offlinebundle.PublicKeyFingerprint(pub))
	return nil
}

func writeBundleArchive(outputPath string, manifest []byte, signature []byte, contents []offlinebundle.ContentFile, files map[string][]byte) error {
	out, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("create output: %w", err)
	}
	defer func() { _ = out.Close() }()

	gz := gzip.NewWriter(out)
	defer func() { _ = gz.Close() }()
	tw := tar.NewWriter(gz)
	defer func() { _ = tw.Close() }()

	if err := addFactoryTarFile(tw, offlinebundle.ManifestPath, manifest); err != nil {
		return err
	}
	if err := addFactoryTarFile(tw, offlinebundle.SignaturePath, []byte(base64.StdEncoding.EncodeToString(signature))); err != nil {
		return err
	}
	for _, content := range contents {
		data, ok := files[content.Path]
		if !ok {
			return fmt.Errorf("missing staged content %s", content.Path)
		}
		if err := addFactoryTarFile(tw, content.Path, data); err != nil {
			return err
		}
	}
	return nil
}

func addFactoryTarFile(tw *tar.Writer, name string, body []byte) error {
	hdr := &tar.Header{
		Name: name,
		Mode: 0o644,
		Size: int64(len(body)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("write tar header %s: %w", name, err)
	}
	if _, err := tw.Write(body); err != nil {
		return fmt.Errorf("write tar file %s: %w", name, err)
	}
	return nil
}

func loadEd25519PrivateKeyFile(filePath string) (ed25519.PrivateKey, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read private key: %w", err)
	}
	if key, err := parseEd25519PrivateKey(data); err == nil {
		return key, nil
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
	if err == nil {
		if key, err := parseEd25519PrivateKey(decoded); err == nil {
			return key, nil
		}
	}
	return nil, errors.New("unable to parse Ed25519 private key")
}

func parseEd25519PrivateKey(data []byte) (ed25519.PrivateKey, error) {
	data = bytes.TrimSpace(data)
	if block, _ := pem.Decode(data); block != nil {
		data = block.Bytes
	}
	if key, err := x509.ParsePKCS8PrivateKey(data); err == nil {
		if priv, ok := key.(ed25519.PrivateKey); ok {
			return priv, nil
		}
		return nil, errors.New("private key is not Ed25519")
	}
	switch len(data) {
	case ed25519.PrivateKeySize:
		out := make(ed25519.PrivateKey, ed25519.PrivateKeySize)
		copy(out, data)
		return out, nil
	case ed25519.SeedSize:
		return ed25519.NewKeyFromSeed(data), nil
	default:
		return nil, fmt.Errorf("invalid Ed25519 key length %d", len(data))
	}
}

func encodeEd25519PublicKeyPEM(pub ed25519.PublicKey) ([]byte, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, fmt.Errorf("marshal public key: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), nil
}

func cleanFactoryArchivePath(value string) (string, bool) {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	if value == "" || strings.HasPrefix(value, "/") {
		return "", false
	}
	cleaned := path.Clean(value)
	if cleaned == "." || strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		return "", false
	}
	return cleaned, true
}

func sha256Bytes(data []byte) []byte {
	sum := sha256.Sum256(data)
	return sum[:]
}
