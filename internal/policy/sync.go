package policy

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/api"
)

// Syncer fetches policies and persists them locally.
type Syncer struct {
	client       *api.Client
	log          *zap.Logger
	policyDir    string
	metadataPath string
	publicKey    ed25519.PublicKey
	fingerprint  string
}

// Options configures Syncer initialization.
type Options struct {
	PolicyDir     string
	PublicKeyPath string
	MetadataPath  string
}

// NewSyncer constructs a Syncer and loads the verification key when provided.
func NewSyncer(client *api.Client, log *zap.Logger, opts Options) (*Syncer, error) {
	s := &Syncer{
		client:       client,
		log:          log,
		policyDir:    opts.PolicyDir,
		metadataPath: opts.MetadataPath,
	}

	if s.policyDir == "" {
		s.policyDir = "."
	}
	if s.metadataPath == "" {
		s.metadataPath = filepath.Join(s.policyDir, "policies.meta.json")
	}

	if opts.PublicKeyPath != "" {
		pub, fp, err := loadEd25519PublicKey(opts.PublicKeyPath)
		if err != nil {
			return nil, err
		}
		s.publicKey = pub
		s.fingerprint = fp
	} else {
		log.Warn("policy public key path not configured; signature verification disabled")
	}

	return s, nil
}

// FetchAndPersist pulls policies for nodeID and stores them to disk.
func (s *Syncer) FetchAndPersist(ctx context.Context, nodeID string) (*PolicySet, error) {
	resp, err := s.client.Do(ctx, httpMethodGet, fmt.Sprintf("/api/v1/policies?node_id=%s", nodeID), nil)
	if err != nil {
		return nil, fmt.Errorf("fetch policies: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("policy fetch failed: status %d", resp.StatusCode)
	}

	var set PolicySet
	if err := json.NewDecoder(resp.Body).Decode(&set); err != nil {
		return nil, fmt.Errorf("decode policy set: %w", err)
	}
	set.FetchedAt = time.Now().UTC()

	if err := s.verifySignature(&set); err != nil {
		return nil, err
	}

	if err := s.persist(nodeID, &set); err != nil {
		return nil, err
	}

	return &set, nil
}

// LoadCached reads the most recent policy set from disk.
func (s *Syncer) LoadCached() ([]Rule, error) {
	path := s.cachePath()
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var set PolicySet
	if err := json.Unmarshal(b, &set); err != nil {
		return nil, err
	}
	return set.Policies, nil
}

func (s *Syncer) persist(nodeID string, set *PolicySet) error {
	if err := os.MkdirAll(s.policyDir, 0o750); err != nil {
		return fmt.Errorf("create policy dir: %w", err)
	}
	payload, err := json.MarshalIndent(set, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(s.cachePath(), payload, 0o640); err != nil {
		return err
	}

	if s.metadataPath != "" {
		meta := CacheMetadata{
			NodeID:     nodeID,
			Signature:  set.Signature,
			Version:    set.Version,
			VerifiedAt: time.Now().UTC(),
			Policies:   len(set.Policies),
			PublicKey:  s.fingerprint,
		}
		metaBytes, err := json.MarshalIndent(meta, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal policy metadata: %w", err)
		}
		if err := os.WriteFile(s.metadataPath, metaBytes, 0o640); err != nil {
			return fmt.Errorf("write policy metadata: %w", err)
		}
	}

	s.log.Info("policies cached", zap.Int("count", len(set.Policies)))
	return nil
}

func (s *Syncer) cachePath() string {
	return filepath.Join(s.policyDir, "policies.json")
}

const (
	httpMethodGet = "GET"
)

func (s *Syncer) verifySignature(set *PolicySet) error {
	if len(s.publicKey) == 0 {
		return nil
	}
	if set.Signature == "" {
		return errors.New("policy signature missing")
	}

	sig, err := base64.StdEncoding.DecodeString(set.Signature)
	if err != nil {
		return fmt.Errorf("decode policy signature: %w", err)
	}

	payload := struct {
		Policies []Rule `json:"policies"`
		Version  string `json:"version,omitempty"`
	}{
		Policies: set.Policies,
		Version:  set.Version,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal policy payload: %w", err)
	}

	if !ed25519.Verify(s.publicKey, data, sig) {
		return errors.New("policy signature verification failed")
	}

	s.log.Info("policy signature verified", zap.Int("policies", len(set.Policies)), zap.String("version", set.Version))
	return nil
}

func loadEd25519PublicKey(path string) (ed25519.PublicKey, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("read policy public key: %w", err)
	}

	keyBytes, err := decodePublicKeyBytes(data)
	if err != nil {
		return nil, "", err
	}

	pub, err := parseEd25519Key(keyBytes)
	if err != nil {
		return nil, "", err
	}

	fp := base64.StdEncoding.EncodeToString(pub)
	return pub, fp, nil
}

func decodePublicKeyBytes(data []byte) ([]byte, error) {
	block, _ := pem.Decode(data)
	if block != nil {
		return block.Bytes, nil
	}

	trimmed := bytes.TrimSpace(data)
	decoded, err := base64.StdEncoding.DecodeString(string(trimmed))
	if err == nil {
		return decoded, nil
	}

	return nil, fmt.Errorf("unsupported public key encoding: %w", err)
}

func parseEd25519Key(der []byte) (ed25519.PublicKey, error) {
	pub, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, fmt.Errorf("parse ed25519 public key: %w", err)
	}
	key, ok := pub.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("public key is not ed25519")
	}
	return key, nil
}
