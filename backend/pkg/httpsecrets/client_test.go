package httpsecrets

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"getarcane.dev/arcane/v3/pkg/secretapi"
)

func TestGetWithTokenAndPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
		assert.Equal(t, "/secrets/my%20app", r.URL.EscapedPath())
		_, _ = w.Write([]byte(`{"A":"1","PORT":80}`))
	}))
	t.Cleanup(server.Close)
	client, err := NewClient(server.Client(), server.URL+"/secrets/", "tok")
	require.NoError(t, err)

	object, err := client.Get(t.Context(), "/my app/")
	require.NoError(t, err)
	assert.Len(t, object, 2)
}

func TestGetWithoutTokenSendsNoAuthorization(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Empty(t, r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`null`))
	}))
	t.Cleanup(server.Close)
	client, err := NewClient(server.Client(), server.URL, "")
	require.NoError(t, err)

	object, err := client.Get(t.Context(), "")
	require.NoError(t, err)
	assert.Empty(t, object)
}

func TestGetRejectsNonObject(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`["a","b"]`))
	}))
	t.Cleanup(server.Close)
	client, err := NewClient(server.Client(), server.URL, "")
	require.NoError(t, err)
	_, err = client.Get(t.Context(), "")
	assert.ErrorIs(t, err, secretapi.ErrUnexpectedShape)
	assert.NotContains(t, err.Error(), `"a"`, "decode errors must not quote the body")
}
