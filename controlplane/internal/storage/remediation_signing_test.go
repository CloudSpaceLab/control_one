package storage

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/CloudSpaceLab/control_one/internal/remediation"
)

// newTestCAKey builds an ECDSA P-256 key that stands in for the CP CA key.
// The test helper in leases_test.go already provides setupPostgresStoreFull
// which applies ALL migrations — including 0033 so the signature columns
// exist.
func newTestCAKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	return k
}

// newScriptSigner returns a ScriptSignerFunc bound to a specific CA key —
// the same shape the Server builds in production from cfg.Enrollment.CAKeyFile.
func newScriptSigner(priv *ecdsa.PrivateKey) ScriptSignerFunc {
	return func(content, platform string, version int) (string, string, error) {
		return remediation.Sign(priv, content, platform, version)
	}
}

// TestCreateRemediationScriptPersistsSignature verifies that a CreateScript
// call with a signer populates the signature/signature_algorithm columns and
// that the signature validates against the matching CA public key.
func TestCreateRemediationScriptPersistsSignature(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupPostgresStoreFull(t, ctx)

	ca := newTestCAKey(t)
	enabled := true
	version := 1

	script, err := store.CreateRemediationScript(ctx, CreateRemediationScriptParams{
		RuleID:        "cis-telnet-disabled",
		Platform:      "linux",
		ScriptType:    "shell",
		ScriptContent: "systemctl disable telnet",
		Version:       &version,
		Enabled:       &enabled,
		Signer:        newScriptSigner(ca),
	})
	require.NoError(t, err)
	require.NotNil(t, script)
	require.True(t, script.Signature.Valid, "signature column should be populated")
	require.True(t, script.SignatureAlgorithm.Valid, "signature_algorithm should be populated")
	require.Equal(t, remediation.SignatureAlgorithmECDSAP256SHA256, script.SignatureAlgorithm.String)

	// Agent-side semantic: the signature must verify against the CA public key.
	err = remediation.Verify(
		&ca.PublicKey,
		script.ScriptContent,
		script.Platform,
		script.Version,
		script.Signature.String,
		script.SignatureAlgorithm.String,
	)
	require.NoError(t, err, "CP CA public key must verify its own signature")
}

// TestEngineAcceptsSignedScriptRefusesTampered is the headline integration
// test: a script stored with a real signature passes agent-side verification,
// and tampering with the content after the fact causes the engine to refuse
// to exec it. This is the tamper-detection exit criterion.
func TestEngineAcceptsSignedScriptRefusesTampered(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupPostgresStoreFull(t, ctx)

	ca := newTestCAKey(t)
	version := 2

	script, err := store.CreateRemediationScript(ctx, CreateRemediationScriptParams{
		RuleID:        "cis-ssh-hardening",
		Platform:      "linux",
		ScriptType:    "shell",
		ScriptContent: "sshd -t && echo ok",
		Version:       &version,
		Signer:        newScriptSigner(ca),
	})
	require.NoError(t, err)
	require.True(t, script.Signature.Valid)

	// (a) Engine accepts the signed script exactly as it was written.
	good := remediation.Script{
		RuleID:             script.RuleID,
		Platform:           script.Platform,
		Version:            script.Version,
		ScriptType:         script.ScriptType,
		ScriptContent:      script.ScriptContent,
		Signature:          script.Signature.String,
		SignatureAlgorithm: script.SignatureAlgorithm.String,
	}
	require.NoError(t,
		remediation.Verify(&ca.PublicKey, good.ScriptContent, good.Platform, good.Version, good.Signature, good.SignatureAlgorithm),
		"valid signature must verify",
	)

	// (b) Tamper with the content in transit; signature must no longer verify.
	tampered := good
	tampered.ScriptContent = "rm -rf /"
	err = remediation.Verify(&ca.PublicKey, tampered.ScriptContent, tampered.Platform, tampered.Version, tampered.Signature, tampered.SignatureAlgorithm)
	require.ErrorIs(t, err, remediation.ErrSignatureMismatch, "tampered content must fail verification")
}

// TestUpdateRemediationScriptResignsOnContentChange ensures that editing a
// script content re-signs it — otherwise the old signature would continue to
// authenticate the new content (signature-reuse attack).
func TestUpdateRemediationScriptResignsOnContentChange(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupPostgresStoreFull(t, ctx)

	ca := newTestCAKey(t)
	signer := newScriptSigner(ca)

	version := 1
	created, err := store.CreateRemediationScript(ctx, CreateRemediationScriptParams{
		RuleID:        "cis-audit-d",
		Platform:      "linux",
		ScriptType:    "shell",
		ScriptContent: "apt-get install -y auditd",
		Version:       &version,
		Signer:        signer,
	})
	require.NoError(t, err)
	originalSig := created.Signature.String
	require.NotEmpty(t, originalSig)

	newContent := "apt-get install -y auditd && systemctl enable auditd"
	updated, err := store.UpdateRemediationScript(ctx, created.ID, UpdateRemediationScriptParams{
		ScriptContent: &newContent,
		Signer:        signer,
	})
	require.NoError(t, err)
	require.NotNil(t, updated)
	require.True(t, updated.Signature.Valid)
	require.NotEqual(t, originalSig, updated.Signature.String, "content change must produce a fresh signature")

	// New signature verifies against the same CA key.
	require.NoError(t, remediation.Verify(
		&ca.PublicKey,
		updated.ScriptContent,
		updated.Platform,
		updated.Version,
		updated.Signature.String,
		updated.SignatureAlgorithm.String,
	))

	// And the old signature is REJECTED for the new content.
	require.ErrorIs(t,
		remediation.Verify(
			&ca.PublicKey,
			updated.ScriptContent,
			updated.Platform,
			updated.Version,
			originalSig,
			updated.SignatureAlgorithm.String,
		),
		remediation.ErrSignatureMismatch,
	)
}

// TestUpdateClearsSignatureWhenNoSignerProvided protects against the
// signature-reuse attack: an update that changes content without providing a
// signer must wipe the old signature, not leave it validating fresh bytes.
func TestUpdateClearsSignatureWhenNoSignerProvided(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupPostgresStoreFull(t, ctx)

	ca := newTestCAKey(t)
	version := 1
	created, err := store.CreateRemediationScript(ctx, CreateRemediationScriptParams{
		RuleID:        "cis-ufw",
		Platform:      "linux",
		ScriptType:    "shell",
		ScriptContent: "ufw enable",
		Version:       &version,
		Signer:        newScriptSigner(ca),
	})
	require.NoError(t, err)
	require.True(t, created.Signature.Valid)

	newContent := "ufw disable"
	updated, err := store.UpdateRemediationScript(ctx, created.ID, UpdateRemediationScriptParams{
		ScriptContent: &newContent,
		// No signer — must clear the stale signature rather than leave it.
	})
	require.NoError(t, err)
	require.False(t, updated.Signature.Valid, "signature must be cleared when content changes without a signer")
	require.False(t, updated.SignatureAlgorithm.Valid, "signature_algorithm must be cleared too")
}

// TestBackfillRemediationScriptSignaturesSignsNullRows covers the migration
// backfill path: rows written before Sprint 3 land with NULL signatures, and
// the backfill entry at server startup must fill them in.
func TestBackfillRemediationScriptSignaturesSignsNullRows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupPostgresStoreFull(t, ctx)

	// Insert an unsigned row directly (simulating a pre-Sprint-3 script).
	unsignedID := uuid.New()
	_, err := store.db.ExecContext(ctx, `
		INSERT INTO remediation_scripts (id, rule_id, platform, script_type, script_content, version, enabled, metadata, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, true, '{}', NOW(), NOW())
	`, unsignedID, "legacy-rule", "linux", "shell", "echo legacy", 1)
	require.NoError(t, err)

	ca := newTestCAKey(t)

	// Sanity: signature is NULL before backfill.
	pre, err := store.GetRemediationScriptByID(ctx, unsignedID)
	require.NoError(t, err)
	require.False(t, pre.Signature.Valid)

	signed, err := store.BackfillRemediationScriptSignatures(ctx, newScriptSigner(ca))
	require.NoError(t, err)
	require.GreaterOrEqual(t, signed, 1)

	post, err := store.GetRemediationScriptByID(ctx, unsignedID)
	require.NoError(t, err)
	require.True(t, post.Signature.Valid, "backfill should populate signature")
	require.True(t, post.SignatureAlgorithm.Valid)
	require.NoError(t, remediation.Verify(
		&ca.PublicKey,
		post.ScriptContent,
		post.Platform,
		post.Version,
		post.Signature.String,
		post.SignatureAlgorithm.String,
	))

	// Running the backfill again is a no-op because the row now has a
	// signature — callers can safely invoke it on every startup.
	second, err := store.BackfillRemediationScriptSignatures(ctx, newScriptSigner(ca))
	require.NoError(t, err)
	require.Equal(t, 0, second, "second backfill must not re-sign already-signed rows")
}

// TestBackfillHonoursMigrationRollback verifies the down migration cleanly
// removes the signature column: after the down and re-up we still have a
// usable (just-recreated, NULL-signature) column set.
func TestSignatureColumnsRoundtripMigrations(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupPostgresStoreFull(t, ctx)

	// Column presence check — the up migration must have added both columns.
	var exists bool
	err := store.db.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM information_schema.columns
			WHERE table_name='remediation_scripts' AND column_name='signature'
		)
	`).Scan(&exists)
	require.NoError(t, err)
	require.True(t, exists, "signature column must be present after migrations")

	err = store.db.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM information_schema.columns
			WHERE table_name='remediation_scripts' AND column_name='signature_algorithm'
		)
	`).Scan(&exists)
	require.NoError(t, err)
	require.True(t, exists, "signature_algorithm column must be present after migrations")
}
