package doppler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"getarcane.dev/types/v3"
)

func TestNormalizeTargetPairs(t *testing.T) {
	target, err := NormalizeTarget(nil)
	require.NoError(t, err)
	assert.Equal(t, &types.SecretSourceDopplerTarget{}, target)

	target, err = NormalizeTarget(&types.SecretSourceDopplerTarget{Project: " api ", Config: "prd"})
	require.NoError(t, err)
	assert.Equal(t, &types.SecretSourceDopplerTarget{Project: "api", Config: "prd"}, target)

	_, err = NormalizeTarget(&types.SecretSourceDopplerTarget{Project: "api"})
	assert.Error(t, err)
}

func TestNormalizeSettingsStoresDefaultAsEmpty(t *testing.T) {
	settings, err := NormalizeSettings(&types.SecretSourceDopplerSettings{APIURL: "https://api.doppler.com/"})
	require.NoError(t, err)
	assert.Empty(t, settings.APIURL)

	settings, err = NormalizeSettings(&types.SecretSourceDopplerSettings{APIURL: "http://doppler-mock:8080"})
	require.NoError(t, err)
	assert.Equal(t, "http://doppler-mock:8080", settings.APIURL)
}

func TestTestWithServiceTokenStillPasses(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v3/me", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"name":"arcane-prd","type_":"service_token","workplace":{"name":"Homelab"}}`))
	})
	mux.HandleFunc("GET /v3/projects", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	provider, err := New(server.Client(), types.SecretSourceDopplerSettings{APIURL: server.URL}, "dp.st.prd.x")
	require.NoError(t, err)

	result := provider.Test(t.Context())
	assert.True(t, result.OK)
	assert.False(t, result.CanBrowse)
	assert.Contains(t, result.Message, "arcane-prd in Homelab")
}
