package policy

import "time"

// Rule models a compliance rule fetched from the control plane.
type Rule struct {
	ID          string `json:"id"`
	Version     string `json:"version"`
	Severity    string `json:"severity"`
	Check       string `json:"check"`
	Remediation string `json:"remediation"`
}

// PolicySet wraps a list of rules with signature metadata.
type PolicySet struct {
	Policies  []Rule    `json:"policies"`
	Signature string    `json:"signature"`
	Version   string    `json:"version,omitempty"`
	FetchedAt time.Time `json:"fetched_at"`
}

// CacheMetadata captures persisted verification context for the cached bundle.
type CacheMetadata struct {
	NodeID      string    `json:"node_id"`
	Signature   string    `json:"signature"`
	Version     string    `json:"version,omitempty"`
	VerifiedAt  time.Time `json:"verified_at"`
	Policies    int       `json:"policies"`
	PublicKey   string    `json:"public_key"`
}
