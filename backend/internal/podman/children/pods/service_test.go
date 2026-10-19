package pods

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	podmantypes "github.com/getarcaneapp/arcane/types/v2/podman"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/podman"
)

func TestCreateMapsRequestToPodSpec(t *testing.T) {
	tests := []struct {
		name string
		req  podmantypes.PodCreateRequest
		want map[string]any
	}{
		{
			name: "named network with ports",
			req: podmantypes.PodCreateRequest{
				Name:             " web ",
				Hostname:         "web-host",
				Labels:           map[string]string{"tier": "front"},
				Network:          "frontend",
				SharedNamespaces: []string{"net", "ipc"},
				PortMappings: []podmantypes.PodPortMapping{
					{ContainerPort: 80, HostPort: 8080, Protocol: "tcp"},
				},
			},
			want: map[string]any{
				"name":              "web",
				"hostname":          "web-host",
				"labels":            map[string]any{"tier": "front"},
				"shared_namespaces": []any{"net", "ipc"},
				"portmappings":      []any{map[string]any{"container_port": float64(80), "host_port": float64(8080), "protocol": "tcp"}},
				"netns":             map[string]any{"nsmode": "bridge"},
				"Networks":          map[string]any{"frontend": map[string]any{}},
			},
		},
		{
			name: "host network without infra",
			req: podmantypes.PodCreateRequest{
				Name:    "host-pod",
				Network: "host",
				NoInfra: true,
			},
			want: map[string]any{
				"name":     "host-pod",
				"no_infra": true,
				"netns":    map[string]any{"nsmode": "host"},
			},
		},
		{
			name: "advanced fields override typed fields",
			req: podmantypes.PodCreateRequest{
				Name:     "custom",
				Hostname: "typed",
				Advanced: json.RawMessage(`{"hostname":"advanced","dns_server":["1.1.1.1"]}`),
			},
			want: map[string]any{
				"name":       "custom",
				"hostname":   "advanced",
				"dns_server": []any{"1.1.1.1"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/v4.0.0/libpod/pods/create", r.URL.Path)
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&got))
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"Id":"pod-1"}`))
			}))
			t.Cleanup(server.Close)
			client, err := podman.NewClient(server.URL)
			require.NoError(t, err)

			svc := NewService(func(context.Context) (*podman.Client, error) { return client, nil })
			id, err := svc.Create(t.Context(), tt.req)
			require.NoError(t, err)
			assert.Equal(t, "pod-1", id)
			assert.Equal(t, tt.want, got)
		})
	}
}
