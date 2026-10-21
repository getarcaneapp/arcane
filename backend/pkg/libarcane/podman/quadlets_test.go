package podman

import (
	"bytes"
	"io"
	"net/http"
	"testing"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientQuadletInstall(t *testing.T) {
	tests := []struct {
		name          string
		appsSupported bool
		query         string
	}{
		{name: "apps supported", appsSupported: true, query: "application=shop&replace=true"},
		{name: "apps unsupported", query: "replace=true"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var archive []byte
			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/v4.0.0/libpod/quadlets", r.URL.Path)
				assert.Equal(t, tt.query, r.URL.RawQuery)
				assert.Equal(t, "application/x-tar", r.Header.Get("Content-Type"))
				data, err := io.ReadAll(r.Body)
				assert.NoError(t, err)
				archive = data
				_, _ = w.Write([]byte(`{"InstalledQuadlets":{"web.kube":"/etc/containers/systemd/web.kube"},"QuadletErrors":{}}`))
			})

			report, err := client.QuadletInstall(t.Context(), QuadletInstallOptions{
				File:          types.QuadletFile{Name: "web.kube", Content: "[Kube]\nYaml=web.yaml\n"},
				ExtraFiles:    []types.QuadletFile{{Name: "web.yaml", Content: "kind: Pod\n"}},
				Replace:       true,
				Application:   "shop",
				AppsSupported: tt.appsSupported,
			})
			require.NoError(t, err)
			assert.Equal(t, "/etc/containers/systemd/web.kube", report.InstalledQuadlets["web.kube"])
			assert.Equal(t, map[string]string{
				"web.kube": "[Kube]\nYaml=web.yaml\n",
				"web.yaml": "kind: Pod\n",
			}, readTar(t, bytes.NewReader(archive)))
		})
	}
}

func TestClientQuadletListFileRemove(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v4.0.0/libpod/quadlets/json":
			assert.JSONEq(t, `{"name":["web*"],"status":["active"]}`, r.URL.Query().Get("filters"))
			_, _ = w.Write([]byte(`[{"Name":"web.container","UnitName":"web.service","Path":"/etc/containers/systemd/web.container","Status":"active","App":"shop"}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/v4.0.0/libpod/quadlets/web.container/file":
			_, _ = w.Write([]byte("[Container]\nImage=nginx\n"))
		case r.Method == http.MethodDelete && r.URL.Path == "/v4.0.0/libpod/quadlets/web.container":
			assert.Equal(t, "force=true&ignore=false", r.URL.RawQuery)
			_, _ = w.Write([]byte(`{"Removed":["web.container"],"Errors":{}}`))
		default:
			http.NotFound(w, r)
		}
	})

	quadlets, err := client.QuadletList(t.Context(), map[string][]string{
		"name":   {"web*"},
		"pod":    nil,
		"status": {"active"},
	})
	require.NoError(t, err)
	require.Len(t, quadlets, 1)
	assert.Equal(t, types.Quadlet{
		Name:     "web.container",
		UnitName: "web.service",
		Path:     "/etc/containers/systemd/web.container",
		Status:   "active",
		App:      "shop",
	}, quadlets[0])

	content, err := client.QuadletFile(t.Context(), "web.container")
	require.NoError(t, err)
	assert.Equal(t, "[Container]\nImage=nginx\n", content)

	report, err := client.QuadletRemove(t.Context(), "web.container", QuadletRemoveOptions{Force: true})
	require.NoError(t, err)
	assert.Equal(t, []string{"web.container"}, report.Removed)
}
