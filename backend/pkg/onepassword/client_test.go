package onepassword

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVaultsItemsAndItem(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/vaults", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`[{"id":"v1","name":"Homelab","description":"deploy"}]`))
	})
	mux.HandleFunc("GET /v1/vaults/v1/items", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"id":"i1","title":"DB_PASSWORD","category":"PASSWORD"}]`))
	})
	mux.HandleFunc("GET /v1/vaults/v1/items/i1", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"i1","title":"DB_PASSWORD","category":"PASSWORD","fields":[{"id":"password","type":"CONCEALED","purpose":"PASSWORD","label":"password","value":"pw"}]}`))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	client, err := NewClient(server.Client(), server.URL, "tok")
	require.NoError(t, err)

	vaults, err := client.Vaults(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []Vault{{ID: "v1", Name: "Homelab", Description: "deploy"}}, vaults)

	items, err := client.Items(t.Context(), "v1")
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Empty(t, items[0].Fields)

	item, err := client.GetItem(t.Context(), "v1", "i1")
	require.NoError(t, err)
	value, ok := PrimaryValue(item)
	assert.True(t, ok)
	assert.Equal(t, "pw", value)
}

func TestPrimaryValueOrder(t *testing.T) {
	credential := &Item{Fields: []Field{
		{Type: "STRING", Label: "username", Purpose: PurposeUsername, Value: "u"},
		{Type: "CONCEALED", Label: "credential", Value: "api-key"},
	}}
	value, ok := PrimaryValue(credential)
	assert.True(t, ok)
	assert.Equal(t, "api-key", value)

	note := &Item{Fields: []Field{{Type: "STRING", Purpose: PurposeNotes, Label: "notesPlain", Value: "text"}}}
	value, ok = PrimaryValue(note)
	assert.True(t, ok)
	assert.Equal(t, "text", value)

	_, ok = PrimaryValue(&Item{Fields: []Field{{Type: "STRING", Label: "url", Value: "x"}}})
	assert.False(t, ok)
}

func TestUnauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"status":401,"message":"Invalid token signature"}`))
	}))
	t.Cleanup(server.Close)
	client, err := NewClient(server.Client(), server.URL, "bad")
	require.NoError(t, err)
	_, err = client.Vaults(t.Context())
	assert.ErrorContains(t, err, "1Password Connect returned HTTP 401: Invalid token signature")
}
