package patchgovernance

import (
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestOrganizationPolicyDatesAndIdentity(t *testing.T) {
	now := time.Now().UTC()
	p := OrganizationPolicy{Name: "Isolated sign-off", OwnerID: uuid.New(), ApproverID: uuid.New(), Responsibilities: "Operations and security", Scope: "One isolated Ubuntu host", CustomerAuthorization: "Lab-only authorization", Exclusions: "No production rollout", Reason: "Test review", EffectiveAt: now, ExpiresAt: now.Add(time.Hour)}
	require.NoError(t, p.Validate(now))
	for _, change := range []func(*OrganizationPolicy){func(p *OrganizationPolicy) { p.OwnerID = uuid.Nil }, func(p *OrganizationPolicy) { p.Scope = " " }, func(p *OrganizationPolicy) { p.ExpiresAt = now }, func(p *OrganizationPolicy) { p.ExpiresAt = now.Add(91 * 24 * time.Hour) }} {
		invalid := p
		change(&invalid)
		require.Error(t, invalid.Validate(now))
	}
}
