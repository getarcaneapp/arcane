package libarcane

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	containertypes "github.com/moby/moby/api/types/container"
	networktypes "github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeContainerInspectRawJSONInternal(t *testing.T) {
	raw := []byte(`{
		"Id":"abc123",
		"Name":"/app",
		"Config":{"Image":"test:latest"},
		"HostConfig":{"NetworkMode":"bridge"},
		"NetworkSettings":{
			"Networks":{
				"bridge":{
					"IPAMConfig":{
						"IPv4Address":"172.18.0.50/16",
						"IPv6Address":"fdd0:0:0:c::10/64",
						"LinkLocalIPs":["169.254.10.10/16","fe80::10/64"]
					},
					"Gateway":"172.18.0.1/16",
					"IPAddress":"172.18.0.20/16",
					"IPPrefixLen":0,
					"IPv6Gateway":"fdd0:0:0:c::1/64",
					"GlobalIPv6Address":"fdd0:0:0:c::10/64",
					"GlobalIPv6PrefixLen":0
				}
			}
		}
	}`)

	normalized, changed, err := normalizeContainerInspectRawJSONInternal(raw)
	require.NoError(t, err)
	require.True(t, changed)

	var inspect containertypes.InspectResponse
	require.NoError(t, json.Unmarshal(normalized, &inspect))

	endpoint := inspect.NetworkSettings.Networks["bridge"]
	require.NotNil(t, endpoint)
	require.NotNil(t, endpoint.IPAMConfig)
	assert.Equal(t, netip.MustParseAddr("172.18.0.1"), endpoint.Gateway)
	assert.Equal(t, netip.MustParseAddr("172.18.0.20"), endpoint.IPAddress)
	assert.Equal(t, 16, endpoint.IPPrefixLen)
	assert.Equal(t, netip.MustParseAddr("fdd0:0:0:c::1"), endpoint.IPv6Gateway)
	assert.Equal(t, netip.MustParseAddr("fdd0:0:0:c::10"), endpoint.GlobalIPv6Address)
	assert.Equal(t, 64, endpoint.GlobalIPv6PrefixLen)
	assert.Equal(t, netip.MustParseAddr("172.18.0.50"), endpoint.IPAMConfig.IPv4Address)
	assert.Equal(t, netip.MustParseAddr("fdd0:0:0:c::10"), endpoint.IPAMConfig.IPv6Address)
	assert.Equal(t, []netip.Addr{
		netip.MustParseAddr("169.254.10.10"),
		netip.MustParseAddr("fe80::10"),
	}, endpoint.IPAMConfig.LinkLocalIPs)
}

func TestNormalizeContainerInspectRawJSONInternal_NormalizesTopLevelNetworkSettings(t *testing.T) {
	raw := []byte(`{
		"Id":"abc123",
		"Name":"/app",
		"Config":{"Image":"test:latest"},
		"HostConfig":{"NetworkMode":"bridge"},
		"NetworkSettings":{
			"Gateway":"172.18.0.1/16",
			"IPAddress":"172.18.0.20/16",
			"IPPrefixLen":0,
			"IPv6Gateway":"fdd0:0:0:c::1/64",
			"GlobalIPv6Address":"fdd0:0:0:c::10/64",
			"GlobalIPv6PrefixLen":0,
			"Networks":{}
		}
	}`)

	normalized, changed, err := normalizeContainerInspectRawJSONInternal(raw)
	require.NoError(t, err)
	require.True(t, changed)

	payload := map[string]any{}
	require.NoError(t, json.Unmarshal(normalized, &payload))

	networkSettings, ok := asMapInternal(payload["NetworkSettings"])
	require.True(t, ok)
	assert.Equal(t, "172.18.0.1", networkSettings["Gateway"])
	assert.Equal(t, "172.18.0.20", networkSettings["IPAddress"])
	assert.Equal(t, float64(16), networkSettings["IPPrefixLen"])
	assert.Equal(t, "fdd0:0:0:c::1", networkSettings["IPv6Gateway"])
	assert.Equal(t, "fdd0:0:0:c::10", networkSettings["GlobalIPv6Address"])
	assert.Equal(t, float64(64), networkSettings["GlobalIPv6PrefixLen"])
}

func TestNormalizeNetworkInspectRawJSONInternal(t *testing.T) {
	raw := []byte(`{
		"Name":"test-net",
		"Id":"net123",
		"Created":"2026-03-11T00:00:00Z",
		"Scope":"local",
		"Driver":"bridge",
		"EnableIPv6":true,
		"IPAM":{
			"Driver":"default",
			"Config":[
				{
					"Subnet":"fdd0:0:0:c::/64",
					"Gateway":"fdd0:0:0:c::1/64",
					"AuxiliaryAddresses":{
						"router":"fdd0:0:0:c::2/64"
					}
				}
			]
		},
		"Containers":{}
	}`)

	normalized, changed, err := normalizeNetworkInspectRawJSONInternal(raw)
	require.NoError(t, err)
	require.True(t, changed)

	var inspect networktypes.Inspect
	require.NoError(t, json.Unmarshal(normalized, &inspect))
	require.Len(t, inspect.IPAM.Config, 1)
	assert.Equal(t, netip.MustParsePrefix("fdd0:0:0:c::/64"), inspect.IPAM.Config[0].Subnet)
	assert.Equal(t, netip.MustParseAddr("fdd0:0:0:c::1"), inspect.IPAM.Config[0].Gateway)
	assert.Equal(t, netip.MustParseAddr("fdd0:0:0:c::2"), inspect.IPAM.Config[0].AuxAddress["router"])
}

func TestNormalizeNetworkInspectRawJSONInternal_NormalizesContainerEndpoints(t *testing.T) {
	raw := []byte(`{
		"Name":"test-net",
		"Id":"net123",
		"Created":"2026-03-11T00:00:00Z",
		"Scope":"local",
		"Driver":"bridge",
		"IPAM":{"Driver":"default","Config":[]},
		"Containers":{
			"abc123":{
				"Name":"app",
				"EndpointID":"ep1",
				"IPv4Address":" 172.18.0.20/16 ",
				"IPv6Address":" fdd0:0:0:c::10/64 "
			}
		}
	}`)

	normalized, changed, err := normalizeNetworkInspectRawJSONInternal(raw)
	require.NoError(t, err)
	require.True(t, changed)

	var inspect networktypes.Inspect
	require.NoError(t, json.Unmarshal(normalized, &inspect))
	endpoint := inspect.Containers["abc123"]
	assert.Equal(t, netip.MustParsePrefix("172.18.0.20/16"), endpoint.IPv4Address)
	assert.Equal(t, netip.MustParsePrefix("fdd0:0:0:c::10/64"), endpoint.IPv6Address)
}

func TestNormalizeNetworkListRawJSONInternal(t *testing.T) {
	raw := []byte(`[
		{
			"Name":"test-net",
			"Id":"net123",
			"Created":"2026-03-11T00:00:00Z",
			"Scope":"local",
			"Driver":"bridge",
			"IPAM":{
				"Driver":"default",
				"Config":[
					{
						"Subnet":"fdd0:0:0:c::/64",
						"Gateway":"fdd0:0:0:c::1/64",
						"AuxiliaryAddresses":{
							"router":"fdd0:0:0:c::2/64"
						}
					}
				]
			}
		}
	]`)

	normalized, changed, err := normalizeNetworkListRawJSONInternal(raw)
	require.NoError(t, err)
	require.True(t, changed)

	var networks []networktypes.Summary
	require.NoError(t, json.Unmarshal(normalized, &networks))
	require.Len(t, networks, 1)
	require.Len(t, networks[0].IPAM.Config, 1)
	assert.Equal(t, netip.MustParseAddr("fdd0:0:0:c::1"), networks[0].IPAM.Config[0].Gateway)
	assert.Equal(t, netip.MustParseAddr("fdd0:0:0:c::2"), networks[0].IPAM.Config[0].AuxAddress["router"])
}

func TestNormalizeAddressStringInternal_TrimsWhitespaceAroundValidAddress(t *testing.T) {
	normalized, changed := normalizeAddressStringInternal(" 172.18.0.1 ")
	assert.True(t, changed)
	assert.Equal(t, "172.18.0.1", normalized)
}

func TestNormalizeAddressWithPrefixFieldInternal_TrimsWhitespaceAroundValidAddress(t *testing.T) {
	obj := map[string]any{
		"IPAddress":   " 172.18.0.20 ",
		"IPPrefixLen": 24,
	}

	changed := normalizeAddressWithPrefixFieldInternal(obj, "IPAddress", "IPPrefixLen")
	assert.True(t, changed)
	assert.Equal(t, "172.18.0.20", obj["IPAddress"])
	assert.Equal(t, 24, obj["IPPrefixLen"])
}

func TestIsInspectAddressParseErrorInternal_MatchesPrefixErrors(t *testing.T) {
	_, err := netip.ParsePrefix(" 172.18.0.20/16 ")
	assert.True(t, isInspectAddressParseErrorInternal(err))
}

func TestContainerInspectWithCompatibility(t *testing.T) {
	containerJSON := `{
		"Id":"abc123",
		"Name":"/app",
		"Config":{"Image":"test:latest"},
		"HostConfig":{"NetworkMode":"bridge"},
		"NetworkSettings":{
			"Networks":{
				"bridge":{
					"IPAMConfig":{"IPv6Address":"fdd0:0:0:c::10/64"},
					"IPv6Gateway":"fdd0:0:0:c::1/64",
					"GlobalIPv6Address":"fdd0:0:0:c::10/64",
					"GlobalIPv6PrefixLen":0
				}
			}
		}
	}`

	dockerClient := newTestDockerClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1.41/containers/test-container/json", r.URL.Path)
		_, _ = w.Write([]byte(containerJSON))
	})

	result, err := ContainerInspectWithCompatibility(context.Background(), dockerClient, "test-container", client.ContainerInspectOptions{})
	require.NoError(t, err)
	assert.Equal(t, "abc123", result.Container.ID)
	endpoint := result.Container.NetworkSettings.Networks["bridge"]
	require.NotNil(t, endpoint)
	assert.Equal(t, netip.MustParseAddr("fdd0:0:0:c::1"), endpoint.IPv6Gateway)
	assert.Equal(t, netip.MustParseAddr("fdd0:0:0:c::10"), endpoint.GlobalIPv6Address)
	assert.Equal(t, 64, endpoint.GlobalIPv6PrefixLen)
}

func TestNetworkInspectWithCompatibility(t *testing.T) {
	networkJSON := `{
		"Name":"test-net",
		"Id":"net123",
		"Created":"2026-03-11T00:00:00Z",
		"Scope":"local",
		"Driver":"bridge",
		"IPAM":{
			"Driver":"default",
			"Config":[
				{
					"Subnet":"fdd0:0:0:c::/64",
					"Gateway":"fdd0:0:0:c::1/64"
				}
			]
		},
		"Containers":{}
	}`

	dockerClient := newTestDockerClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1.41/networks/test-net", r.URL.Path)
		_, _ = w.Write([]byte(networkJSON))
	})

	result, err := NetworkInspectWithCompatibility(context.Background(), dockerClient, "test-net", client.NetworkInspectOptions{})
	require.NoError(t, err)
	require.Len(t, result.Network.IPAM.Config, 1)
	assert.Equal(t, netip.MustParseAddr("fdd0:0:0:c::1"), result.Network.IPAM.Config[0].Gateway)
}

func TestNetworkListWithCompatibility(t *testing.T) {
	networkJSON := `[
		{
			"Name":"test-net",
			"Id":"net123",
			"Created":"2026-03-11T00:00:00Z",
			"Scope":"local",
			"Driver":"bridge",
			"IPAM":{
				"Driver":"default",
				"Config":[
					{
						"Subnet":"fdd0:0:0:c::/64",
						"Gateway":"fdd0:0:0:c::1/64",
						"AuxiliaryAddresses":{
							"router":"fdd0:0:0:c::2/64"
						}
					}
				]
			}
		}
	]`

	callCount := 0
	dockerClient := newTestDockerClient(t, func(w http.ResponseWriter, r *http.Request) {
		callCount++
		assert.Equal(t, "/v1.41/networks", r.URL.Path)
		assert.Equal(t, `{"name":{"test-net":true}}`, r.URL.Query().Get("filters"))
		_, _ = w.Write([]byte(networkJSON))
	})

	result, err := NetworkListWithCompatibility(context.Background(), dockerClient, client.NetworkListOptions{
		Filters: make(client.Filters).Add("name", "test-net"),
	})
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	require.Len(t, result.Items[0].IPAM.Config, 1)
	assert.Equal(t, netip.MustParseAddr("fdd0:0:0:c::1"), result.Items[0].IPAM.Config[0].Gateway)
	assert.Equal(t, netip.MustParseAddr("fdd0:0:0:c::2"), result.Items[0].IPAM.Config[0].AuxAddress["router"])
	assert.Equal(t, 2, callCount)
}

func TestNetworkListWithCompatibility_SuccessSkipsFallback(t *testing.T) {
	networkJSON := `[
		{
			"Name":"test-net",
			"Id":"net123",
			"Created":"2026-03-11T00:00:00Z",
			"Scope":"local",
			"Driver":"bridge",
			"IPAM":{
				"Driver":"default",
				"Config":[
					{
						"Subnet":"fdd0:0:0:c::/64",
						"Gateway":"fdd0:0:0:c::1"
					}
				]
			}
		}
	]`

	callCount := 0
	dockerClient := newTestDockerClient(t, func(w http.ResponseWriter, r *http.Request) {
		callCount++
		assert.Equal(t, "/v1.41/networks", r.URL.Path)
		_, _ = w.Write([]byte(networkJSON))
	})

	result, err := NetworkListWithCompatibility(context.Background(), dockerClient, client.NetworkListOptions{})
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	assert.Equal(t, netip.MustParseAddr("fdd0:0:0:c::1"), result.Items[0].IPAM.Config[0].Gateway)
	assert.Equal(t, 1, callCount)
}

func TestContainerInspectWithCompatibility_TLSRemote(t *testing.T) {
	containerJSON := `{
		"Id":"abc123",
		"Name":"/app",
		"Config":{"Image":"test:latest"},
		"HostConfig":{"NetworkMode":"bridge"},
		"NetworkSettings":{
			"Networks":{
				"bridge":{
					"IPv6Gateway":"fdd0:0:0:c::1/64"
				}
			}
		}
	}`

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1.41/containers/test-container/json", r.URL.Path)
		_, _ = w.Write([]byte(containerJSON))
	}))
	defer server.Close()

	dockerClient, err := client.New(
		client.WithHTTPClient(server.Client()),
		client.WithHost("tcp://"+strings.TrimPrefix(server.URL, "https://")),
		client.WithScheme("https"),
		client.WithAPIVersion("1.41"),
	)
	require.NoError(t, err)
	defer func() {
		_ = dockerClient.Close()
	}()

	result, err := ContainerInspectWithCompatibility(context.Background(), dockerClient, "test-container", client.ContainerInspectOptions{})
	require.NoError(t, err)
	assert.Equal(t, netip.MustParseAddr("fdd0:0:0:c::1"), result.Container.NetworkSettings.Networks["bridge"].IPv6Gateway)
}

func TestInspectCompatibilityLeavesInvalidValuesFailing(t *testing.T) {
	dockerClient := newTestDockerClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"Name":"test-net",
			"Id":"net123",
			"Created":"2026-03-11T00:00:00Z",
			"Scope":"local",
			"Driver":"bridge",
			"IPAM":{"Driver":"default","Config":[{"Subnet":"fdd0:0:0:c::/64","Gateway":"definitely-not-an-ip"}]},
			"Containers":{}
		}`))
	})

	_, err := NetworkInspectWithCompatibility(context.Background(), dockerClient, "test-net", client.NetworkInspectOptions{})
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), `ParseAddr("definitely-not-an-ip")`))
}

func TestNetworkListWithCompatibility_LeavesInvalidValuesFailing(t *testing.T) {
	callCount := 0
	dockerClient := newTestDockerClient(t, func(w http.ResponseWriter, r *http.Request) {
		callCount++
		assert.Equal(t, "/v1.41/networks", r.URL.Path)
		_, _ = w.Write([]byte(`[
			{
				"Name":"test-net",
				"Id":"net123",
				"Created":"2026-03-11T00:00:00Z",
				"Scope":"local",
				"Driver":"bridge",
				"IPAM":{"Driver":"default","Config":[{"Subnet":"fdd0:0:0:c::/64","Gateway":"definitely-not-an-ip"}]}
			}
		]`))
	})

	_, err := NetworkListWithCompatibility(context.Background(), dockerClient, client.NetworkListOptions{})
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), `ParseAddr("definitely-not-an-ip")`))
	assert.Equal(t, 2, callCount)
}

func TestWrapDockerAPIClientForInspectCompatibility(t *testing.T) {
	dockerClient := newTestDockerClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"Id":"abc123",
			"Name":"/app",
			"Config":{"Image":"test:latest"},
			"HostConfig":{"NetworkMode":"bridge"},
			"NetworkSettings":{"Networks":{"bridge":{"IPv6Gateway":"fdd0:0:0:c::1/64"}}}
		}`))
	})

	wrapped := WrapDockerAPIClientForInspectCompatibility(dockerClient)
	result, err := wrapped.ContainerInspect(context.Background(), "test-container", client.ContainerInspectOptions{})
	require.NoError(t, err)
	assert.Equal(t, netip.MustParseAddr("fdd0:0:0:c::1"), result.Container.NetworkSettings.Networks["bridge"].IPv6Gateway)
}

func TestWrapDockerAPIClientForInspectCompatibility_NetworkList(t *testing.T) {
	callCount := 0
	dockerClient := newTestDockerClient(t, func(w http.ResponseWriter, r *http.Request) {
		callCount++
		assert.Equal(t, "/v1.41/networks", r.URL.Path)
		_, _ = w.Write([]byte(`[
			{
				"Name":"test-net",
				"Id":"net123",
				"Created":"2026-03-11T00:00:00Z",
				"Scope":"local",
				"Driver":"bridge",
				"IPAM":{"Driver":"default","Config":[{"Subnet":"fdd0:0:0:c::/64","Gateway":"fdd0:0:0:c::1/64"}]}
			}
		]`))
	})

	wrapped := WrapDockerAPIClientForInspectCompatibility(dockerClient)
	result, err := wrapped.NetworkList(context.Background(), client.NetworkListOptions{})
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	assert.Equal(t, netip.MustParseAddr("fdd0:0:0:c::1"), result.Items[0].IPAM.Config[0].Gateway)
	assert.Equal(t, 2, callCount)
}

func TestWrapDockerAPIClientForInspectCompatibility_ContainerCreateLegacyNetworks(t *testing.T) {
	type createRequest struct {
		NetworkingConfig networktypes.NetworkingConfig `json:"NetworkingConfig"`
	}
	type connectRequest struct {
		Container      string                         `json:"Container"`
		EndpointConfig *networktypes.EndpointSettings `json:"EndpointConfig"`
	}

	var createPayload createRequest
	connectPayloads := map[string]connectRequest{}

	dockerClient := newTestDockerClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/create"):
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(body, &createPayload))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Id":"new-container-id","Warnings":[]}`))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/networks/") && strings.HasSuffix(r.URL.Path, "/connect"):
			networkName := r.URL.Path
			networkName = strings.TrimSuffix(networkName, "/connect")
			networkName = networkName[strings.LastIndex(networkName, "/networks/")+len("/networks/"):]
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)

			var payload connectRequest
			require.NoError(t, json.Unmarshal(body, &payload))
			connectPayloads[networkName] = payload
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	})

	wrapped := WrapDockerAPIClientForInspectCompatibility(dockerClient)
	result, err := wrapped.ContainerCreate(context.Background(), client.ContainerCreateOptions{
		Config: &containertypes.Config{
			Image: "nginx:alpine",
		},
		HostConfig: &containertypes.HostConfig{
			NetworkMode: containertypes.NetworkMode("synobridge"),
		},
		NetworkingConfig: &networktypes.NetworkingConfig{
			EndpointsConfig: map[string]*networktypes.EndpointSettings{
				"synobridge": {
					Aliases: []string{"app"},
				},
				"nginx-proxy-manager_zbridge": {
					Aliases: []string{"proxy"},
				},
			},
		},
		Name: "test-app",
	})
	require.NoError(t, err)
	assert.Equal(t, "new-container-id", result.ID)

	require.Len(t, createPayload.NetworkingConfig.EndpointsConfig, 1)
	require.Contains(t, createPayload.NetworkingConfig.EndpointsConfig, "synobridge")
	assert.Equal(t, []string{"app"}, createPayload.NetworkingConfig.EndpointsConfig["synobridge"].Aliases)

	require.Len(t, connectPayloads, 1)
	require.Contains(t, connectPayloads, "nginx-proxy-manager_zbridge")
	assert.Equal(t, "new-container-id", connectPayloads["nginx-proxy-manager_zbridge"].Container)
	require.NotNil(t, connectPayloads["nginx-proxy-manager_zbridge"].EndpointConfig)
	assert.Equal(t, []string{"proxy"}, connectPayloads["nginx-proxy-manager_zbridge"].EndpointConfig.Aliases)
}

func newTestDockerClient(t *testing.T, handler http.HandlerFunc) *client.Client {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	dockerClient, err := client.New(
		client.WithHost("tcp://"+strings.TrimPrefix(server.URL, "http://")),
		client.WithAPIVersion("1.41"),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = dockerClient.Close()
	})

	return dockerClient
}
