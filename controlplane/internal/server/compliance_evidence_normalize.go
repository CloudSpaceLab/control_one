package server

import (
	"fmt"
	"strings"
)

const complianceEvidenceContractVersion = "compliance.evaluator.evidence.v1"

func complianceResultMetadata(evidence map[string]any) map[string]any {
	if len(evidence) == 0 {
		return nil
	}
	normalized, redactions := normalizeComplianceEvidence(evidence, "")
	out := map[string]any{
		"evidence_contract": complianceEvidenceContractVersion,
		"evidence":          normalized,
		"evidence_redacted": redactions > 0,
	}
	if redactions > 0 {
		out["privacy_redactions"] = redactions
	}
	return out
}

func complianceEvidenceFromMetadata(metadata map[string]any) map[string]any {
	if len(metadata) == 0 {
		return nil
	}
	if evidence, ok := metadata["evidence"].(map[string]any); ok {
		return evidence
	}
	return nil
}

func normalizeComplianceEvidence(value any, path string) (any, int) {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		fieldSensitive := false
		if field, ok := typed["field"].(string); ok && sensitiveComplianceEvidencePath(field) {
			fieldSensitive = true
		}
		var redactions int
		for key, child := range typed {
			childPath := joinEvidencePath(path, key)
			if sensitiveComplianceEvidencePath(childPath) || (fieldSensitive && key == "actual") {
				out[key] = redactedEvidenceValue(child)
				redactions++
				continue
			}
			normalized, childRedactions := normalizeComplianceEvidence(child, childPath)
			out[key] = normalized
			redactions += childRedactions
		}
		return out, redactions
	case []any:
		out := make([]any, 0, len(typed))
		var redactions int
		for i, child := range typed {
			normalized, childRedactions := normalizeComplianceEvidence(child, fmt.Sprintf("%s[%d]", path, i))
			out = append(out, normalized)
			redactions += childRedactions
		}
		return out, redactions
	default:
		return typed, 0
	}
}

func sensitiveComplianceEvidencePath(path string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(path, "-", "_"))
	if normalized == "" {
		return false
	}
	if strings.Contains(normalized, "password_auth") || strings.Contains(normalized, "password_login") {
		return false
	}
	for _, term := range []string{
		"shadow",
		"passwd",
		"password_hash",
		"private_key",
		"secret",
		"token",
		"credential",
		"ntlm",
	} {
		if strings.Contains(normalized, term) {
			return true
		}
	}
	return false
}

func redactedEvidenceValue(value any) map[string]any {
	out := map[string]any{
		"redacted":         true,
		"present":          value != nil && strings.TrimSpace(fmt.Sprint(value)) != "",
		"value_type":       redactedEvidenceType(value),
		"redaction_reason": "sensitive_compliance_evidence",
	}
	if text, ok := value.(string); ok {
		trimmed := strings.TrimSpace(text)
		out["length_bucket"] = redactedEvidenceLengthBucket(len(trimmed))
		if algorithm := credentialHashAlgorithm(trimmed); algorithm != "" {
			out["credential_algorithm"] = algorithm
		}
		if state := credentialSecretState(trimmed); state != "" {
			out["credential_state"] = state
		}
	}
	return out
}

func redactedEvidenceType(value any) string {
	switch value.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case bool:
		return "bool"
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return "number"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return "unknown"
	}
}

func redactedEvidenceLengthBucket(length int) string {
	switch {
	case length <= 0:
		return "empty"
	case length <= 16:
		return "1-16"
	case length <= 64:
		return "17-64"
	case length <= 256:
		return "65-256"
	default:
		return "257+"
	}
}

func credentialHashAlgorithm(value string) string {
	trimmed := strings.TrimSpace(value)
	switch {
	case strings.HasPrefix(trimmed, "$y$"):
		return "yescrypt"
	case strings.HasPrefix(trimmed, "$6$"):
		return "sha512_crypt"
	case strings.HasPrefix(trimmed, "$5$"):
		return "sha256_crypt"
	case strings.HasPrefix(trimmed, "$2a$"), strings.HasPrefix(trimmed, "$2b$"), strings.HasPrefix(trimmed, "$2y$"):
		return "bcrypt"
	case strings.HasPrefix(trimmed, "$1$"):
		return "md5_crypt"
	case isNTLMHashShape(trimmed):
		return "ntlm"
	default:
		return ""
	}
}

func credentialSecretState(value string) string {
	value = strings.TrimSpace(value)
	switch {
	case value == "":
		return "empty"
	case strings.HasPrefix(value, "!") || strings.HasPrefix(value, "*"):
		return "locked"
	case credentialHashAlgorithm(value) != "":
		return "hashed"
	default:
		return "plaintext_like"
	}
}

func isNTLMHashShape(value string) bool {
	value = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), "ntlm:")
	if len(value) != 32 {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func joinEvidencePath(prefix, key string) string {
	if strings.TrimSpace(prefix) == "" {
		return key
	}
	return prefix + "." + key
}
