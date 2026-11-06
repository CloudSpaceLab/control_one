package auth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestOIDCDiscoveryRecoversWithoutProcessRestart(t *testing.T) {
	var available atomic.Bool
	var requests atomic.Int32
	var issuer string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if !available.Load() {
			http.Error(w, "isolated discovery outage", 503)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"issuer": issuer, "jwks_uri": issuer + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}})
	}))
	defer server.Close()
	issuer = server.URL
	cfg := config.AuthConfig{OIDC: config.OIDCConfig{Enabled: true, IssuerURL: issuer, ClientID: "browser", Audience: []string{"api"}}}
	mw := NewMiddleware(zap.NewNop(), false, cfg, &fakeIdentityStore{})
	p, err := mw.ensureOIDCProvider(context.Background())
	require.Error(t, err)
	require.Nil(t, p)
	// Concurrent requests during a failed discovery backoff do not hammer the issuer.
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, e := mw.ensureOIDCProvider(context.Background())
			if e == nil || p != nil {
				t.Error("failed discovery supplied a verifier")
			}
		}()
	}
	wg.Wait()
	require.EqualValues(t, 1, requests.Load())
	available.Store(true)
	mw.oidcMu.Lock()
	mw.oidcRetryAt = time.Now().Add(-time.Second)
	mw.oidcMu.Unlock()
	p, err = mw.ensureOIDCProvider(context.Background())
	require.NoError(t, err)
	require.NotNil(t, p)
	require.EqualValues(t, 2, requests.Load())
	again, err := mw.ensureOIDCProvider(context.Background())
	require.NoError(t, err)
	require.Same(t, p, again)
	require.EqualValues(t, 2, requests.Load())
}
func TestOIDCSigningKeyRotationAndOutage(t *testing.T) {
	first, e := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, e)
	second, e := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, e)
	rogue, e := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, e)
	var mu sync.Mutex
	current := first
	kid := "first"
	outage := false
	var issuer string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/.well-known/openid-configuration" {
			_ = json.NewEncoder(w).Encode(map[string]any{"issuer": issuer, "jwks_uri": issuer + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}})
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if outage {
			http.Error(w, "isolated key service outage", 503)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]any{"kty": "RSA", "kid": kid, "use": "sig", "alg": "RS256", "n": base64.RawURLEncoding.EncodeToString(current.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(current.E)).Bytes())}}})
	}))
	defer server.Close()
	issuer = server.URL
	provider, e := NewOIDCProvider(context.Background(), config.OIDCConfig{Enabled: true, IssuerURL: issuer, ClientID: "browser", Audience: []string{"api"}}, config.RBACConfig{DefaultRole: "viewer"})
	require.NoError(t, e)
	token := func(key *rsa.PrivateKey, id string) string {
		header, _ := json.Marshal(map[string]any{"alg": "RS256", "typ": "at+jwt", "kid": id})
		payload, _ := json.Marshal(map[string]any{"iss": issuer, "aud": "api", "sub": "reviewer", "client_id": "browser", "jti": "fixture", "scope": "controlone.access", "iat": time.Now().Unix(), "exp": time.Now().Add(time.Minute).Unix()})
		unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
		digest := sha256.Sum256([]byte(unsigned))
		sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
		require.NoError(t, err)
		return unsigned + "." + base64.RawURLEncoding.EncodeToString(sig)
	}
	old := token(first, "first")
	_, e = provider.Verify(context.Background(), old)
	require.NoError(t, e)
	mu.Lock()
	outage = true
	mu.Unlock()
	_, e = provider.Verify(context.Background(), old)
	require.NoError(t, e, "a previously verified cached key remains trusted until policy/expiry invalidates the token")
	next := token(second, "second")
	_, e = provider.Verify(context.Background(), next)
	require.Error(t, e, "unknown signing key during outage must fail closed")
	mu.Lock()
	outage = false
	current = second
	kid = "second"
	mu.Unlock()
	_, e = provider.Verify(context.Background(), next)
	require.NoError(t, e, "legitimate rotation must recover with the same verifier")
	_, e = provider.Verify(context.Background(), token(rogue, "second"))
	require.Error(t, e, "matching key ID does not bypass signature checks")
	// Providers can rotate key material while retaining a key ID: refresh on failed signature.
	mu.Lock()
	current = first
	mu.Unlock()
	_, e = provider.Verify(context.Background(), token(first, "second"))
	require.NoError(t, e)
}
