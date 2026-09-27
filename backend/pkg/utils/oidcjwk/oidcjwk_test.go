package oidcjwk_test

import (
	"context"
	"crypto/mldsa"
	"encoding/base64"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/oidcjwk"
	"github.com/lestrrat-go/httprc/v3"
	"github.com/stretchr/testify/require"
)

func signCompactInternal(t *testing.T, sk *mldsa.PrivateKey, alg, kid string, claims map[string]any) string {
	t.Helper()
	header, err := json.Marshal(map[string]string{"alg": alg, "kid": kid, "typ": "JWT"})
	require.NoError(t, err)
	payload, err := json.Marshal(claims)
	require.NoError(t, err)
	signingInput := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	sig, err := sk.Sign(nil, []byte(signingInput), nil)
	require.NoError(t, err)
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func TestKeySetVerifySignature(t *testing.T) {
	tests := []struct {
		params         mldsa.Parameters
		tamper         bool
		initialFailure string
	}{
		{params: mldsa.MLDSA44()},
		{params: mldsa.MLDSA65()},
		{params: mldsa.MLDSA87()},
		{params: mldsa.MLDSA87(), tamper: true},
		{params: mldsa.MLDSA44(), initialFailure: "http"},
		{params: mldsa.MLDSA44(), initialFailure: "json"},
	}
	for _, tc := range tests {
		name := tc.params.String()
		if tc.tamper {
			name += "-tampered"
		}
		if tc.initialFailure != "" {
			name += "-" + tc.initialFailure
		}
		t.Run(name, func(t *testing.T) {
			sk, err := mldsa.GenerateKey(tc.params)
			require.NoError(t, err)

			jwks := fmt.Sprintf(`{"keys":[{"kty":"AKP","alg":%q,"kid":"pq","use":"sig","pub":%q}]}`,
				tc.params.String(), base64.RawURLEncoding.EncodeToString(sk.PublicKey().Bytes()))
			var failing atomic.Bool
			failing.Store(tc.initialFailure != "")
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if failing.Load() {
					if tc.initialFailure == "http" {
						w.WriteHeader(http.StatusServiceUnavailable)
					} else {
						if _, err := fmt.Fprint(w, `{"keys":`); err != nil {
							t.Errorf("write malformed JWKS response: %v", err)
						}
					}
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if _, err := fmt.Fprint(w, jwks); err != nil {
					t.Errorf("write JWKS response: %v", err)
				}
			}))
			defer srv.Close()

			token := signCompactInternal(t, sk, tc.params.String(), "pq", map[string]any{
				"iss": srv.URL,
				"sub": "alice",
				"aud": "arcane",
				"exp": time.Now().Add(5 * time.Minute).Unix(),
				"iat": time.Now().Unix(),
			})
			if tc.tamper {
				token = token[:len(token)-4] + "AAAA"
			}

			ctx := t.Context()
			manager := oidcjwk.NewKeySetManager(ctx)
			jwksURL := srv.URL + "/jwks?credential=test-secret"
			t.Cleanup(func() {
				shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				require.NoError(t, manager.Shutdown(shutdownCtx))
			})
			if tc.initialFailure != "" {
				initCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
				failedSet, initErr := manager.KeySet(initCtx, srv.Client(), jwksURL)
				cancel()
				require.Nil(t, failedSet)
				require.Error(t, initErr)
				require.NotErrorIs(t, initErr, context.DeadlineExceeded)
				require.NotContains(t, initErr.Error(), "test-secret")
				require.NotContains(t, fmt.Sprintf("%+v", initErr), "test-secret")
				if tc.initialFailure == "http" {
					require.ErrorIs(t, initErr, httprc.ErrUnexpectedStatusCode())
					require.ErrorContains(t, initErr, "503")
				} else {
					require.ErrorIs(t, initErr, httprc.ErrTransformerFailed())
				}
				failing.Store(false)
			}
			keySet, err := manager.KeySet(ctx, srv.Client(), jwksURL)
			require.NoError(t, err)
			cachedSet, err := manager.KeySet(ctx, srv.Client(), jwksURL)
			require.NoError(t, err)
			require.Same(t, keySet, cachedSet)
			verifier := oidc.NewVerifier(srv.URL, keySet, &oidc.Config{
				ClientID:             "arcane",
				SupportedSigningAlgs: oidcjwk.SupportedSigningAlgs(),
			})

			idToken, err := verifier.Verify(ctx, token)
			if tc.tamper {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, "alice", idToken.Subject)
		})
	}
}
