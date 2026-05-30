package ci

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDetectFederatedTokenFromGitHubActionsInternal(t *testing.T) {
	t.Parallel()

	var gotAudience string
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAudience = r.URL.Query().Get("audience")
		gotAuth = r.Header.Get("Authorization")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]string{"value": "github.jwt"}))
	}))
	t.Cleanup(server.Close)

	env := map[string]string{
		"ACTIONS_ID_TOKEN_REQUEST_URL":   server.URL + "?api-version=2",
		"ACTIONS_ID_TOKEN_REQUEST_TOKEN": "request-token",
	}
	token, provider, err := DetectToken(context.Background(), "auto", "https://arcane.example.com", envGetterFromMapInternal(env), server.Client())

	require.NoError(t, err)
	require.Equal(t, "github.jwt", token)
	require.Equal(t, "github", provider)
	require.Equal(t, "https://arcane.example.com", gotAudience)
	require.Equal(t, "Bearer request-token", gotAuth)
}

func TestDetectFederatedTokenFromGitLabLegacyInternal(t *testing.T) {
	t.Parallel()

	token, provider, err := DetectToken(context.Background(), "auto", "https://arcane.example.com", envGetterFromMapInternal(map[string]string{
		"CI_JOB_JWT_V2": "gitlab.jwt",
	}), http.DefaultClient)

	require.NoError(t, err)
	require.Equal(t, "gitlab.jwt", token)
	require.Equal(t, "gitlab", provider)
}

func TestDetectFederatedTokenProviderMismatchInternal(t *testing.T) {
	t.Parallel()

	_, _, err := DetectToken(context.Background(), "github", "aud", envGetterFromMapInternal(map[string]string{
		"CI_JOB_JWT_V2": "gitlab.jwt",
	}), http.DefaultClient)

	require.Error(t, err)
}

func envGetterFromMapInternal(values map[string]string) func(string) string {
	return func(key string) string {
		return values[key]
	}
}
