package podman

import (
	"net/http"
	"testing"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientAutoUpdate(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v4.0.0/libpod/autoupdate", r.URL.Path)
		query := r.URL.Query()
		assert.Equal(t, "true", query.Get("dryRun"))
		assert.Equal(t, "false", query.Get("rollback"))
		assert.Empty(t, query.Get("tlsVerify"))
		assert.Equal(t, "/run/auth.json", query.Get("authfile"))
		assert.JSONEq(t, `{"label":["app=web"]}`, query.Get("filters"))
		_, _ = w.Write([]byte(`{
			"Reports":[{"ContainerID":"abc","ContainerName":"web","ImageName":"nginx:latest","Policy":"registry","SystemdUnit":"web.service","Updated":"pending"}],
			"Errors":["db: image not found"]
		}`))
	})

	result, err := client.AutoUpdate(t.Context(), types.PodmanAutoUpdateOptions{
		DryRun:   true,
		Rollback: new(false),
		Authfile: "/run/auth.json",
		Filters:  map[string][]string{"label": {"app=web"}},
	})
	require.NoError(t, err)
	assert.Equal(t, types.PodmanAutoUpdateResult{
		Mode: types.PodmanAutoUpdateModeREST,
		Reports: []types.PodmanAutoUpdateReport{
			{
				ContainerID:   "abc",
				ContainerName: "web",
				ImageName:     "nginx:latest",
				Policy:        "registry",
				SystemdUnit:   "web.service",
				Updated:       "pending",
			},
		},
		Errors: []string{"db: image not found"},
	}, result)
}
