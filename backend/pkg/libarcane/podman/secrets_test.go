package podman

import (
	"io"
	"net/http"
	"testing"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientSecrets(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v4.0.0/libpod/secrets/json":
			_, _ = w.Write([]byte(`[{"ID":"sec1","CreatedAt":"2026-01-02T03:04:05Z","Spec":{"Name":"db-pass","Driver":{"Name":"file"},"Labels":{"app":"db"}}}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/v4.0.0/libpod/secrets/db-pass/json":
			assert.Empty(t, r.URL.Query().Get("showsecret"))
			_, _ = w.Write([]byte(`{"ID":"sec1","CreatedAt":"2026-01-02T03:04:05Z","Spec":{"Name":"db-pass","Driver":{"Name":"file","Options":{"path":"/s"}},"Labels":{"app":"db"}}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v4.0.0/libpod/secrets/create" && r.URL.Query().Get("replace") == "true":
			assert.Equal(t, "db-pass", r.URL.Query().Get("name"))
			data, err := io.ReadAll(r.Body)
			assert.NoError(t, err)
			assert.Equal(t, "rotated", string(data))
			_, _ = w.Write([]byte(`{"ID":"sec2"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v4.0.0/libpod/secrets/create":
			query := r.URL.Query()
			assert.Equal(t, "db-pass", query.Get("name"))
			assert.Equal(t, "file", query.Get("driver"))
			assert.JSONEq(t, `{"app":"db"}`, query.Get("labels"))
			assert.Empty(t, query.Get("driveropts"))
			data, err := io.ReadAll(r.Body)
			assert.NoError(t, err)
			assert.Equal(t, "hunter2", string(data))
			_, _ = w.Write([]byte(`{"ID":"sec1"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v4.0.0/libpod/secrets/db-pass":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})

	secrets, err := client.SecretList(t.Context(), nil)
	require.NoError(t, err)
	require.Len(t, secrets, 1)
	assert.Equal(t, "db-pass", secrets[0].Spec.Name)
	assert.Equal(t, "file", secrets[0].Spec.Driver.Name)

	id, err := client.SecretCreate(t.Context(), types.PodmanSecretCreateRequest{
		Name:   "db-pass",
		Data:   "hunter2",
		Driver: "file",
		Labels: map[string]string{"app": "db"},
	}, false)
	require.NoError(t, err)
	assert.Equal(t, "sec1", id)

	secret, err := client.SecretInspect(t.Context(), "db-pass")
	require.NoError(t, err)
	assert.Equal(t, "sec1", secret.ID)
	assert.Equal(t, map[string]string{"path": "/s"}, secret.Spec.Driver.Options)
	assert.Equal(t, map[string]string{"app": "db"}, secret.Spec.Labels)

	replacedID, err := client.SecretCreate(t.Context(), types.PodmanSecretCreateRequest{
		Name: "db-pass",
		Data: "rotated",
	}, true)
	require.NoError(t, err)
	assert.Equal(t, "sec2", replacedID)

	require.NoError(t, client.SecretRemove(t.Context(), "db-pass"))
}
