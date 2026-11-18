package onepassword

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"getarcane.dev/arcane/v3/pkg/onepassword"
	"getarcane.dev/types/v3"
)

func TestItemsToValues(t *testing.T) {
	values, skipped, err := ItemsToValues([]*onepassword.Item{
		{Title: "DB_PASSWORD", Fields: []onepassword.Field{{Purpose: "PASSWORD", Value: "pw"}}},
		{Title: "NO_VALUE", Fields: []onepassword.Field{{Type: "URL", Value: "https://x"}}},
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"DB_PASSWORD": "pw"}, values)
	assert.Equal(t, []string{"NO_VALUE"}, skipped)

	_, _, err = ItemsToValues([]*onepassword.Item{
		{Title: "A", Fields: []onepassword.Field{{Purpose: "PASSWORD", Value: "1"}}},
		{Title: "A", Fields: []onepassword.Field{{Purpose: "PASSWORD", Value: "2"}}},
	})
	assert.ErrorContains(t, err, "more than one item is titled A")
}

func TestFieldsToValuesSkipsOTPAndUnlabeled(t *testing.T) {
	values, skipped, err := FieldsToValues([]onepassword.Field{
		{Label: "API_KEY", Type: "CONCEALED", Value: "k"},
		{Label: "TOTP", Type: "OTP", Value: "otpauth://..."},
		{Label: "", Type: "STRING", Value: "x"},
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"API_KEY": "k"}, values)
	assert.Equal(t, []string{"(unlabeled field)", "TOTP"}, skipped)
}

func TestNormalizeTarget(t *testing.T) {
	target, err := NormalizeTarget(&types.SecretSourceOnePasswordTarget{Scope: "vault", VaultID: " v1 ", ItemID: "ignored"})
	require.NoError(t, err)
	assert.Equal(t, &types.SecretSourceOnePasswordTarget{Scope: "vault", VaultID: "v1"}, target)

	_, err = NormalizeTarget(&types.SecretSourceOnePasswordTarget{Scope: "item", VaultID: "v1"})
	assert.Error(t, err)
	_, err = NormalizeTarget(&types.SecretSourceOnePasswordTarget{Scope: "folder", VaultID: "v1"})
	assert.Error(t, err)
}

func TestFetchVaultReadsEachItem(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/vaults/v1/items", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"id":"a","title":"A"},{"id":"b","title":"B"}]`))
	})
	mux.HandleFunc("GET /v1/vaults/v1/items/a", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"a","title":"A","fields":[{"purpose":"PASSWORD","value":"1"}]}`))
	})
	mux.HandleFunc("GET /v1/vaults/v1/items/b", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"b","title":"B","fields":[{"purpose":"NOTES","value":"note"}]}`))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	provider, err := New(server.Client(), types.SecretSourceOnePasswordSettings{ServerURL: server.URL}, "tok")
	require.NoError(t, err)

	values, _, err := provider.Fetch(t.Context(), types.SecretSourceBindingTarget{OnePassword: &types.SecretSourceOnePasswordTarget{Scope: "vault", VaultID: "v1"}})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"A": "1", "B": "note"}, values)
}
