package remediation

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"runtime"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func mustEngineCAKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate CA key: %v", err)
	}
	return k
}

func TestEngineExecuteSkipsVerificationWhenNotConfigured(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("engine shell path is posix-specific")
	}
	eng := NewEngine(zap.NewNop(), Options{})

	res, err := eng.Execute(context.Background(), Script{
		RuleID:        "rule.echo",
		Platform:      "linux",
		Version:       1,
		ScriptType:    "shell",
		ScriptContent: "true",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if res == nil || !res.Success {
		t.Fatalf("expected success, got: %#v", res)
	}
	if res.VerificationFailed {
		t.Fatal("expected VerificationFailed=false when verification disabled")
	}
}

func TestEngineExecuteVerifiesValidSignature(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("engine shell path is posix-specific")
	}
	priv := mustEngineCAKey(t)

	content := "true"
	sig, alg, err := Sign(priv, content, "linux", 1)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	eng := NewEngine(zap.NewNop(), Options{VerifyKey: &priv.PublicKey})
	res, err := eng.Execute(context.Background(), Script{
		RuleID:             "rule.good",
		Platform:           "linux",
		Version:            1,
		ScriptType:         "shell",
		ScriptContent:      content,
		Signature:          sig,
		SignatureAlgorithm: alg,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success, got: %#v", res)
	}
	if res.VerificationFailed {
		t.Fatal("expected VerificationFailed=false on good signature")
	}
}

func TestEngineExecuteRefusesTamperedContent(t *testing.T) {
	t.Parallel()
	priv := mustEngineCAKey(t)

	// Signer signs the original content…
	original := "echo ok"
	sig, alg, err := Sign(priv, original, "linux", 1)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	// …but the engine receives tampered content carrying the old signature.
	eng := NewEngine(zap.NewNop(), Options{VerifyKey: &priv.PublicKey})
	res, err := eng.Execute(context.Background(), Script{
		RuleID:             "rule.tampered",
		Platform:           "linux",
		Version:            1,
		ScriptType:         "shell",
		ScriptContent:      "rm -rf /",
		Signature:          sig,
		SignatureAlgorithm: alg,
	})
	if err == nil {
		t.Fatal("expected error on tampered content, got nil")
	}
	if res == nil || !res.VerificationFailed {
		t.Fatalf("expected VerificationFailed=true, got: %#v", res)
	}
	if res.Success {
		t.Fatal("tampered script reported Success=true")
	}
	if !strings.Contains(err.Error(), VerificationFailedReason) {
		t.Fatalf("expected %q in error, got: %v", VerificationFailedReason, err)
	}
	// The engine must never exec a rejected script — that's the whole point.
	if strings.TrimSpace(res.Output) != "" {
		t.Fatalf("verification-failed result leaked output: %q", res.Output)
	}
}

func TestEngineExecuteRefusesUnsignedWhenVerifyKeyPresent(t *testing.T) {
	t.Parallel()
	priv := mustEngineCAKey(t)

	eng := NewEngine(zap.NewNop(), Options{VerifyKey: &priv.PublicKey})
	res, err := eng.Execute(context.Background(), Script{
		RuleID:        "rule.unsigned",
		Platform:      "linux",
		Version:       1,
		ScriptType:    "shell",
		ScriptContent: "true",
		// No signature — must be refused.
	})
	if err == nil {
		t.Fatal("expected error on unsigned script with verify key configured")
	}
	if !res.VerificationFailed {
		t.Fatalf("expected VerificationFailed=true, got: %#v", res)
	}
	if !errors.Is(err, ErrMissingSignature) && !strings.Contains(err.Error(), "signature missing") {
		t.Fatalf("expected missing-signature error, got: %v", err)
	}
}

func TestEngineExecuteRefusesWhenRequireSignatureNoKey(t *testing.T) {
	t.Parallel()
	// No verify key but RequireSignature on — should fail loudly, never exec.
	eng := NewEngine(zap.NewNop(), Options{RequireSignature: true})
	res, err := eng.Execute(context.Background(), Script{
		RuleID:        "rule.misconfig",
		Platform:      "linux",
		Version:       1,
		ScriptType:    "shell",
		ScriptContent: "true",
	})
	if err == nil {
		t.Fatal("expected misconfiguration error, got nil")
	}
	if !res.VerificationFailed {
		t.Fatalf("expected VerificationFailed=true on misconfig, got: %#v", res)
	}
}

func TestEngineExecuteRejectsWrongCASignature(t *testing.T) {
	t.Parallel()
	originalCA := mustEngineCAKey(t)
	rogueCA := mustEngineCAKey(t)

	content := "true"
	sig, alg, err := Sign(rogueCA, content, "linux", 1)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	eng := NewEngine(zap.NewNop(), Options{VerifyKey: &originalCA.PublicKey})
	res, err := eng.Execute(context.Background(), Script{
		RuleID:             "rule.rogue",
		Platform:           "linux",
		Version:            1,
		ScriptType:         "shell",
		ScriptContent:      content,
		Signature:          sig,
		SignatureAlgorithm: alg,
	})
	if err == nil {
		t.Fatal("expected signature mismatch error, got nil")
	}
	if !res.VerificationFailed {
		t.Fatalf("expected VerificationFailed=true, got: %#v", res)
	}
}
