package patchgovernance

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// OrganizationPolicy records a sign-off on specific saved controls. It never
// grants installation authority or substitutes for host qualification.
type OrganizationPolicy struct {
	Name                  string    `json:"name"`
	OwnerID               uuid.UUID `json:"owner_id"`
	ApproverID            uuid.UUID `json:"approver_id"`
	Responsibilities      string    `json:"responsibilities"`
	Scope                 string    `json:"scope"`
	CustomerAuthorization string    `json:"customer_authorization"`
	Exclusions            string    `json:"exclusions"`
	Reason                string    `json:"reason"`
	EffectiveAt           time.Time `json:"effective_at"`
	ExpiresAt             time.Time `json:"expires_at"`
}

func (p OrganizationPolicy) Validate(now time.Time) error {
	if p.OwnerID == uuid.Nil || p.ApproverID == uuid.Nil {
		return fmt.Errorf("select an authorized accountable owner and designated approver")
	}
	for _, f := range []struct {
		name, value string
		max         int
	}{
		{"policy name", p.Name, 160}, {"responsibilities", p.Responsibilities, 2000},
		{"scope", p.Scope, 4000}, {"customer authorization reference", p.CustomerAuthorization, 2000},
		{"exclusions and accepted risks", p.Exclusions, 4000}, {"change reason", p.Reason, 2000},
	} {
		if strings.TrimSpace(f.value) == "" || len(f.value) > f.max {
			return fmt.Errorf("%s is required and must be at most %d bytes", f.name, f.max)
		}
	}
	if p.EffectiveAt.IsZero() || !p.ExpiresAt.After(p.EffectiveAt) || !p.ExpiresAt.After(now) || p.ExpiresAt.After(now.Add(90*24*time.Hour)) {
		return fmt.Errorf("effective/expiry dates must define a valid period expiring within 90 days")
	}
	return nil
}
