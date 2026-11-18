package vault

import (
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newServerInternal(t *testing.T, mux *http.ServeMux) string {
	t.Helper()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server.URL
}

func TestTokenAuthReadsKVv2AndSendsNamespace(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/secret/data/apps/immich", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "root", r.Header.Get("X-Vault-Token"))
		assert.Equal(t, "team-a", r.Header.Get("X-Vault-Namespace"))
		_, _ = w.Write([]byte(`{"data":{"data":{"DB_PASSWORD":"pw","PORT":5432},"metadata":{"version":3}}}`))
	})
	client, err := NewClient(nil, Config{Address: newServerInternal(t, mux), Namespace: "team-a", Secret: "root"})
	require.NoError(t, err)

	data, version, err := client.Read(t.Context(), "secret", 2, "/apps/immich/")
	require.NoError(t, err)
	assert.Equal(t, 3, version)
	assert.JSONEq(t, `"pw"`, string(data["DB_PASSWORD"]))
	assert.JSONEq(t, `5432`, string(data["PORT"]))
}

func TestKVv1ReadAndList(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/kv/app", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"A":"1"}}`))
	})
	mux.HandleFunc("GET /v1/kv/", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "true", r.URL.Query().Get("list"))
		_, _ = w.Write([]byte(`{"data":{"keys":["app","team/"]}}`))
	})
	client, err := NewClient(nil, Config{Address: newServerInternal(t, mux), Secret: "t"})
	require.NoError(t, err)

	data, _, err := client.Read(t.Context(), "kv", 1, "app")
	require.NoError(t, err)
	assert.JSONEq(t, `"1"`, string(data["A"]))

	keys, err := client.List(t.Context(), "kv", 1, "")
	require.NoError(t, err)
	assert.Equal(t, []string{"app", "team/"}, keys)
}

func TestMissingSecretIsNotFoundAndEmptyListIsEmpty(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/secret/data/nope", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":[]}`))
	})
	mux.HandleFunc("GET /v1/secret/metadata/empty/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	client, err := NewClient(nil, Config{Address: newServerInternal(t, mux), Secret: "t"})
	require.NoError(t, err)

	_, _, err = client.Read(t.Context(), "secret", 2, "nope")
	assert.True(t, IsNotFound(err))

	keys, err := client.List(t.Context(), "secret", 2, "empty")
	require.NoError(t, err)
	assert.Empty(t, keys)
}

func TestAppRoleLogsInOnceAndCachesToken(t *testing.T) {
	var logins atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/auth/approle/login", func(w http.ResponseWriter, r *http.Request) {
		logins.Add(1)
		assert.Empty(t, r.Header.Get("X-Vault-Token"))
		var body map[string]string
		raw, _ := io.ReadAll(r.Body)
		require.NoError(t, json.Unmarshal(raw, &body))
		assert.Equal(t, map[string]string{"role_id": "role", "secret_id": "sid"}, body)
		_, _ = w.Write([]byte(`{"auth":{"client_token":"issued","lease_duration":3600}}`))
	})
	mux.HandleFunc("GET /v1/auth/token/lookup-self", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "issued", r.Header.Get("X-Vault-Token"))
		_, _ = w.Write([]byte(`{"data":{"display_name":"approle","policies":["default","arcane-read"]}}`))
	})
	client, err := NewClient(nil, Config{Address: newServerInternal(t, mux), AuthMethod: AuthAppRole, RoleID: "role", Secret: "sid"})
	require.NoError(t, err)

	for range 3 {
		info, lookupErr := client.LookupSelf(t.Context())
		require.NoError(t, lookupErr)
		assert.Equal(t, []string{"default", "arcane-read"}, info.Policies)
	}
	assert.Equal(t, int32(1), logins.Load())

	// A secret whose path ends in "login" still gets the token.
	mux.HandleFunc("GET /v1/secret/data/web/login", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "issued", r.Header.Get("X-Vault-Token"))
		_, _ = w.Write([]byte(`{"data":{"data":{"A":"1"},"metadata":{"version":1}}}`))
	})
	_, _, err = client.Read(t.Context(), "secret", 2, "web/login")
	require.NoError(t, err)
}

func TestWriteKVv2SendsCAS(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/secret/data/app", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		assert.JSONEq(t, `{"data":{"A":"1","N":2},"options":{"cas":4}}`, string(raw))
		_, _ = w.Write([]byte(`{"data":{"version":5}}`))
	})
	client, err := NewClient(nil, Config{Address: newServerInternal(t, mux), Secret: "t"})
	require.NoError(t, err)
	require.NoError(t, client.Write(t.Context(), "secret", 2, "app", map[string]any{"A": "1", "N": 2}, 4))
}

func TestKVMountsKeepsOnlyKV(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/sys/internal/ui/mounts", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"secret":{"secret/":{"type":"kv","options":{"version":"2"}},"kv/":{"type":"kv","options":null},"pki/":{"type":"pki"}}}}`))
	})
	client, err := NewClient(nil, Config{Address: newServerInternal(t, mux), Secret: "t"})
	require.NoError(t, err)
	mounts, err := client.KVMounts(t.Context())
	require.NoError(t, err)
	assert.ElementsMatch(t, []Mount{{Path: "secret", Type: "kv", Version: "2"}, {Path: "kv", Type: "kv", Version: "1"}}, mounts)
}

func TestNewClientValidates(t *testing.T) {
	_, err := NewClient(nil, Config{Address: "http://x", Secret: ""})
	assert.ErrorContains(t, err, "token is required")
	_, err = NewClient(nil, Config{Address: "http://x", AuthMethod: AuthAppRole, Secret: "s"})
	assert.ErrorContains(t, err, "role ID")
	_, err = NewClient(nil, Config{Address: "http://x", AuthMethod: "ldap", Secret: "s"})
	assert.ErrorContains(t, err, "unknown auth method")
}

func TestAppRoleLogsInAgainAfterRevokedToken(t *testing.T) {
	var logins atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/auth/approle/login", func(w http.ResponseWriter, _ *http.Request) {
		n := logins.Add(1)
		_, _ = w.Write([]byte(`{"auth":{"client_token":"t` + string(rune('0'+n)) + `","lease_duration":3600}}`))
	})
	mux.HandleFunc("GET /v1/auth/token/lookup-self", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Vault-Token") == "t1" {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"errors":["permission denied"]}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"display_name":"approle"}}`))
	})
	client, err := NewClient(nil, Config{Address: newServerInternal(t, mux), AuthMethod: AuthAppRole, RoleID: "r", Secret: "s"})
	require.NoError(t, err)

	_, err = client.LookupSelf(t.Context())
	require.NoError(t, err)
	assert.Equal(t, int32(2), logins.Load())
}

func TestShortLeaseIsStillCached(t *testing.T) {
	var logins atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/auth/approle/login", func(w http.ResponseWriter, _ *http.Request) {
		logins.Add(1)
		_, _ = w.Write([]byte(`{"auth":{"client_token":"t","lease_duration":20}}`))
	})
	mux.HandleFunc("GET /v1/auth/token/lookup-self", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{}}`))
	})
	client, err := NewClient(nil, Config{Address: newServerInternal(t, mux), AuthMethod: AuthAppRole, RoleID: "r", Secret: "s"})
	require.NoError(t, err)
	for range 3 {
		_, err = client.LookupSelf(t.Context())
		require.NoError(t, err)
	}
	assert.Equal(t, int32(1), logins.Load())
}
