package ci

import (
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectFederatedTokenFromGitHubActions(t *testing.T) {
	t.Parallel()

	var gotAudience string
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAudience = r.URL.Query().Get("audience")
		gotAuth = r.Header.Get("Authorization")
		assert.NoError(t, json.MarshalWrite(w, map[string]string{"value": "github.jwt"}))
	}))
	t.Cleanup(server.Close)

	env := map[string]string{
		"ACTIONS_ID_TOKEN_REQUEST_URL":   server.URL + "?api-version=2",
		"ACTIONS_ID_TOKEN_REQUEST_TOKEN": "request-token",
	}
	token, provider, err := DetectToken(t.Context(), "auto", "https://arcane.example.com", envGetterFromMap(env), server.Client())

	require.NoError(t, err)
	require.Equal(t, "github.jwt", token)
	require.Equal(t, "github", provider)
	require.Equal(t, "https://arcane.example.com", gotAudience)
	require.Equal(t, "Bearer request-token", gotAuth)
}

func TestDetectFederatedTokenFromGitLabLegacy(t *testing.T) {
	t.Parallel()

	token, provider, err := DetectToken(t.Context(), "auto", "https://arcane.example.com", envGetterFromMap(map[string]string{
		"CI_JOB_JWT_V2": "gitlab.jwt",
	}), http.DefaultClient)

	require.NoError(t, err)
	require.Equal(t, "gitlab.jwt", token)
	require.Equal(t, "gitlab", provider)
}

func TestDetectFederatedTokenProviderMismatch(t *testing.T) {
	t.Parallel()

	_, _, err := DetectToken(t.Context(), "github", "aud", envGetterFromMap(map[string]string{
		"CI_JOB_JWT_V2": "gitlab.jwt",
	}), http.DefaultClient)

	require.Error(t, err)
}

func envGetterFromMap(values map[string]string) func(string) string {
	return func(key string) string {
		return values[key]
	}
}
