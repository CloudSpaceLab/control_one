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
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/stretchr/testify/require"
)

func TestOIDCPublishedKeyValidation(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	other, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	var issuer string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/.well-known/openid-configuration" {
			_ = json.NewEncoder(w).Encode(map[string]any{"issuer": issuer, "jwks_uri": issuer + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]any{"kty": "RSA", "kid": "fixture", "alg": "RS256", "use": "sig", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())}}})
	}))
	defer server.Close()
	issuer = server.URL
	provider, err := NewOIDCProvider(context.Background(), config.OIDCConfig{Enabled: true, IssuerURL: issuer, ClientID: "console", Audience: []string{"console"}}, config.RBACConfig{DefaultRole: "viewer", RoleMappings: map[string][]string{"operator": {"operations"}}})
	require.NoError(t, err)
	for _, scenario := range []string{"valid", "wrong_signature", "wrong_issuer", "wrong_audience", "expired", "missing_subject"} {
		t.Run(scenario, func(t *testing.T) {
			claims := map[string]any{"iss": issuer, "aud": "console", "sub": "reviewer", "exp": time.Now().Add(time.Minute).Unix(), "iat": time.Now().Unix(), "groups": []string{"operations"}}
			signingKey := key
			switch scenario {
			case "wrong_signature":
				signingKey = other
			case "wrong_issuer":
				claims["iss"] = "https://other.invalid"
			case "wrong_audience":
				claims["aud"] = "other-client"
			case "expired":
				claims["exp"] = time.Now().Add(-time.Minute).Unix()
			case "missing_subject":
				delete(claims, "sub")
			}
			header, _ := json.Marshal(map[string]string{"alg": "RS256", "kid": "fixture"})
			payload, _ := json.Marshal(claims)
			unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
			digest := sha256.Sum256([]byte(unsigned))
			sig, err := rsa.SignPKCS1v15(rand.Reader, signingKey, crypto.SHA256, digest[:])
			require.NoError(t, err)
			principal, err := provider.Verify(context.Background(), unsigned+"."+base64.RawURLEncoding.EncodeToString(sig))
			if scenario != "valid" {
				require.Error(t, err)
				require.Nil(t, principal)
				return
			}
			require.NoError(t, err)
			require.Equal(t, "oidc:"+issuer, principal.authenticationAuthority)
			require.Equal(t, "reviewer", principal.Subject)
			require.ElementsMatch(t, []string{"viewer", "operator"}, principal.Roles)
		})
	}
}
