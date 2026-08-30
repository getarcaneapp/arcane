package mldsajose_test

import (
	"crypto/mldsa"
	"encoding/base64"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/mldsajose"
	"github.com/golang-jwt/jwt/v5"
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
		params mldsa.Parameters
		tamper bool
	}{
		{params: mldsa.MLDSA44()},
		{params: mldsa.MLDSA65()},
		{params: mldsa.MLDSA87()},
		{params: mldsa.MLDSA87(), tamper: true},
	}
	for _, tc := range tests {
		name := tc.params.String()
		if tc.tamper {
			name += "-tampered"
		}
		t.Run(name, func(t *testing.T) {
			sk, err := mldsa.GenerateKey(tc.params)
			require.NoError(t, err)

			jwks := fmt.Sprintf(`{"keys":[{"kty":"AKP","alg":%q,"kid":"pq","use":"sig","pub":%q}]}`,
				tc.params.String(), base64.RawURLEncoding.EncodeToString(sk.PublicKey().Bytes()))
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, jwks)
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
			keySet := mldsajose.NewKeySet(oidc.ClientContext(ctx, srv.Client()), srv.URL)
			verifier := oidc.NewVerifier(srv.URL, keySet, &oidc.Config{
				ClientID:             "arcane",
				SupportedSigningAlgs: mldsajose.SupportedSigningAlgs(),
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

func TestSigningMethodMLDSA87(t *testing.T) {
	sk, err := mldsa.GenerateKey(mldsajose.Parameters)
	require.NoError(t, err)

	signed, err := jwt.NewWithClaims(mldsajose.SigningMethodMLDSA87, jwt.MapClaims{"sub": "x"}).SignedString(sk)
	require.NoError(t, err)

	parsed, err := jwt.Parse(signed, func(t *jwt.Token) (any, error) {
		return sk.PublicKey(), nil
	}, jwt.WithValidMethods([]string{mldsajose.AlgMLDSA87}))
	require.NoError(t, err)
	require.True(t, parsed.Valid)

	_, err = mldsajose.SigningMethodMLDSA87.Sign("payload", []byte("not-a-key"))
	require.ErrorIs(t, err, mldsajose.ErrInvalidKeyType)
}
