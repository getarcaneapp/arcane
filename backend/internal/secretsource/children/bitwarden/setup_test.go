package bitwarden

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"getarcane.dev/types/v3"
)

func TestNormalizeSetupTargetBitwarden(t *testing.T) {
	target, err := NormalizeSetupTarget(types.SecretSourceSetupTarget{Mode: "new-folder"}, "immich")
	require.NoError(t, err)
	assert.Equal(t, types.SecretSourceSetupTarget{Mode: "new-folder", FolderName: "immich"}, target)

	target, err = NormalizeSetupTarget(types.SecretSourceSetupTarget{Mode: "existing-folder", FolderID: " f1 ", Environment: "prod"}, "x")
	require.NoError(t, err)
	assert.Equal(t, types.SecretSourceSetupTarget{Mode: "existing-folder", FolderID: "f1"}, target)

	for _, bad := range []types.SecretSourceSetupTarget{{Mode: "existing-folder"}, {Mode: "new-project"}, {Mode: "new-folder"}} {
		_, err = NormalizeSetupTarget(bad, "")
		assert.Error(t, err, "%+v", bad)
	}
}

func TestRemoteItemsIncludesEmptyAndRejectsDuplicates(t *testing.T) {
	body := `{"success":true,"data":{"object":"list","data":[
		{"id":"1","name":"DB_PASSWORD","type":1,"login":{"password":"pw"}},
		{"id":"2","name":"EMPTY","type":1,"login":{"password":null}},
		{"id":"3","name":"NOTE","type":2,"notes":"text"}
	]}}`
	mux := http.NewServeMux()
	mux.HandleFunc("POST /sync", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"success":true}`))
	})
	mux.HandleFunc("GET /list/object/items", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	setup, err := NewSetup(server.Client(), types.SecretSourceBitwardenSettings{ServeURL: server.URL})
	require.NoError(t, err)

	remote, err := setup.RemoteItems(t.Context(), "f1")
	require.NoError(t, err)
	assert.Equal(t, map[string]RemoteItem{
		"DB_PASSWORD": {ID: "1", Value: "pw"},
		"EMPTY":       {ID: "2"},
		"NOTE":        {ID: "3", Value: "text"},
	}, remote)

	empty, err := setup.RemoteItems(t.Context(), "")
	require.NoError(t, err)
	assert.Empty(t, empty)

	body = `{"success":true,"data":{"object":"list","data":[{"id":"1","name":"A","type":2,"notes":"x"},{"id":"2","name":"A","type":2,"notes":"y"}]}}`
	_, err = setup.RemoteItems(t.Context(), "f1")
	require.ErrorContains(t, err, `"A"`)
}
