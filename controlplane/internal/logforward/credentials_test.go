package logforward

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

type fakeVaultSecrets struct {
	data map[string]interface{}
	path string
	err  error
}

func (f *fakeVaultSecrets) ReadSecret(_ context.Context, path string) (map[string]interface{}, error) {
	f.path = path
	if f.err != nil {
		return nil, f.err
	}
	return f.data, nil
}

func TestEnvCredentialResolver(t *testing.T) {
	t.Setenv("C1_TEST_SECRET", "secret-value")
	resolver := EnvCredentialResolver{}
	value, err := resolver.ResolveCredential(context.Background(), uuid.New(), "env:C1_TEST_SECRET")
	if err != nil {
		t.Fatal(err)
	}
	if value != "secret-value" {
		t.Fatalf("value = %q", value)
	}
	if _, err := resolver.ResolveCredential(context.Background(), uuid.New(), "vault://secret/data/app#token"); !errors.Is(err, ErrUnsupportedCredentialRef) {
		t.Fatalf("err = %v, want unsupported", err)
	}
}

func TestVaultCredentialResolver(t *testing.T) {
	client := &fakeVaultSecrets{data: map[string]interface{}{"token": "vault-token", "api_key": "api-key"}}
	resolver := VaultCredentialResolver{Client: client}
	value, err := resolver.ResolveCredential(context.Background(), uuid.New(), "vault://secret/data/control-one/splunk#token")
	if err != nil {
		t.Fatal(err)
	}
	if client.path != "secret/data/control-one/splunk" || value != "vault-token" {
		t.Fatalf("path/value = %q/%q", client.path, value)
	}

	value, err = resolver.ResolveCredential(context.Background(), uuid.New(), "vault:///secret/data/control-one/elastic?key=api_key")
	if err != nil {
		t.Fatal(err)
	}
	if value != "api-key" {
		t.Fatalf("query key value = %q", value)
	}
}

func TestMultiCredentialResolverRoutesByScheme(t *testing.T) {
	t.Setenv("C1_CHAIN_SECRET", "env-secret")
	vaultClient := &fakeVaultSecrets{data: map[string]interface{}{"value": "vault-secret"}}
	resolver := MultiCredentialResolver{
		EnvCredentialResolver{},
		VaultCredentialResolver{Client: vaultClient},
	}
	envValue, err := resolver.ResolveCredential(context.Background(), uuid.New(), "env:C1_CHAIN_SECRET")
	if err != nil {
		t.Fatal(err)
	}
	if envValue != "env-secret" {
		t.Fatalf("env value = %q", envValue)
	}
	vaultValue, err := resolver.ResolveCredential(context.Background(), uuid.New(), "vault://secret/data/control-one/sentinel")
	if err != nil {
		t.Fatal(err)
	}
	if vaultValue != "vault-secret" {
		t.Fatalf("vault value = %q", vaultValue)
	}
}
