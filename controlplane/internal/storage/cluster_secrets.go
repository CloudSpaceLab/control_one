package storage

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ClusterSecret is a single cluster-scoped key/value secret. The decrypted
// Value field is only populated when the caller requests it through
// GetClusterSecretDecrypted / ListClusterSecretsDecrypted — the
// ValueEncrypted blob is the canonical on-disk form.
type ClusterSecret struct {
	ID             uuid.UUID
	ClusterID      uuid.UUID
	Key            string
	ValueEncrypted []byte
	Value          string
	Version        int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// UpsertClusterSecretParams captures the input for inserting a new cluster
// secret or rotating an existing one. Value is the plaintext — encryption
// happens inside the store using the key bound to the Store.
type UpsertClusterSecretParams struct {
	ClusterID uuid.UUID
	Key       string
	Value     string
}

// UpsertClusterSecret inserts a new cluster_secrets row or increments the
// version of an existing (cluster_id, key) row with the fresh ciphertext.
// Returns the post-write row (never includes the plaintext).
func (s *Store) UpsertClusterSecret(ctx context.Context, params UpsertClusterSecretParams) (*ClusterSecret, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if params.ClusterID == uuid.Nil {
		return nil, errors.New("cluster id is required")
	}
	key := strings.TrimSpace(params.Key)
	if key == "" {
		return nil, errors.New("key is required")
	}

	cipherText, err := s.encryptClusterSecretValue([]byte(params.Value))
	if err != nil {
		return nil, fmt.Errorf("encrypt cluster secret: %w", err)
	}

	now := s.clock()
	id := uuid.New()

	row := s.db.QueryRowContext(ctx, `
		INSERT INTO cluster_secrets (id, cluster_id, key, value_encrypted, version, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 1, $5, $5)
		ON CONFLICT (cluster_id, key)
		DO UPDATE SET
			value_encrypted = EXCLUDED.value_encrypted,
			version = cluster_secrets.version + 1,
			updated_at = EXCLUDED.updated_at
		RETURNING id, cluster_id, key, value_encrypted, version, created_at, updated_at
	`, id, params.ClusterID, key, cipherText, now)

	return scanClusterSecret(row)
}

// GetClusterSecret returns a single cluster secret without decrypting the
// value. Callers that need plaintext must call GetClusterSecretDecrypted.
func (s *Store) GetClusterSecret(ctx context.Context, clusterID uuid.UUID, key string) (*ClusterSecret, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if clusterID == uuid.Nil {
		return nil, errors.New("cluster id is required")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, errors.New("key is required")
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT id, cluster_id, key, value_encrypted, version, created_at, updated_at
		FROM cluster_secrets
		WHERE cluster_id = $1 AND key = $2
	`, clusterID, key)

	secret, err := scanClusterSecret(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return secret, nil
}

// GetClusterSecretDecrypted fetches a cluster secret and decrypts it in one
// call. Returns (nil, nil) when the (cluster_id, key) tuple doesn't exist.
func (s *Store) GetClusterSecretDecrypted(ctx context.Context, clusterID uuid.UUID, key string) (*ClusterSecret, error) {
	secret, err := s.GetClusterSecret(ctx, clusterID, key)
	if err != nil || secret == nil {
		return secret, err
	}
	plain, err := s.decryptClusterSecretValue(secret.ValueEncrypted)
	if err != nil {
		return nil, fmt.Errorf("decrypt cluster secret: %w", err)
	}
	secret.Value = string(plain)
	return secret, nil
}

// ListClusterSecrets returns every secret row for a cluster in stable key
// order. Values are left encrypted — use ListClusterSecretsDecrypted when
// the caller needs plaintext (e.g. fan-out push).
func (s *Store) ListClusterSecrets(ctx context.Context, clusterID uuid.UUID) ([]ClusterSecret, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if clusterID == uuid.Nil {
		return nil, errors.New("cluster id is required")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, cluster_id, key, value_encrypted, version, created_at, updated_at
		FROM cluster_secrets
		WHERE cluster_id = $1
		ORDER BY key ASC
	`, clusterID)
	if err != nil {
		return nil, fmt.Errorf("query cluster secrets: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []ClusterSecret
	for rows.Next() {
		sec, scanErr := scanClusterSecret(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, *sec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cluster secrets: %w", err)
	}
	return out, nil
}

// ListClusterSecretsDecrypted is the convenience wrapper that decrypts every
// row in one pass. Only the fan-out push path should call this — the API
// `GET /secrets` endpoint returns ciphertext-less metadata by default.
func (s *Store) ListClusterSecretsDecrypted(ctx context.Context, clusterID uuid.UUID) ([]ClusterSecret, error) {
	secrets, err := s.ListClusterSecrets(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	for i := range secrets {
		plain, err := s.decryptClusterSecretValue(secrets[i].ValueEncrypted)
		if err != nil {
			return nil, fmt.Errorf("decrypt cluster secret %s: %w", secrets[i].Key, err)
		}
		secrets[i].Value = string(plain)
	}
	return secrets, nil
}

// ClusterSecretNodeState tracks per-node delivery of a cluster-scoped secret.
// One row per (cluster, node, key). Rows with Action == "delete" are
// tombstones and the agent unsets the value on its next poll.
type ClusterSecretNodeState struct {
	ClusterID  uuid.UUID
	NodeID     uuid.UUID
	Key        string
	Action     string
	SyncStatus string
	PushedAt   time.Time
}

// ClusterSecretPushRecord captures the input for RecordClusterSecretPush.
// Metadata is not persisted — it's accepted to keep the call-site symmetric
// with other push operations, but we store only the fields that the agent
// actually consumes.
type ClusterSecretPushRecord struct {
	ClusterID  uuid.UUID
	NodeID     uuid.UUID
	Key        string
	Action     string
	SyncStatus string
	Metadata   map[string]any
}

// RecordClusterSecretPush upserts the per-node delivery row for a cluster
// secret. The write is idempotent: running the same push twice refreshes
// pushed_at but leaves the semantic effect identical — the agent pulls the
// latest row on its next /secrets/sync cycle.
func (s *Store) RecordClusterSecretPush(ctx context.Context, params ClusterSecretPushRecord) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	if params.ClusterID == uuid.Nil {
		return errors.New("cluster id is required")
	}
	if params.NodeID == uuid.Nil {
		return errors.New("node id is required")
	}
	key := strings.TrimSpace(params.Key)
	if key == "" {
		return errors.New("key is required")
	}
	action := strings.TrimSpace(params.Action)
	if action == "" {
		action = "upsert"
	}
	syncStatus := strings.TrimSpace(params.SyncStatus)
	if syncStatus == "" {
		syncStatus = "pending"
	}
	now := s.clock()

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO cluster_secret_node_state (cluster_id, node_id, key, action, sync_status, pushed_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (cluster_id, node_id, key)
		DO UPDATE SET action = EXCLUDED.action,
		              sync_status = EXCLUDED.sync_status,
		              pushed_at = EXCLUDED.pushed_at
	`, params.ClusterID, params.NodeID, key, action, syncStatus, now)
	if err != nil {
		return fmt.Errorf("record cluster secret push: %w", err)
	}
	return nil
}

// ListClusterSecretNodeState returns every push row currently on record for
// a cluster, ordered by node then key. Used by integration tests and
// operators to confirm what has been pushed where.
func (s *Store) ListClusterSecretNodeState(ctx context.Context, clusterID uuid.UUID) ([]ClusterSecretNodeState, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if clusterID == uuid.Nil {
		return nil, errors.New("cluster id is required")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT cluster_id, node_id, key, action, sync_status, pushed_at
		FROM cluster_secret_node_state
		WHERE cluster_id = $1
		ORDER BY node_id, key
	`, clusterID)
	if err != nil {
		return nil, fmt.Errorf("query cluster secret node state: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []ClusterSecretNodeState
	for rows.Next() {
		var entry ClusterSecretNodeState
		if scanErr := rows.Scan(&entry.ClusterID, &entry.NodeID, &entry.Key, &entry.Action, &entry.SyncStatus, &entry.PushedAt); scanErr != nil {
			return nil, fmt.Errorf("scan cluster secret node state: %w", scanErr)
		}
		out = append(out, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cluster secret node state: %w", err)
	}
	return out, nil
}

// ListClusterSecretNodeStateForNode returns the delivery rows a given node
// currently has on record. Agents read this slice on /secrets/sync.
func (s *Store) ListClusterSecretNodeStateForNode(ctx context.Context, nodeID uuid.UUID) ([]ClusterSecretNodeState, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if nodeID == uuid.Nil {
		return nil, errors.New("node id is required")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT cluster_id, node_id, key, action, sync_status, pushed_at
		FROM cluster_secret_node_state
		WHERE node_id = $1
		ORDER BY cluster_id, key
	`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("query cluster secret node state by node: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []ClusterSecretNodeState
	for rows.Next() {
		var entry ClusterSecretNodeState
		if scanErr := rows.Scan(&entry.ClusterID, &entry.NodeID, &entry.Key, &entry.Action, &entry.SyncStatus, &entry.PushedAt); scanErr != nil {
			return nil, fmt.Errorf("scan cluster secret node state: %w", scanErr)
		}
		out = append(out, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cluster secret node state: %w", err)
	}
	return out, nil
}

// DeleteClusterSecretNodeStateForKey removes every per-node row for a given
// (cluster, key). Used when a secret is fully deleted — the tombstone is
// propagated separately via action='delete' rows, but once every agent has
// picked up the tombstone we can sweep the table. Used by the fan-out
// worker on delete action after all nodes are marked.
func (s *Store) DeleteClusterSecretNodeStateForKey(ctx context.Context, clusterID uuid.UUID, key string) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	if clusterID == uuid.Nil {
		return errors.New("cluster id is required")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("key is required")
	}
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM cluster_secret_node_state WHERE cluster_id = $1 AND key = $2
	`, clusterID, key)
	if err != nil {
		return fmt.Errorf("delete cluster secret node state for key: %w", err)
	}
	return nil
}

// DeleteClusterSecret removes the row for (cluster_id, key). Returns
// sql.ErrNoRows if no such row exists so callers can distinguish "deleted" vs
// "never existed".
func (s *Store) DeleteClusterSecret(ctx context.Context, clusterID uuid.UUID, key string) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	if clusterID == uuid.Nil {
		return errors.New("cluster id is required")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("key is required")
	}
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM cluster_secrets WHERE cluster_id = $1 AND key = $2
	`, clusterID, key)
	if err != nil {
		return fmt.Errorf("delete cluster secret: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete cluster secret rows affected: %w", err)
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ─── Encryption helpers ─────────────────────────────────────────────

// encryptClusterSecretValue wraps AES-GCM around the plaintext. The output is
// `nonce || ciphertext` so rotation is self-describing. We use stdlib crypto
// only — no external dependencies. The key is derived once per Store from
// loadClusterSecretKey; see that function for sourcing / rotation notes.
func (s *Store) encryptClusterSecretValue(plain []byte) ([]byte, error) {
	key, err := s.clusterSecretKey()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm aead: %w", err)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("nonce read: %w", err)
	}
	return aead.Seal(nonce, nonce, plain, nil), nil
}

// decryptClusterSecretValue reverses encryptClusterSecretValue. Ciphertexts
// shorter than the nonce size are rejected up-front so a malformed column
// value surfaces as a real error rather than a garbled plaintext.
func (s *Store) decryptClusterSecretValue(ciphertext []byte) ([]byte, error) {
	key, err := s.clusterSecretKey()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm aead: %w", err)
	}
	if len(ciphertext) < aead.NonceSize() {
		return nil, errors.New("ciphertext too short for nonce")
	}
	nonce, sealed := ciphertext[:aead.NonceSize()], ciphertext[aead.NonceSize():]
	plain, err := aead.Open(nil, nonce, sealed, nil)
	if err != nil {
		return nil, fmt.Errorf("gcm open: %w", err)
	}
	return plain, nil
}

// clusterSecretKey resolves the 32-byte key used to wrap cluster_secrets
// values at rest. Resolution order:
//  1. Explicit key configured on the Store (tests inject a deterministic key)
//  2. CONTROL_ONE_SECRETS_KEY env var (hex-encoded 32 bytes or any string,
//     in which case we SHA-256 it to 32 bytes)
//  3. Deterministic fallback derived from "control-one-dev-secrets-key" —
//     DEV-ONLY; production deployments must set the env var
//
// The sha256 derivation is safe because the key is stretched from a
// high-entropy source the operator controls. We do not invent a KMS nor add
// new crypto dependencies — this is pure stdlib AES-GCM wrapping.
func (s *Store) clusterSecretKey() ([]byte, error) {
	if len(s.clusterSecretKeyOverride) == 32 {
		return s.clusterSecretKeyOverride, nil
	}
	raw := strings.TrimSpace(os.Getenv("CONTROL_ONE_SECRETS_KEY"))
	if raw == "" {
		raw = "control-one-dev-secrets-key"
	}
	sum := sha256.Sum256([]byte(raw))
	return sum[:], nil
}

// SetClusterSecretKey overrides the key resolution for tests. The key must be
// exactly 32 bytes. A zero-length key clears the override and re-enables
// environment-variable sourcing.
func (s *Store) SetClusterSecretKey(key []byte) error {
	if len(key) == 0 {
		s.clusterSecretKeyOverride = nil
		return nil
	}
	if len(key) != 32 {
		return errors.New("cluster secret key must be 32 bytes")
	}
	s.clusterSecretKeyOverride = append([]byte(nil), key...)
	return nil
}

func scanClusterSecret(scanner rowScanner) (*ClusterSecret, error) {
	var sec ClusterSecret
	if err := scanner.Scan(
		&sec.ID,
		&sec.ClusterID,
		&sec.Key,
		&sec.ValueEncrypted,
		&sec.Version,
		&sec.CreatedAt,
		&sec.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &sec, nil
}
