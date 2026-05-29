package logforward

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/google/uuid"
)

var ErrUnsupportedCredentialRef = errors.New("unsupported credential ref")

type CredentialResolver interface {
	ResolveCredential(context.Context, uuid.UUID, string) (string, error)
}

type CredentialResolverFunc func(context.Context, uuid.UUID, string) (string, error)

func (f CredentialResolverFunc) ResolveCredential(ctx context.Context, tenantID uuid.UUID, ref string) (string, error) {
	return f(ctx, tenantID, ref)
}

type MultiCredentialResolver []CredentialResolver

func (m MultiCredentialResolver) ResolveCredential(ctx context.Context, tenantID uuid.UUID, ref string) (string, error) {
	var last error
	for _, resolver := range m {
		if resolver == nil {
			continue
		}
		value, err := resolver.ResolveCredential(ctx, tenantID, ref)
		if err == nil {
			return value, nil
		}
		last = err
		if !errors.Is(err, ErrUnsupportedCredentialRef) {
			return "", err
		}
	}
	if last != nil {
		return "", last
	}
	return "", fmt.Errorf("%w %q", ErrUnsupportedCredentialRef, strings.TrimSpace(ref))
}

type EnvCredentialResolver struct{}

func (EnvCredentialResolver) ResolveCredential(_ context.Context, _ uuid.UUID, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", errors.New("credential ref is required")
	}
	if !strings.HasPrefix(strings.ToLower(ref), "env:") {
		return "", fmt.Errorf("%w %q", ErrUnsupportedCredentialRef, ref)
	}
	name := strings.TrimSpace(ref[len("env:"):])
	if name == "" {
		return "", errors.New("env credential ref requires variable name")
	}
	value, ok := os.LookupEnv(name)
	if !ok || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("credential env var %q is not set", name)
	}
	return value, nil
}

type VaultSecretClient interface {
	ReadSecret(context.Context, string) (map[string]interface{}, error)
}

type VaultCredentialResolver struct {
	Client VaultSecretClient
}

func (r VaultCredentialResolver) ResolveCredential(ctx context.Context, _ uuid.UUID, ref string) (string, error) {
	path, key, err := parseVaultCredentialRef(ref)
	if err != nil {
		return "", err
	}
	if r.Client == nil {
		return "", errors.New("vault credential resolver is not configured")
	}
	data, err := r.Client.ReadSecret(ctx, path)
	if err != nil {
		return "", err
	}
	if len(data) == 0 {
		return "", fmt.Errorf("vault credential ref %q not found", ref)
	}
	value, ok := vaultCredentialValue(data, key)
	if !ok {
		if key != "" {
			return "", fmt.Errorf("vault credential ref %q missing key %q", ref, key)
		}
		return "", fmt.Errorf("vault credential ref %q has no default secret key", ref)
	}
	return value, nil
}

func parseVaultCredentialRef(ref string) (string, string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", "", errors.New("credential ref is required")
	}
	parsed, err := url.Parse(ref)
	if err != nil || strings.ToLower(parsed.Scheme) != "vault" {
		return "", "", fmt.Errorf("%w %q", ErrUnsupportedCredentialRef, ref)
	}
	path := strings.Trim(strings.TrimSpace(parsed.Host+parsed.Path), "/")
	if path == "" {
		return "", "", errors.New("vault credential ref requires path")
	}
	key := strings.TrimSpace(parsed.Fragment)
	if key == "" {
		key = strings.TrimSpace(parsed.Query().Get("key"))
	}
	return path, key, nil
}

func vaultCredentialValue(data map[string]interface{}, key string) (string, bool) {
	keys := []string{strings.TrimSpace(key)}
	if strings.TrimSpace(key) == "" {
		keys = []string{"value", "token", "api_key", "secret", "password", "bearer_token"}
	}
	for _, candidate := range keys {
		if candidate == "" {
			continue
		}
		value, ok := data[candidate]
		if !ok {
			continue
		}
		text := strings.TrimSpace(fmt.Sprint(value))
		if text == "" || text == "<nil>" {
			continue
		}
		return text, true
	}
	return "", false
}
