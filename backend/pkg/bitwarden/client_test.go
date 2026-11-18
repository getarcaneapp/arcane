package bitwarden

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestClientInternal(t *testing.T, mux *http.ServeMux) *Client {
	t.Helper()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	client, err := NewClient(server.Client(), server.URL+"/")
	require.NoError(t, err)
	return client
}

func TestStatusAndSync(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"data":{"object":"template","template":{"serverUrl":"https://vault.example","userEmail":"a@b","status":"unlocked"}}}`))
	})
	var synced atomic.Bool
	mux.HandleFunc("POST /sync", func(w http.ResponseWriter, _ *http.Request) {
		synced.Store(true)
		_, _ = w.Write([]byte(`{"success":true,"data":{"object":"message","title":"Syncing complete."}}`))
	})
	client := newTestClientInternal(t, mux)

	status, err := client.Status(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "unlocked", status.Status)
	assert.Equal(t, "a@b", status.UserEmail)

	require.NoError(t, client.Sync(t.Context()))
	assert.True(t, synced.Load())
}

func TestListItemsFiltersAndDropsTrash(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /list/object/items", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "f1", r.URL.Query().Get("folderid"))
		_, _ = w.Write([]byte(`{"success":true,"data":{"object":"list","data":[
			{"id":"1","name":"KEEP","type":1,"login":{"password":"x"}},
			{"id":"2","name":"TRASHED","type":1,"deletedDate":"2026-01-01T00:00:00Z"}
		]}}`))
	})
	client := newTestClientInternal(t, mux)

	items, err := client.ListItems(t.Context(), ItemFilter{FolderID: "f1"})
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "KEEP", items[0].Name)
}

func TestLockedVaultAndErrors(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /list/object/folders", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"success":false,"message":"Vault is locked."}`))
	})
	mux.HandleFunc("GET /object/item/missing", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"success":false,"message":"Not found."}`))
	})
	client := newTestClientInternal(t, mux)

	_, err := client.ListFolders(t.Context())
	require.ErrorIs(t, err, ErrVaultLocked)

	_, err = client.GetItem(t.Context(), "missing")
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusNotFound, apiErr.StatusCode)
	assert.Equal(t, "Not found.", apiErr.Message)
}

func TestParseServeURL(t *testing.T) {
	parsed, err := ParseServeURL(" http://bw-serve:8087/ ")
	require.NoError(t, err)
	assert.Equal(t, "http://bw-serve:8087", parsed.String())

	_, err = ParseServeURL("")
	require.Error(t, err)
	_, err = ParseServeURL("tcp://bw-serve:8087")
	require.Error(t, err)
}
