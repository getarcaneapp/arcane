package podman

import (
	"io"
	"net/http"
	"testing"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientKubePlayDownGenerate(t *testing.T) {
	const manifest = "apiVersion: v1\nkind: Pod\n"
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v4.0.0/libpod/play/kube":
			assert.Equal(t, "network=frontend&replace=true&start=false", r.URL.RawQuery)
			data, err := io.ReadAll(r.Body)
			assert.NoError(t, err)
			assert.Equal(t, manifest, string(data))
			_, _ = w.Write([]byte(`{"Pods":[{"ID":"pod123","Containers":["abc"]}],"Volumes":[{"Name":"data"}]}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v4.0.0/libpod/play/kube":
			assert.Equal(t, "force=true", r.URL.RawQuery)
			_, _ = w.Write([]byte(`{"StopReport":[{"Id":"pod123","Errs":[]}],"RmReport":[{"Id":"pod123","RemovedCtrs":{}}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v4.0.0/libpod/generate/kube":
			assert.Equal(t, "names=web&names=db&service=true&type=deployment", r.URL.RawQuery)
			_, _ = w.Write([]byte(manifest))
		default:
			http.NotFound(w, r)
		}
	})

	played, err := client.KubePlay(t.Context(), types.KubePlayRequest{
		YAML:     manifest,
		Start:    new(false),
		Replace:  true,
		Networks: []string{"frontend"},
	})
	require.NoError(t, err)
	require.Len(t, played.Pods, 1)
	assert.Equal(t, "pod123", played.Pods[0].ID)
	assert.Equal(t, "data", played.Volumes[0].Name)

	down, err := client.KubeDown(t.Context(), types.KubeDownRequest{YAML: manifest, Force: true})
	require.NoError(t, err)
	assert.Equal(t, "pod123", down.RmReport[0].ID)

	generated, err := client.KubeGenerate(t.Context(), types.KubeGenerateRequest{
		Names:   []string{"web", "db"},
		Type:    types.KubeGenerateTypeDeployment,
		Service: true,
	})
	require.NoError(t, err)
	assert.Equal(t, manifest, generated)
}
