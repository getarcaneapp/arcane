package doppler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestClientInternal(t *testing.T, mux *http.ServeMux) *Client {
	t.Helper()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	client, err := NewClient(server.Client(), server.URL, "dp.st.prd.abc")
	require.NoError(t, err)
	return client
}

func TestDownloadStripsDopplerMetaKeys(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v3/configs/config/secrets/download", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer dp.st.prd.abc", r.Header.Get("Authorization"))
		assert.Equal(t, "json", r.URL.Query().Get("format"))
		assert.Empty(t, r.URL.Query().Get("project"))
		_, _ = w.Write([]byte(`{"API_KEY":"k","DOPPLER_PROJECT":"api","DOPPLER_CONFIG":"prd","DOPPLER_ENVIRONMENT":"prd"}`))
	})
	client := newTestClientInternal(t, mux)

	secrets, err := client.Download(t.Context(), "", "")
	require.NoError(t, err)
	require.Len(t, secrets, 1)
	assert.JSONEq(t, `"k"`, string(secrets["API_KEY"]))
}

func TestDownloadWithProjectAndConfig(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v3/configs/config/secrets/download", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "api", r.URL.Query().Get("project"))
		assert.Equal(t, "dev", r.URL.Query().Get("config"))
		_, _ = w.Write([]byte(`{}`))
	})
	client := newTestClientInternal(t, mux)
	_, err := client.Download(t.Context(), "api", "dev")
	require.NoError(t, err)
}

func TestMeProjectsConfigs(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v3/me", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"name":"arcane","type_":"personal","workplace":{"name":"Homelab"}}`))
	})
	mux.HandleFunc("GET /v3/projects", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"projects":[{"slug":"api","name":"API"}]}`))
	})
	mux.HandleFunc("GET /v3/configs", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "api", r.URL.Query().Get("project"))
		_, _ = w.Write([]byte(`{"configs":[{"name":"dev","environment":"dev"}]}`))
	})
	client := newTestClientInternal(t, mux)

	me, err := client.Me(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "personal", me.Type)
	assert.Equal(t, "Homelab", me.Workplace.Name)

	projects, err := client.Projects(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []Project{{Slug: "api", Name: "API"}}, projects)

	configs, err := client.Configs(t.Context(), "api")
	require.NoError(t, err)
	assert.Equal(t, []Config{{Name: "dev", Environment: "dev"}}, configs)
}

func TestServiceTokenCannotListProjects(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v3/projects", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"messages":["This token does not have access to requested resource"],"success":false}`))
	})
	client := newTestClientInternal(t, mux)
	_, err := client.Projects(t.Context())
	assert.ErrorContains(t, err, "Doppler returned HTTP 403: This token does not have access")
}

func TestParseAPIURLDefaults(t *testing.T) {
	parsed, err := ParseAPIURL("")
	require.NoError(t, err)
	assert.Equal(t, DefaultAPIURL, parsed.String())
	_, err = NewClient(nil, "", " ")
	assert.Error(t, err)
}
