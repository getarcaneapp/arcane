package httpsource

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"getarcane.dev/types/v3"
)

func TestNormalizeTarget(t *testing.T) {
	target, err := NormalizeTarget(&types.SecretSourceHTTPTarget{Path: " /secrets/immich/ "})
	require.NoError(t, err)
	assert.Equal(t, "secrets/immich", target.Path)

	target, err = NormalizeTarget(nil)
	require.NoError(t, err)
	assert.Empty(t, target.Path)

	for _, bad := range []string{"a/../b", "a//b", "a?x=1", "a#b"} {
		_, err := NormalizeTarget(&types.SecretSourceHTTPTarget{Path: bad})
		assert.Error(t, err, bad)
	}
}

func TestTestAndFetch(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	mux.HandleFunc("GET /secrets/web", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"A":"1","LIST":[1,2]}`))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	provider, err := New(server.Client(), types.SecretSourceHTTPSettings{BaseURL: server.URL}, "")
	require.NoError(t, err)

	result := provider.Test(t.Context())
	assert.True(t, result.OK)
	assert.Contains(t, result.Message, "set a path")

	// A directory listing at the root (python -m http.server) is reachable too.
	listing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<html><body>web</body></html>`))
	}))
	t.Cleanup(listing.Close)
	html, err := New(listing.Client(), types.SecretSourceHTTPSettings{BaseURL: listing.URL}, "")
	require.NoError(t, err)
	assert.True(t, html.Test(t.Context()).OK)

	values, skipped, err := provider.Fetch(t.Context(), types.SecretSourceBindingTarget{HTTP: &types.SecretSourceHTTPTarget{Path: "secrets/web"}})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"A": "1"}, values)
	assert.Equal(t, []string{"LIST"}, skipped)
}
