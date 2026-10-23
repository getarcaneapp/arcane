package podman

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := NewClient(server.URL)
	require.NoError(t, err)
	return client
}

func TestNewClient(t *testing.T) {
	tests := []struct {
		name    string
		host    string
		baseURL string
		wantErr bool
	}{
		{name: "unix", host: "unix:///run/podman/podman.sock", baseURL: "http://podman"},
		{name: "tcp", host: "tcp://10.0.0.5:8888", baseURL: "http://10.0.0.5:8888"},
		{name: "https", host: "https://podman.example:8443", baseURL: "https://podman.example:8443"},
		{name: "unsupported", host: "ssh://user@host", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient(tt.host)
			if tt.wantErr {
				require.ErrorIs(t, err, common.ErrBadRequest)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.baseURL, client.baseURL)
		})
	}
}

func TestDetectFeatures(t *testing.T) {
	tests := []struct {
		name             string
		version          string
		systemdAvailable bool
		want             Features
	}{
		{
			name:             "podman 4 with systemd",
			version:          "4.9.4",
			systemdAvailable: true,
			want:             Features{AutoUpdateMode: types.PodmanAutoUpdateModeSystemd},
		},
		{
			name:    "podman 5.7 without systemd",
			version: "5.7.1",
			want:    Features{AutoUpdateMode: types.PodmanAutoUpdateModeUnavailable},
		},
		{
			name:             "podman 5.8",
			version:          "5.8.0",
			systemdAvailable: true,
			want: Features{
				QuadletsSupported: true,
				AutoUpdateMode:    types.PodmanAutoUpdateModeSystemd,
			},
		},
		{
			name:    "podman 6.1 with v prefix",
			version: "v6.1.2",
			want: Features{
				QuadletsSupported:    true,
				QuadletAppsSupported: true,
				AutoUpdateMode:       types.PodmanAutoUpdateModeUnavailable,
			},
		},
		{
			name:    "podman 6.2 uses rest",
			version: "6.2.0",
			want: Features{
				QuadletsSupported:    true,
				QuadletAppsSupported: true,
				AutoUpdateMode:       types.PodmanAutoUpdateModeREST,
			},
		},
		{
			name:             "invalid version",
			version:          "unknown",
			systemdAvailable: true,
			want:             Features{AutoUpdateMode: types.PodmanAutoUpdateModeSystemd},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, DetectFeatures(tt.version, tt.systemdAvailable))
		})
	}
}

func TestClientVersionAndInfo(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v4.0.0/libpod/version":
			_, _ = w.Write([]byte(`{"Version":"5.8.1","ApiVersion":"1.41","Components":[{"Name":"Podman Engine","Version":"5.8.1"}]}`))
		case "/v4.0.0/libpod/info":
			_, _ = w.Write([]byte(`{"host":{"hostname":"box","security":{"rootless":true}},"store":{"containerStore":{"number":3}},"version":{"APIVersion":"5.8.1","Version":"5.8.1"}}`))
		default:
			http.NotFound(w, r)
		}
	})

	version, err := client.Version(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "5.8.1", version.Version)
	assert.Equal(t, "1.41", version.APIVersion)
	require.Len(t, version.Components, 1)
	assert.Equal(t, "Podman Engine", version.Components[0].Name)

	info, err := client.Info(t.Context())
	require.NoError(t, err)
	assert.True(t, info.Host.Security.Rootless)
	assert.Equal(t, "box", info.Host.Hostname)
	assert.Equal(t, 3, info.Store.ContainerStore.Number)
	assert.Equal(t, "5.8.1", info.Version.APIVersion)
}

func TestClientContainerList(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v4.0.0/libpod/containers/json", r.URL.Path)
		assert.Equal(t, "true", r.URL.Query().Get("all"))
		assert.JSONEq(t, `{"label":["PODMAN_SYSTEMD_UNIT=web.service"]}`, r.URL.Query().Get("filters"))
		_, _ = w.Write([]byte(`[{"Id":"abc","Names":["web"],"Labels":{"PODMAN_SYSTEMD_UNIT":"web.service"},"PodName":"","IsInfra":false,` +
			`"Ports":[{"host_ip":"127.0.0.1","container_port":80,"host_port":8080,"range":1,"protocol":"tcp"}]}]`))
	})

	containers, err := client.ContainerList(t.Context(), []string{types.PodmanSystemdUnitLabel + "=web.service"}, nil)
	require.NoError(t, err)
	require.Len(t, containers, 1)
	assert.Equal(t, "abc", containers[0].ID)
	assert.Equal(t, "web.service", containers[0].Labels[types.PodmanSystemdUnitLabel])
	assert.Equal(t, []types.PodPortMapping{
		{
			ContainerPort: 80,
			HostPort:      8080,
			HostIP:        "127.0.0.1",
			Protocol:      "tcp",
			Range:         1,
		},
	}, containers[0].Ports)
}

func TestClientErrorClassification(t *testing.T) {
	tests := []struct {
		name   string
		status int
		kind   error
	}{
		{name: "not found", status: http.StatusNotFound, kind: common.ErrNotFound},
		{name: "conflict", status: http.StatusConflict, kind: common.ErrConflict},
		{name: "bad request", status: http.StatusBadRequest, kind: common.ErrBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(`{"cause":"no such pod","message":"pod web: no such pod","response":0}`))
			})

			_, err := client.PodInspect(t.Context(), "web")
			require.ErrorIs(t, err, tt.kind)
			assert.Contains(t, err.Error(), "pod web: no such pod")
		})
	}
}
