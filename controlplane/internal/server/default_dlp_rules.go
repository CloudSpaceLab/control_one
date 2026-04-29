package server

import "github.com/CloudSpaceLab/control_one/controlplane/internal/storage"

// DefaultDLPRules contains built-in PII/secret detection patterns that are
// seeded per-tenant on first DLP scan or via the seed-rules endpoint.
var DefaultDLPRules = []storage.DataClassificationRule{
	{Name: "Email address", PIIType: "email", Regex: `\b[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}\b`, Severity: "high"},
	{Name: "US SSN", PIIType: "ssn", Regex: `\b\d{3}-\d{2}-\d{4}\b`, Severity: "critical"},
	{Name: "Credit card (Luhn prefix)", PIIType: "credit_card", Regex: `\b(?:4\d{12}(?:\d{3})?|5[1-5]\d{14}|3[47]\d{13}|6(?:011|5\d\d)\d{12})\b`, Severity: "critical"},
	{Name: "US phone", PIIType: "phone", Regex: `\b\(?\d{3}\)?[-.\s]?\d{3}[-.\s]?\d{4}\b`, Severity: "medium"},
	{Name: "IPv4 address", PIIType: "ip_address", Regex: `\b(?:\d{1,3}\.){3}\d{1,3}\b`, Severity: "low"},
	{Name: "JWT token", PIIType: "jwt", Regex: `eyJ[A-Za-z0-9\-_]+\.eyJ[A-Za-z0-9\-_]+\.[A-Za-z0-9\-_.+/]*`, Severity: "critical"},
	{Name: "AWS access key", PIIType: "aws_key", Regex: `\bAKIA[0-9A-Z]{16}\b`, Severity: "critical"},
	{Name: "GitHub PAT", PIIType: "github_pat", Regex: `ghp_[A-Za-z0-9]{36}`, Severity: "critical"},
	{Name: "Slack token", PIIType: "slack_token", Regex: `xox[baprs]-[0-9A-Za-z\-]+`, Severity: "critical"},
	{Name: "PEM private key block", PIIType: "pem_key", Regex: `-----BEGIN (?:RSA |EC )?PRIVATE KEY-----`, Severity: "critical"},
	{Name: "IBAN", PIIType: "iban", Regex: `\b[A-Z]{2}\d{2}[A-Z0-9]{4}\d{7}(?:[A-Z0-9]?){0,16}\b`, Severity: "high"},
	{Name: "MAC address", PIIType: "mac_address", Regex: `\b([0-9A-Fa-f]{2}[:\-]){5}[0-9A-Fa-f]{2}\b`, Severity: "low"},
}
