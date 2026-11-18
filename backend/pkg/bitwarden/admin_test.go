package bitwarden

import (
	"encoding/json/v2"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateFolderAndSecureNote(t *testing.T) {
	var folderBody, itemBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("POST /object/folder", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.NoError(t, json.UnmarshalRead(r.Body, &folderBody))
		_, _ = w.Write([]byte(`{"success":true,"data":{"object":"folder","id":"f-new","name":"immich"}}`))
	})
	mux.HandleFunc("POST /object/item", func(w http.ResponseWriter, r *http.Request) {
		assert.NoError(t, json.UnmarshalRead(r.Body, &itemBody))
		_, _ = w.Write([]byte(`{"success":true,"data":{"object":"item","id":"i-new","name":"DB_PASSWORD","type":2,"notes":"pw"}}`))
	})
	client := newTestClientInternal(t, mux)

	folder, err := client.CreateFolder(t.Context(), " immich ")
	require.NoError(t, err)
	assert.Equal(t, "f-new", *folder.ID)
	assert.Equal(t, map[string]any{"name": "immich"}, folderBody)

	item, err := client.CreateSecureNote(t.Context(), "f-new", "DB_PASSWORD", "pw")
	require.NoError(t, err)
	assert.Equal(t, "i-new", item.ID)
	assert.Equal(t, float64(ItemTypeSecureNote), itemBody["type"])
	assert.Equal(t, "f-new", itemBody["folderId"])
	assert.Equal(t, "pw", itemBody["notes"])
	assert.Equal(t, map[string]any{"type": float64(0)}, itemBody["secureNote"])

	_, err = client.CreateFolder(t.Context(), "")
	require.Error(t, err)
}

func TestSetItemValueKeepsTheRestOfTheItem(t *testing.T) {
	items := map[string]string{
		"note":  `{"id":"note","type":2,"name":"A","notes":"old","folderId":"f","fields":[{"name":"x","value":"y","type":0}],"passwordHistory":[]}`,
		"login": `{"id":"login","type":1,"name":"B","login":{"username":"u","password":"old","uris":[]}}`,
		"card":  `{"id":"card","type":3,"name":"C"}`,
	}
	put := map[string]map[string]any{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /object/item/{id}", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"data":` + items[r.PathValue("id")] + `}`))
	})
	mux.HandleFunc("PUT /object/item/{id}", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		assert.NoError(t, json.UnmarshalRead(r.Body, &body))
		put[r.PathValue("id")] = body
		_, _ = w.Write([]byte(`{"success":true,"data":{"object":"item"}}`))
	})
	client := newTestClientInternal(t, mux)

	require.NoError(t, client.SetItemValue(t.Context(), "note", "new"))
	assert.Equal(t, "new", put["note"]["notes"])
	assert.Equal(t, "f", put["note"]["folderId"])
	assert.Len(t, put["note"]["fields"], 1)

	require.NoError(t, client.SetItemValue(t.Context(), "login", "new"))
	login := put["login"]["login"].(map[string]any)
	assert.Equal(t, "new", login["password"])
	assert.Equal(t, "u", login["username"])

	require.Error(t, client.SetItemValue(t.Context(), "card", "x"))
	assert.NotContains(t, put, "card")
}
