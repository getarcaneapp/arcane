package docker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	containertypes "github.com/moby/moby/api/types/container"
	networktypes "github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

func TestSelectAutoNetworkMode(t *testing.T) {
	t.Run("prefers explicit host network mode", func(t *testing.T) {
		mode := SelectAutoNetworkMode(&containertypes.InspectResponse{
			HostConfig: &containertypes.HostConfig{NetworkMode: "host"},
		})

		require.Equal(t, "host", mode)
	})

	t.Run("prefers attached custom network over bridge", func(t *testing.T) {
		mode := SelectAutoNetworkMode(&containertypes.InspectResponse{
			HostConfig: &containertypes.HostConfig{NetworkMode: "bridge"},
			NetworkSettings: &containertypes.NetworkSettings{
				Networks: map[string]*networktypes.EndpointSettings{
					"bridge":          {},
					"arcane-internal": {},
				},
			},
		})

		require.Equal(t, "arcane-internal", mode)
	})

	t.Run("falls back to bridge when no custom network is attached", func(t *testing.T) {
		mode := SelectAutoNetworkMode(&containertypes.InspectResponse{
			HostConfig: &containertypes.HostConfig{NetworkMode: "bridge"},
			NetworkSettings: &containertypes.NetworkSettings{
				Networks: map[string]*networktypes.EndpointSettings{
					"bridge": {},
				},
			},
		})

		require.Equal(t, "bridge", mode)
	})
}

func TestSelectDockerHostReachableNetworkMode(t *testing.T) {
	// Server container on an external tunnel network plus a compose-prefixed
	// socket-proxy network. Plain alphabetical selection picks the tunnel (#3533).
	inspect := &containertypes.InspectResponse{
		NetworkSettings: &containertypes.NetworkSettings{
			Networks: map[string]*networktypes.EndpointSettings{
				"cloudflare-tunnel":       {},
				"myproj_socket-proxy-net": {},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/containers/json") {
			_, _ = w.Write([]byte(`[{"Id":"proxy1","Names":["/myproj-socket-proxy-1"],"Labels":{"com.docker.compose.service":"socket-proxy"},"State":"running","NetworkSettings":{"Networks":{"myproj_socket-proxy-net":{"Aliases":["socket-proxy"]}}}}]`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	dockerClient, err := client.New(
		client.WithHost("tcp://"+strings.TrimPrefix(server.URL, "http://")),
		client.WithAPIVersion("1.41"),
	)
	require.NoError(t, err)
	defer func() { _ = dockerClient.Close() }()

	t.Run("prefers shared network with the DOCKER_HOST container", func(t *testing.T) {
		got := SelectDockerHostReachableNetworkMode(context.Background(), dockerClient, inspect, "tcp://socket-proxy:2375")
		require.Equal(t, "myproj_socket-proxy-net", got)
	})

	t.Run("falls back to auto heuristic when nothing matches", func(t *testing.T) {
		got := SelectDockerHostReachableNetworkMode(context.Background(), dockerClient, inspect, "tcp://unknown-host:2375")
		require.Equal(t, "cloudflare-tunnel", got)
	})
}
