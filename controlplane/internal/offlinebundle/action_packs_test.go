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
	"strings"
	"testing"
	"time"
)

func TestLoadActiveRemediationAndAIInvestigationPacks(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	body := makeActionPacksBundle(t, priv, actionPacksBundleFixture{Now: now})

	verified, err := VerifyArchive(bytes.NewReader(body), ImportOptions{PublicKey: priv.Public().(ed25519.PublicKey), Now: now})
	if err != nil {
		t.Fatalf("verify archive: %v", err)
	}
	root := t.TempDir()
	if _, err := InstallVerified(t.Context(), verified, ImportOptions{RootDir: root, Now: now}); err != nil {
		t.Fatalf("install verified: %v", err)
	}

	remediationPacks, err := LoadActiveRemediationPacks(root)
	if err != nil {
		t.Fatalf("load remediation packs: %v", err)
	}
	if len(remediationPacks) != 1 || remediationPacks[0].Manifest.PackID != "controlone.bank.remediation.baseline" {
		t.Fatalf("unexpected remediation packs: %+v", remediationPacks)
	}

	aiPacks, err := LoadActiveAIInvestigationPacks(root)
	if err != nil {
		t.Fatalf("load AI packs: %v", err)
	}
	if len(aiPacks) != 1 || aiPacks[0].Manifest.PackID != "controlone.bank.ai.baseline" {
		t.Fatalf("unexpected AI packs: %+v", aiPacks)
	}
}

func TestVerifyArchiveRejectsHighRiskRemediationPackWithoutDualApproval(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	body := makeActionPacksBundle(t, priv, actionPacksBundleFixture{
		Now: now,
		Remediation: RemediationPackManifest{
			SchemaVersion: 1,
			PackID:        "bad-remediation",
			Version:       "2026.05.29",
			Actions: []RemediationPackAction{{
				ID:                "disable-firewall",
				Title:             "Disable firewall",
				ActionType:        "host_firewall",
				TargetKinds:       []string{"node"},
				Risk:              "critical",
				RequiresApproval:  true,
				MinApprovers:      1,
				ScriptRefs:        []OfflineBundleArtifactRef{{Name: "apply", Path: "scripts/apply.sh", SHA256: sampleSHA256Hex("apply")}},
				VerificationSteps: []string{"verify firewall remains enabled"},
			}},
		},
	})

	_, err = VerifyArchive(bytes.NewReader(body), ImportOptions{PublicKey: priv.Public().(ed25519.PublicKey), Now: now})
	if err == nil || !strings.Contains(err.Error(), "dual approval") {
		t.Fatalf("VerifyArchive error = %v, want dual approval rejection", err)
	}
}

func TestVerifyArchiveRejectsAIInvestigationPackWithoutCitations(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	body := makeActionPacksBundle(t, priv, actionPacksBundleFixture{
		Now: now,
		AI: AIInvestigationPackManifest{
			SchemaVersion: 1,
			PackID:        "bad-ai",
			Version:       "2026.05.29",
			Tools: []AIInvestigationTool{{
				ID:         "unsafe-summary",
				Name:       "Unsafe summary",
				Purpose:    "Summarize without evidence",
				Guardrails: []string{"Do not invent facts."},
			}},
		},
	})

	_, err = VerifyArchive(bytes.NewReader(body), ImportOptions{PublicKey: priv.Public().(ed25519.PublicKey), Now: now})
	if err == nil || !strings.Contains(err.Error(), "must require citations") {
		t.Fatalf("VerifyArchive error = %v, want citation rejection", err)
	}
}

type actionPacksBundleFixture struct {
	Now         time.Time
	Remediation RemediationPackManifest
	AI          AIInvestigationPackManifest
}

func makeActionPacksBundle(t *testing.T, priv ed25519.PrivateKey, f actionPacksBundleFixture) []byte {
	t.Helper()
	if f.Now.IsZero() {
		f.Now = time.Now().UTC()
	}
	if f.Remediation.SchemaVersion == 0 {
		f.Remediation = sampleRemediationPackManifest()
	}
	if f.AI.SchemaVersion == 0 {
		f.AI = sampleAIInvestigationPackManifest()
	}
	remediationContent, err := json.Marshal(f.Remediation)
	if err != nil {
		t.Fatalf("marshal remediation pack: %v", err)
	}
	aiContent, err := json.Marshal(f.AI)
	if err != nil {
		t.Fatalf("marshal AI pack: %v", err)
	}
	remediationSum := sha256.Sum256(remediationContent)
	aiSum := sha256.Sum256(aiContent)
	bundleManifest := Manifest{
		SchemaVersion: 1,
		BundleID:      "operator-packs",
		Version:       "2026.05.29",
		Sequence:      1,
		IssuedAt:      f.Now.Add(-time.Hour),
		ExpiresAt:     f.Now.Add(90 * 24 * time.Hour),
		Contents: []ContentFile{
			{
				Type:    ContentTypeRemediationPack,
				Name:    "bank-remediation-baseline",
				Version: "2026.05.29",
				Path:    "content/remediation-pack.json",
				SHA256:  hex.EncodeToString(remediationSum[:]),
			},
			{
				Type:    ContentTypeAIInvestigationPack,
				Name:    "bank-ai-baseline",
				Version: "2026.05.29",
				Path:    "content/ai-investigation-pack.json",
				SHA256:  hex.EncodeToString(aiSum[:]),
			},
		},
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
	addTarFile(t, tw, "content/remediation-pack.json", remediationContent)
	addTarFile(t, tw, "content/ai-investigation-pack.json", aiContent)
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return buf.Bytes()
}

func sampleRemediationPackManifest() RemediationPackManifest {
	return RemediationPackManifest{
		SchemaVersion: 1,
		PackID:        "controlone.bank.remediation.baseline",
		Version:       "2026.05.29",
		Actions: []RemediationPackAction{{
			ID:                 "linux-host-firewall-close-public-ssh",
			Title:              "Close public SSH exposure",
			ActionType:         "host_firewall",
			TargetKinds:        []string{"node"},
			Risk:               "high",
			RequiresApproval:   true,
			MinApprovers:       2,
			SeparationOfDuties: true,
			ScriptRefs: []OfflineBundleArtifactRef{{
				Name:   "apply",
				Path:   "scripts/linux/close-public-ssh.sh",
				SHA256: sampleSHA256Hex("close-public-ssh"),
			}},
			RollbackRefs: []OfflineBundleArtifactRef{{
				Name:   "rollback",
				Path:   "scripts/linux/rollback-close-public-ssh.sh",
				SHA256: sampleSHA256Hex("rollback-close-public-ssh"),
			}},
			VerificationSteps: []string{
				"confirm host firewall default deny remains active",
				"confirm private-access policy still reaches approved admin group",
			},
		}},
	}
}

func sampleAIInvestigationPackManifest() AIInvestigationPackManifest {
	return AIInvestigationPackManifest{
		SchemaVersion: 1,
		PackID:        "controlone.bank.ai.baseline",
		Version:       "2026.05.29",
		Tools: []AIInvestigationTool{{
			ID:               "private-access-exposure-summary",
			Name:             "Private access exposure summary",
			Purpose:          "Summarize public exposure findings with cited rows.",
			CitationRequired: true,
			Guardrails:       []string{"Only cite imported findings and SOC case evidence."},
			InputSchemaRef: OfflineBundleArtifactRef{
				Name:   "schema",
				Path:   "schemas/private-access-exposure-summary.schema.json",
				SHA256: sampleSHA256Hex("private-access-schema"),
			},
		}},
		Prompts: []AIInvestigationPrompt{{
			ID:           "exposure-triage",
			Name:         "Exposure triage",
			Purpose:      "Create an evidence-first triage note for private-access exposure drift.",
			AllowedTools: []string{"private-access-exposure-summary"},
			PromptRef: OfflineBundleArtifactRef{
				Name:   "prompt",
				Path:   "prompts/exposure-triage.md",
				SHA256: sampleSHA256Hex("exposure-triage"),
			},
			Guardrails: []string{"Do not recommend changes without an action plan approval path."},
		}},
	}
}

func sampleSHA256Hex(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}
