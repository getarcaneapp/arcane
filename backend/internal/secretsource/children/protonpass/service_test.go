package protonpass

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"getarcane.dev/types/v3"
)

// fakeKitInternal answers like pass-kit.
func fakeKitInternal(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer kit-token" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"missing or wrong bearer token"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.EscapedPath() {
		case "/vaults":
			_, _ = w.Write([]byte(`{"vaults":[{"id":"S2","name":"zeta"},{"id":"S1==","name":"Arcane"}]}`))
		case "/vaults/S1==/items":
			_, _ = w.Write([]byte(`{"items":[{"id":"I2","title":"web","type":"custom"},{"id":"I1","title":"DB_PASSWORD","type":"login"}]}`))
		case "/secrets/S1==":
			_, _ = w.Write([]byte(`{"DB_PASSWORD":"pw","my item":null}`))
		case "/secrets/S1==/I2":
			_, _ = w.Write([]byte(`{"PORT":"8080","OTP":null}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"vault not found, or the token has no access to it"}`))
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func newProviderInternal(t *testing.T, token string) *Provider {
	t.Helper()
	server := fakeKitInternal(t)
	provider, err := New(server.Client(), types.SecretSourceProtonPassSettings{KitURL: server.URL}, token)
	require.NoError(t, err)
	return provider
}

func TestNormalizeTarget(t *testing.T) {
	target, err := NormalizeTarget(&types.SecretSourceProtonPassTarget{Scope: "vault", VaultID: " S1== ", ItemID: "ignored"})
	require.NoError(t, err)
	assert.Equal(t, &types.SecretSourceProtonPassTarget{Scope: "vault", VaultID: "S1=="}, target)

	_, err = NormalizeTarget(&types.SecretSourceProtonPassTarget{Scope: "item", VaultID: "S1"})
	require.Error(t, err)
	_, err = NormalizeTarget(&types.SecretSourceProtonPassTarget{Scope: "vault", VaultID: "../x"})
	require.Error(t, err)
	_, err = NormalizeTarget(&types.SecretSourceProtonPassTarget{Scope: "item", VaultID: "S1", ItemID: ".."})
	require.Error(t, err)
	_, err = NormalizeTarget(&types.SecretSourceProtonPassTarget{Scope: "folder", VaultID: "S1"})
	require.Error(t, err)
}

func TestTestAndBrowse(t *testing.T) {
	provider := newProviderInternal(t, "kit-token")
	result := provider.Test(t.Context())
	assert.True(t, result.OK, result.Message)
	assert.Equal(t, 2, result.VisibleCount)

	vaults, err := provider.Browse(t.Context(), types.SecretSourceBrowseQuery{Kind: "vaults"})
	require.NoError(t, err)
	assert.Equal(t, []types.SecretSourceBrowseItem{{ID: "S1==", Name: "Arcane"}, {ID: "S2", Name: "zeta"}}, vaults)

	items, err := provider.Browse(t.Context(), types.SecretSourceBrowseQuery{Kind: "items", VaultID: "S1=="})
	require.NoError(t, err)
	assert.Equal(t, []types.SecretSourceBrowseItem{
		{ID: "I1", Name: "DB_PASSWORD", Detail: "login"},
		{ID: "I2", Name: "web", Detail: "custom"},
	}, items)

	_, err = provider.Browse(t.Context(), types.SecretSourceBrowseQuery{Kind: "items"})
	require.Error(t, err)
}

func TestFetch(t *testing.T) {
	provider := newProviderInternal(t, "kit-token")
	values, skipped, err := provider.Fetch(t.Context(), types.SecretSourceBindingTarget{
		ProtonPass: &types.SecretSourceProtonPassTarget{Scope: "vault", VaultID: "S1=="},
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"DB_PASSWORD": "pw"}, values)
	assert.Equal(t, []string{"my item"}, skipped)

	values, skipped, err = provider.Fetch(t.Context(), types.SecretSourceBindingTarget{
		ProtonPass: &types.SecretSourceProtonPassTarget{Scope: "item", VaultID: "S1==", ItemID: "I2"},
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"PORT": "8080"}, values)
	assert.Equal(t, []string{"OTP"}, skipped)

	_, _, err = provider.Fetch(t.Context(), types.SecretSourceBindingTarget{
		ProtonPass: &types.SecretSourceProtonPassTarget{Scope: "vault", VaultID: "missing"},
	})
	require.Error(t, err)
}

func TestWrongKitToken(t *testing.T) {
	provider := newProviderInternal(t, "wrong")
	result := provider.Test(t.Context())
	assert.False(t, result.OK)
}
