package offlinebundle

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	ContentTypeRemediationPack     = "remediation_pack"
	ContentTypeAIInvestigationPack = "ai_investigation_pack"
)

type RemediationPackManifest struct {
	SchemaVersion int                        `json:"schema_version"`
	PackID        string                     `json:"pack_id"`
	Version       string                     `json:"version"`
	GeneratedAt   string                     `json:"generated_at,omitempty"`
	Description   string                     `json:"description,omitempty"`
	Actions       []RemediationPackAction    `json:"actions"`
	Artifacts     []OfflineBundleArtifactRef `json:"artifacts,omitempty"`
	Metadata      map[string]any             `json:"metadata,omitempty"`
}

type RemediationPackAction struct {
	ID                 string                     `json:"id"`
	Title              string                     `json:"title"`
	ActionType         string                     `json:"action_type"`
	TargetKinds        []string                   `json:"target_kinds"`
	Risk               string                     `json:"risk"`
	RequiresApproval   bool                       `json:"requires_approval"`
	MinApprovers       int                        `json:"min_approvers,omitempty"`
	SeparationOfDuties bool                       `json:"separation_of_duties,omitempty"`
	ScriptRefs         []OfflineBundleArtifactRef `json:"script_refs,omitempty"`
	RollbackRefs       []OfflineBundleArtifactRef `json:"rollback_refs,omitempty"`
	VerificationSteps  []string                   `json:"verification_steps"`
	SourceRefs         []string                   `json:"source_refs,omitempty"`
	Metadata           map[string]any             `json:"metadata,omitempty"`
}

type AIInvestigationPackManifest struct {
	SchemaVersion int                        `json:"schema_version"`
	PackID        string                     `json:"pack_id"`
	Version       string                     `json:"version"`
	GeneratedAt   string                     `json:"generated_at,omitempty"`
	Description   string                     `json:"description,omitempty"`
	Tools         []AIInvestigationTool      `json:"tools,omitempty"`
	Prompts       []AIInvestigationPrompt    `json:"prompts,omitempty"`
	Artifacts     []OfflineBundleArtifactRef `json:"artifacts,omitempty"`
	Metadata      map[string]any             `json:"metadata,omitempty"`
}

type AIInvestigationTool struct {
	ID               string                   `json:"id"`
	Name             string                   `json:"name"`
	Purpose          string                   `json:"purpose"`
	InputSchemaRef   OfflineBundleArtifactRef `json:"input_schema_ref,omitempty"`
	CitationRequired bool                     `json:"citation_required"`
	Guardrails       []string                 `json:"guardrails"`
	AllowedRoles     []string                 `json:"allowed_roles,omitempty"`
}

type AIInvestigationPrompt struct {
	ID           string                   `json:"id"`
	Name         string                   `json:"name"`
	Purpose      string                   `json:"purpose"`
	PromptRef    OfflineBundleArtifactRef `json:"prompt_ref"`
	AllowedTools []string                 `json:"allowed_tools,omitempty"`
	Guardrails   []string                 `json:"guardrails"`
}

type OfflineBundleArtifactRef struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	SHA256      string `json:"sha256,omitempty"`
	MediaType   string `json:"media_type,omitempty"`
	Description string `json:"description,omitempty"`
}

type ActiveRemediationPack struct {
	Manifest       RemediationPackManifest `json:"manifest"`
	ActivePath     string                  `json:"active_path"`
	ContentReceipt ContentReceipt          `json:"content_receipt"`
}

type ActiveAIInvestigationPack struct {
	Manifest       AIInvestigationPackManifest `json:"manifest"`
	ActivePath     string                      `json:"active_path"`
	ContentReceipt ContentReceipt              `json:"content_receipt"`
}

func LoadActiveRemediationPacks(rootDir string) ([]ActiveRemediationPack, error) {
	activeRoot := filepath.Join(strings.TrimSpace(rootDir), "active", cleanName(ContentTypeRemediationPack))
	matches, err := filepath.Glob(filepath.Join(activeRoot, "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)

	out := make([]ActiveRemediationPack, 0, len(matches))
	for _, activePath := range matches {
		if strings.HasSuffix(activePath, ".receipt.json") {
			continue
		}
		manifest, err := loadRemediationPackManifest(activePath)
		if err != nil {
			return nil, err
		}
		out = append(out, ActiveRemediationPack{
			Manifest:       *manifest,
			ActivePath:     activePath,
			ContentReceipt: readContentReceipt(activePath + ".receipt.json"),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Manifest.PackID != out[j].Manifest.PackID {
			return out[i].Manifest.PackID < out[j].Manifest.PackID
		}
		if out[i].Manifest.Version != out[j].Manifest.Version {
			return out[i].Manifest.Version > out[j].Manifest.Version
		}
		return out[i].ActivePath < out[j].ActivePath
	})
	return out, nil
}

func LoadActiveAIInvestigationPacks(rootDir string) ([]ActiveAIInvestigationPack, error) {
	activeRoot := filepath.Join(strings.TrimSpace(rootDir), "active", cleanName(ContentTypeAIInvestigationPack))
	matches, err := filepath.Glob(filepath.Join(activeRoot, "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)

	out := make([]ActiveAIInvestigationPack, 0, len(matches))
	for _, activePath := range matches {
		if strings.HasSuffix(activePath, ".receipt.json") {
			continue
		}
		manifest, err := loadAIInvestigationPackManifest(activePath)
		if err != nil {
			return nil, err
		}
		out = append(out, ActiveAIInvestigationPack{
			Manifest:       *manifest,
			ActivePath:     activePath,
			ContentReceipt: readContentReceipt(activePath + ".receipt.json"),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Manifest.PackID != out[j].Manifest.PackID {
			return out[i].Manifest.PackID < out[j].Manifest.PackID
		}
		if out[i].Manifest.Version != out[j].Manifest.Version {
			return out[i].Manifest.Version > out[j].Manifest.Version
		}
		return out[i].ActivePath < out[j].ActivePath
	})
	return out, nil
}

func loadRemediationPackManifest(path string) (*RemediationPackManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var manifest RemediationPackManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse remediation pack %s: %w", path, err)
	}
	if err := ValidateRemediationPackManifest(manifest); err != nil {
		return nil, fmt.Errorf("invalid remediation pack %s: %w", path, err)
	}
	return &manifest, nil
}

func loadAIInvestigationPackManifest(path string) (*AIInvestigationPackManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var manifest AIInvestigationPackManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse AI investigation pack %s: %w", path, err)
	}
	if err := ValidateAIInvestigationPackManifest(manifest); err != nil {
		return nil, fmt.Errorf("invalid AI investigation pack %s: %w", path, err)
	}
	return &manifest, nil
}

func ValidateRemediationPackManifest(manifest RemediationPackManifest) error {
	if manifest.SchemaVersion != 1 {
		return fmt.Errorf("unsupported remediation pack schema_version %d", manifest.SchemaVersion)
	}
	if strings.TrimSpace(manifest.PackID) == "" || strings.TrimSpace(manifest.Version) == "" {
		return fmt.Errorf("remediation pack pack_id and version required")
	}
	if len(manifest.Actions) == 0 {
		return fmt.Errorf("remediation pack actions required")
	}
	seenActions := map[string]struct{}{}
	for _, action := range manifest.Actions {
		id := strings.TrimSpace(action.ID)
		if id == "" || strings.TrimSpace(action.Title) == "" || strings.TrimSpace(action.ActionType) == "" {
			return fmt.Errorf("remediation pack action id, title, and action_type required")
		}
		if _, ok := seenActions[id]; ok {
			return fmt.Errorf("duplicate remediation pack action %s", id)
		}
		seenActions[id] = struct{}{}
		if len(trimmedStrings(action.TargetKinds)) == 0 {
			return fmt.Errorf("remediation pack action %s target_kinds required", id)
		}
		risk := strings.ToLower(strings.TrimSpace(action.Risk))
		if risk == "" {
			return fmt.Errorf("remediation pack action %s risk required", id)
		}
		if !validActionPackRisk(risk) {
			return fmt.Errorf("remediation pack action %s has unsupported risk %q", id, action.Risk)
		}
		if risk == "high" || risk == "critical" {
			if !action.RequiresApproval || action.MinApprovers < 2 || !action.SeparationOfDuties {
				return fmt.Errorf("remediation pack action %s high/critical risk requires dual approval and separation_of_duties", id)
			}
		}
		if len(action.ScriptRefs) == 0 {
			return fmt.Errorf("remediation pack action %s script_refs required", id)
		}
		if len(trimmedStrings(action.VerificationSteps)) == 0 {
			return fmt.Errorf("remediation pack action %s verification_steps required", id)
		}
		if err := validateArtifactRefs("remediation pack action "+id+" script_refs", action.ScriptRefs); err != nil {
			return err
		}
		if err := validateArtifactRefs("remediation pack action "+id+" rollback_refs", action.RollbackRefs); err != nil {
			return err
		}
	}
	return validateArtifactRefs("remediation pack artifacts", manifest.Artifacts)
}

func ValidateAIInvestigationPackManifest(manifest AIInvestigationPackManifest) error {
	if manifest.SchemaVersion != 1 {
		return fmt.Errorf("unsupported AI investigation pack schema_version %d", manifest.SchemaVersion)
	}
	if strings.TrimSpace(manifest.PackID) == "" || strings.TrimSpace(manifest.Version) == "" {
		return fmt.Errorf("AI investigation pack pack_id and version required")
	}
	if len(manifest.Tools) == 0 && len(manifest.Prompts) == 0 {
		return fmt.Errorf("AI investigation pack tools or prompts required")
	}
	seenTools := map[string]struct{}{}
	for _, tool := range manifest.Tools {
		id := strings.TrimSpace(tool.ID)
		if id == "" || strings.TrimSpace(tool.Name) == "" || strings.TrimSpace(tool.Purpose) == "" {
			return fmt.Errorf("AI investigation pack tool id, name, and purpose required")
		}
		if _, ok := seenTools[id]; ok {
			return fmt.Errorf("duplicate AI investigation pack tool %s", id)
		}
		seenTools[id] = struct{}{}
		if !tool.CitationRequired {
			return fmt.Errorf("AI investigation pack tool %s must require citations", id)
		}
		if len(trimmedStrings(tool.Guardrails)) == 0 {
			return fmt.Errorf("AI investigation pack tool %s guardrails required", id)
		}
		if err := validateOptionalArtifactRef("AI investigation pack tool "+id+" input_schema_ref", tool.InputSchemaRef); err != nil {
			return err
		}
	}
	seenPrompts := map[string]struct{}{}
	for _, prompt := range manifest.Prompts {
		id := strings.TrimSpace(prompt.ID)
		if id == "" || strings.TrimSpace(prompt.Name) == "" || strings.TrimSpace(prompt.Purpose) == "" {
			return fmt.Errorf("AI investigation pack prompt id, name, and purpose required")
		}
		if _, ok := seenPrompts[id]; ok {
			return fmt.Errorf("duplicate AI investigation pack prompt %s", id)
		}
		seenPrompts[id] = struct{}{}
		if err := validateRequiredArtifactRef("AI investigation pack prompt "+id+" prompt_ref", prompt.PromptRef); err != nil {
			return err
		}
		if len(trimmedStrings(prompt.Guardrails)) == 0 {
			return fmt.Errorf("AI investigation pack prompt %s guardrails required", id)
		}
		for _, toolID := range trimmedStrings(prompt.AllowedTools) {
			if _, ok := seenTools[toolID]; !ok && len(manifest.Tools) > 0 {
				return fmt.Errorf("AI investigation pack prompt %s references unknown tool %s", id, toolID)
			}
		}
	}
	return validateArtifactRefs("AI investigation pack artifacts", manifest.Artifacts)
}

func validateRemediationPackPayload(content ContentFile, data []byte) error {
	var manifest RemediationPackManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("parse remediation pack %s: %w", content.Path, err)
	}
	if err := ValidateRemediationPackManifest(manifest); err != nil {
		return fmt.Errorf("invalid remediation pack %s: %w", content.Path, err)
	}
	return nil
}

func validateAIInvestigationPackPayload(content ContentFile, data []byte) error {
	var manifest AIInvestigationPackManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("parse AI investigation pack %s: %w", content.Path, err)
	}
	if err := ValidateAIInvestigationPackManifest(manifest); err != nil {
		return fmt.Errorf("invalid AI investigation pack %s: %w", content.Path, err)
	}
	return nil
}

func validateArtifactRefs(label string, refs []OfflineBundleArtifactRef) error {
	for _, ref := range refs {
		if err := validateRequiredArtifactRef(label, ref); err != nil {
			return err
		}
	}
	return nil
}

func validateRequiredArtifactRef(label string, ref OfflineBundleArtifactRef) error {
	if strings.TrimSpace(ref.Name) == "" || strings.TrimSpace(ref.Path) == "" {
		return fmt.Errorf("%s artifact name and path required", label)
	}
	if strings.TrimSpace(ref.SHA256) != "" && !validSHA256Hex(ref.SHA256) {
		return fmt.Errorf("%s artifact %s has invalid sha256", label, ref.Name)
	}
	return nil
}

func validateOptionalArtifactRef(label string, ref OfflineBundleArtifactRef) error {
	if strings.TrimSpace(ref.Name) == "" && strings.TrimSpace(ref.Path) == "" && strings.TrimSpace(ref.SHA256) == "" {
		return nil
	}
	return validateRequiredArtifactRef(label, ref)
}

func validActionPackRisk(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "low", "medium", "high", "critical":
		return true
	default:
		return false
	}
}

func validSHA256Hex(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func trimmedStrings(values []string) []string {
	out := values[:0]
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}
