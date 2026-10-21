package secrets

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/podman"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

func newTestService(t *testing.T, handler http.HandlerFunc) *Service {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := podman.NewClient(server.URL)
	require.NoError(t, err)
	return NewService(func(context.Context) (podman.Runtime, error) {
		return podman.Runtime{Client: client}, nil
	})
}

func TestUpdateReplacesSecretAndReturnsInspect(t *testing.T) {
	replaced := false
	svc := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v4.0.0/libpod/secrets/create":
			query := r.URL.Query()
			assert.Equal(t, "db-pass", query.Get("name"))
			assert.Equal(t, "true", query.Get("replace"))
			assert.Equal(t, "file", query.Get("driver"))
			assert.JSONEq(t, `{"tier":"db"}`, query.Get("labels"))
			data, err := io.ReadAll(r.Body)
			assert.NoError(t, err)
			assert.Equal(t, "rotated", string(data))
			replaced = true
			_, _ = w.Write([]byte(`{"ID":"sec2"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v4.0.0/libpod/secrets/db-pass/json":
			assert.Empty(t, r.URL.Query().Get("showsecret"))
			_, _ = w.Write([]byte(`{"ID":"sec2","Spec":{"Name":"db-pass","Driver":{"Name":"file"},"Labels":{"tier":"db"}}}`))
		default:
			http.NotFound(w, r)
		}
	})

	secret, err := svc.Update(t.Context(), "db-pass", types.PodmanSecretUpdateRequest{
		Data:   "rotated",
		Driver: "file",
		Labels: map[string]string{"tier": "db"},
	})
	require.NoError(t, err)
	assert.True(t, replaced)
	assert.Equal(t, "sec2", secret.ID)
	assert.Equal(t, map[string]string{"tier": "db"}, secret.Spec.Labels)
}

func TestListPaginatedSearchesAndSortsSecrets(t *testing.T) {
	svc := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[
			{"ID":"a","Spec":{"Name":"zeta-token","Driver":{"Name":"file"}}},
			{"ID":"b","Spec":{"Name":"alpha-token","Driver":{"Name":"pass"}}},
			{"ID":"c","Spec":{"Name":"db-pass","Driver":{"Name":"file"}}}
		]`))
	})

	items, page, err := svc.ListPaginated(t.Context(), handlerutil.PaginationParams(0, 20, "name", "asc", "token"))
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, "alpha-token", items[0].Spec.Name)
	assert.Equal(t, "zeta-token", items[1].Spec.Name)
	assert.Equal(t, int64(2), page.TotalItems)
}
