package storage

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/internal/privateaccess"
)

func TestNormalizePrivateAccessProviderAccountRequiresSecretRefs(t *testing.T) {
	tenantID := uuid.New()
	_, err := normalizePrivateAccessProviderAccount(UpsertPrivateAccessProviderAccountParams{
		TenantID:  tenantID,
		Provider:  privateaccess.ProviderNetBird,
		AccountID: "prod",
		Config: map[string]any{
			"api_token": "raw-secret",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "secret references") {
		t.Fatalf("err = %v, want raw secret rejection", err)
	}

	next, err := normalizePrivateAccessProviderAccount(UpsertPrivateAccessProviderAccountParams{
		TenantID:    tenantID,
		Provider:    privateaccess.ProviderNetBird,
		AccountID:   " prod ",
		EndpointURL: "https://netbird.bank.local",
		Config: map[string]any{
			"token_ref": "vault://tenant/private-access/netbird",
			"endpoints": map[string]any{
				"peers": "/api/peers",
			},
		},
	})
	if err != nil {
		t.Fatalf("normalize account: %v", err)
	}
	if next.AccountID != "prod" || next.DisplayName != "netbird:prod" || next.ImportIntervalSeconds != defaultPrivateAccessImportIntervalSeconds {
		t.Fatalf("account = %#v", next)
	}
}

func TestNormalizePrivateAccessImportRun(t *testing.T) {
	tenantID := uuid.New()
	accountID := uuid.New()
	run, err := normalizePrivateAccessImportRun(CreatePrivateAccessImportRunParams{
		TenantID:          tenantID,
		ProviderAccountID: accountID,
		Provider:          privateaccess.ProviderOpenZiti,
		Status:            "running",
		Summary:           map[string]any{"services": 2},
	})
	if err != nil {
		t.Fatalf("normalize run: %v", err)
	}
	if run.AccountID != "default" || run.Status != PrivateAccessImportStatusRunning || run.Summary["services"].(int) != 2 {
		t.Fatalf("run = %#v", run)
	}

	_, err = normalizePrivateAccessImportRun(CreatePrivateAccessImportRunParams{
		TenantID:          tenantID,
		ProviderAccountID: accountID,
		Provider:          privateaccess.ProviderOpenZiti,
		Status:            "mystery",
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("err = %v, want unsupported status", err)
	}
}
