package podman

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientPodCreate(t *testing.T) {
	var body map[string]any
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v4.0.0/libpod/pods/create", r.URL.Path)
		data, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		assert.NoError(t, json.Unmarshal(data, &body))
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"Id":"pod123"}`))
	})

	id, err := client.PodCreate(t.Context(), PodSpec{
		Name:     "web",
		Hostname: "web-host",
		PortMappings: []PodPortMapping{
			{ContainerPort: 80, HostPort: 8080},
		},
		Networks:  map[string]PodNetworkOptions{"frontend": {}},
		InfraName: "web-infra",
	})
	require.NoError(t, err)
	assert.Equal(t, "pod123", id)
	assert.Equal(t, "web", body["name"])
	assert.Equal(t, "web-host", body["hostname"])
	assert.Equal(t, "web-infra", body["infra_name"])
	assert.Contains(t, body, "Networks")
	assert.Equal(t, []any{map[string]any{"container_port": float64(80), "host_port": float64(8080)}}, body["portmappings"])
}

func TestClientPodAction(t *testing.T) {
	tests := []struct {
		name   string
		action PodAction
		opts   PodActionOptions
		query  string
	}{
		{name: "start", action: PodActionStart},
		{name: "stop with timeout", action: PodActionStop, opts: PodActionOptions{Timeout: new(5)}, query: "t=5"},
		{name: "kill with signal", action: PodActionKill, opts: PodActionOptions{Signal: "SIGTERM"}, query: "signal=SIGTERM"},
		{name: "restart ignores signal", action: PodActionRestart, opts: PodActionOptions{Signal: "SIGTERM"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/v4.0.0/libpod/pods/web/"+string(tt.action), r.URL.Path)
				assert.Equal(t, tt.query, r.URL.RawQuery)
				_, _ = w.Write([]byte(`{"Id":"pod123","Errs":[]}`))
			})

			require.NoError(t, client.PodAction(t.Context(), "web", tt.action, tt.opts))
		})
	}
}

func TestClientPodStats(t *testing.T) {
	tests := []struct {
		name  string
		names []string
		query string
	}{
		{name: "all pods", query: "all=true&stream=false"},
		{name: "named pods", names: []string{"web", "db"}, query: "namesOrIDs=web&namesOrIDs=db&stream=false"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/v4.0.0/libpod/pods/stats", r.URL.Path)
				assert.Equal(t, tt.query, r.URL.RawQuery)
				_, _ = w.Write([]byte(`[{"Pod":"pod123","CID":"abc","Name":"web-infra","CPU":"0.10%","MemUsage":"1MB / 2GB","NetIO":"1kB / 2kB","PIDS":"1"}]`))
			})

			stats, err := client.PodStats(t.Context(), tt.names)
			require.NoError(t, err)
			require.Len(t, stats, 1)
			assert.Equal(t, "pod123", stats[0].Pod)
			assert.Equal(t, "1kB / 2kB", stats[0].NetIO)
		})
	}
}

func TestClientPodListAndInspect(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v4.0.0/libpod/pods/json":
			assert.JSONEq(t, `{"label":["app=web"]}`, r.URL.Query().Get("filters"))
			_, _ = w.Write([]byte(`[{"Id":"pod123","Name":"web","Status":"Running","InfraId":"infra1","Containers":[{"Id":"abc","Names":"web-app","Status":"running"}]}]`))
		case "/v4.0.0/libpod/pods/web/json":
			_, _ = w.Write([]byte(`{
				"Id":"pod123","Name":"web","State":"Running","InfraContainerID":"infra1","NumContainers":2,
				"SharedNamespaces":["net","ipc"],"Containers":[{"Id":"abc","Name":"web-app","State":"running"}]
			}`))
		default:
			http.NotFound(w, r)
		}
	})

	pods, err := client.PodList(t.Context(), map[string][]string{"label": {"app=web"}})
	require.NoError(t, err)
	require.Len(t, pods, 1)
	assert.Equal(t, "infra1", pods[0].InfraID)
	require.Len(t, pods[0].Containers, 1)
	assert.Equal(t, "web-app", pods[0].Containers[0].Names)

	pod, err := client.PodInspect(t.Context(), "web")
	require.NoError(t, err)
	assert.Equal(t, "infra1", pod.InfraContainerID)
	assert.Equal(t, uint(2), pod.NumContainers)
	assert.Equal(t, []string{"net", "ipc"}, pod.SharedNamespaces)
}
