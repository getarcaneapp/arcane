package vault

import (
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"getarcane.dev/types/v3"
)

type (
	// fakeKVInternal is a tiny KV v2 store with check-and-set.
	fakeKVInternal struct {
		data    map[string]string
		writes  []string
		version int
		mu      sync.Mutex
	}
)

func TestNormalizeTargetDefaultsAndRejectsTraversal(t *testing.T) {
	target, err := NormalizeTarget(&types.SecretSourceVaultTarget{Path: " /apps/immich/ "})
	require.NoError(t, err)
	assert.Equal(t, &types.SecretSourceVaultTarget{Mount: "secret", Path: "apps/immich", KVVersion: 2}, target)

	for _, bad := range []types.SecretSourceVaultTarget{
		{Path: ""},
		{Path: "apps/../sys"},
		{Path: "apps//x"},
		{Mount: "..", Path: "x"},
		{Path: "x", KVVersion: 3},
		{Mount: "auth", Path: "token/lookup-self", KVVersion: 1},
		{Mount: "sys", Path: "internal/ui/mounts"},
		{Mount: "Identity/x", Path: "y"},
		{Mount: "cubbyhole", Path: "x"},
	} {
		_, err := NormalizeTarget(&bad)
		assert.Error(t, err, bad)
	}
}

func TestNormalizeSettings(t *testing.T) {
	settings, err := NormalizeSettings(&types.SecretSourceVaultSettings{Address: "http://openbao:8200/", Namespace: "/team/"})
	require.NoError(t, err)
	assert.Equal(t, &types.SecretSourceVaultSettings{Address: "http://openbao:8200", Namespace: "team", AuthMethod: "token"}, settings)

	settings, err = NormalizeSettings(&types.SecretSourceVaultSettings{Address: "http://v:8200", SetupToken: true})
	require.NoError(t, err)
	assert.True(t, settings.SetupToken, "the setup token flag must survive normalization")

	settings, err = NormalizeSettings(&types.SecretSourceVaultSettings{Address: "http://v:8200", AuthMethod: "approle", RoleID: "r"})
	require.NoError(t, err)
	assert.Equal(t, "approle", settings.AppRoleMount)

	_, err = NormalizeSettings(&types.SecretSourceVaultSettings{Address: "http://v:8200", AuthMethod: "approle"})
	assert.Error(t, err)
}

func TestFetchSkipsNonScalarValues(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/secret/data/app", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"data":{"DB":"pw","PORT":5432,"NESTED":{"a":1}},"metadata":{"version":1}}}`))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	provider, err := New(server.Client(), types.SecretSourceVaultSettings{Address: server.URL, AuthMethod: "token"}, "t")
	require.NoError(t, err)

	values, skipped, err := provider.Fetch(t.Context(), types.SecretSourceBindingTarget{Vault: &types.SecretSourceVaultTarget{Mount: "secret", Path: "app", KVVersion: 2}})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"DB": "pw", "PORT": "5432"}, values)
	assert.Equal(t, []string{"NESTED"}, skipped)

	_, _, err = provider.Fetch(t.Context(), types.SecretSourceBindingTarget{Vault: &types.SecretSourceVaultTarget{Mount: "secret", Path: "missing", KVVersion: 2}})
	assert.ErrorContains(t, err, "no secret at secret/missing")
}

func (f *fakeKVInternal) handler(t *testing.T) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/secret/data/arcane/web", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "setup-token", r.Header.Get("X-Vault-Token"))
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.data == nil {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errors":[]}`))
			return
		}
		body, err := json.Marshal(map[string]any{"data": map[string]any{"data": f.data, "metadata": map[string]int{"version": f.version}}})
		require.NoError(t, err)
		_, _ = w.Write([]byte(body))
	})
	mux.HandleFunc("POST /v1/secret/data/arcane/web", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		defer f.mu.Unlock()
		f.writes = append(f.writes, string(raw))
		_, _ = w.Write([]byte(`{"data":{"version":1}}`))
	})
	return mux
}

func TestSetupMergesWithExistingKeysAndSendsCAS(t *testing.T) {
	kv := &fakeKVInternal{data: map[string]string{"KEEP": "k", "OLD": "o"}, version: 2}
	server := httptest.NewServer(kv.handler(t))
	t.Cleanup(server.Close)

	setup, err := NewSetup(server.Client(), types.SecretSourceVaultSettings{Address: server.URL, AuthMethod: "token", SetupToken: true}, "setup-token")
	require.NoError(t, err)
	target, err := NormalizeSetupTarget(types.SecretSourceSetupTarget{Mode: types.SecretSourceSetupModeKVPath}, "Web")
	require.NoError(t, err)
	assert.Equal(t, "arcane/web", target.SecretPath)

	remote, err := setup.RemoteValues(t.Context(), target)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"KEEP": "k", "OLD": "o"}, remote)

	err = setup.Write(t.Context(), target, map[string]string{"NEW": "n", "OLD": "o2"}, []string{"NEW"}, []string{"OLD"})
	require.NoError(t, err)
	require.Len(t, kv.writes, 1)
	assert.JSONEq(t, `{"data":{"KEEP":"k","NEW":"n","OLD":"o2"},"options":{"cas":2}}`, kv.writes[0])

	err = setup.Write(t.Context(), target, map[string]string{"KEEP": "x"}, []string{"KEEP"}, nil)
	assert.ErrorContains(t, err, "already exists")
}

func TestSetupNeedsSetupToken(t *testing.T) {
	_, err := NewSetup(nil, types.SecretSourceVaultSettings{Address: "http://v:8200"}, "tok")
	assert.ErrorIs(t, err, ErrNoSetupToken)
	_, err = NewSetup(nil, types.SecretSourceVaultSettings{Address: "http://v:8200", SetupToken: true}, "")
	assert.ErrorIs(t, err, ErrNoSetupToken)
}

func TestSuggestPath(t *testing.T) {
	assert.Equal(t, "arcane/my-app", SuggestPath("My App!"))
	assert.Equal(t, "arcane/project", SuggestPath("!!!"))
}

func TestBrowseRefusesSystemMounts(t *testing.T) {
	provider, err := New(nil, types.SecretSourceVaultSettings{Address: "http://127.0.0.1:1", AuthMethod: "token"}, "t")
	require.NoError(t, err)
	_, err = provider.Browse(t.Context(), types.SecretSourceBrowseQuery{Kind: types.SecretSourceBrowseVaultPaths, Mount: "auth", Path: "token"})
	assert.ErrorContains(t, err, "system path")
	_, err = provider.Browse(t.Context(), types.SecretSourceBrowseQuery{Kind: types.SecretSourceBrowseVaultPaths, Mount: "secret", Path: "a/../../sys"})
	assert.ErrorContains(t, err, "invalid path")
}

func TestSetupKeepsNumbersAndHandlesSoftDeletedSecret(t *testing.T) {
	var writes []string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/secret/data/arcane/web", func(w http.ResponseWriter, _ *http.Request) {
		// The latest version is soft-deleted: 404 with metadata only.
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"data":{"data":null,"metadata":{"version":7,"deletion_time":"2026-01-01T00:00:00Z"}}}`))
	})
	mux.HandleFunc("GET /v1/secret/metadata/arcane/web", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"current_version":7}}`))
	})
	mux.HandleFunc("POST /v1/secret/data/arcane/web", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		writes = append(writes, string(raw))
		_, _ = w.Write([]byte(`{"data":{"version":8}}`))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	setup, err := NewSetup(server.Client(), types.SecretSourceVaultSettings{Address: server.URL, SetupToken: true}, "setup-token")
	require.NoError(t, err)
	target, err := NormalizeSetupTarget(types.SecretSourceSetupTarget{Mode: types.SecretSourceSetupModeKVPath}, "web")
	require.NoError(t, err)

	remote, err := setup.RemoteValues(t.Context(), target)
	require.NoError(t, err)
	assert.Empty(t, remote)
	require.NoError(t, setup.Write(t.Context(), target, map[string]string{"A": "1"}, []string{"A"}, nil))
	require.Len(t, writes, 1)
	assert.JSONEq(t, `{"data":{"A":"1"},"options":{"cas":7}}`, writes[0])
}

func TestSetupWritesExistingValuesBackUnchanged(t *testing.T) {
	var written string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/secret/data/arcane/web", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"data":{"BIG":12345678901234567890,"OBJ":{"a":[1,2]}},"metadata":{"version":1}}}`))
	})
	mux.HandleFunc("POST /v1/secret/data/arcane/web", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		written = string(raw)
		_, _ = w.Write([]byte(`{}`))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	setup, err := NewSetup(server.Client(), types.SecretSourceVaultSettings{Address: server.URL, SetupToken: true}, "s")
	require.NoError(t, err)
	target, err := NormalizeSetupTarget(types.SecretSourceSetupTarget{Mode: types.SecretSourceSetupModeKVPath}, "web")
	require.NoError(t, err)
	_, err = setup.RemoteValues(t.Context(), target)
	require.NoError(t, err)
	require.NoError(t, setup.Write(t.Context(), target, map[string]string{"NEW": "n"}, []string{"NEW"}, nil))
	assert.Contains(t, written, `"BIG":12345678901234567890`)
	assert.Contains(t, written, `"OBJ":{"a":[1,2]}`)
}
