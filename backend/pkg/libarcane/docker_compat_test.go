package libarcane

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

type fakeContainerCreateCompatibilityClient struct {
	client.APIClient
	containerCreateResult client.ContainerCreateResult
	containerCreateErr    error
	networkConnectErrs    map[string]error
	containerRemoveErr    error
	containerCreateCalls  []client.ContainerCreateOptions
	networkConnectCalls   []fakeNetworkConnectCall
	containerRemoveCalls  []fakeContainerRemoveCall
	clientVersionCalls    int
	serverVersionCalls    int
}

type fakeNetworkConnectCall struct {
	network string
	options client.NetworkConnectOptions
}

type fakeContainerRemoveCall struct {
	containerID string
	options     client.ContainerRemoveOptions
}

func (f *fakeContainerCreateCompatibilityClient) ClientVersion() string {
	f.clientVersionCalls++
	return ""
}

func (f *fakeContainerCreateCompatibilityClient) ServerVersion(context.Context, client.ServerVersionOptions) (client.ServerVersionResult, error) {
	f.serverVersionCalls++
	return client.ServerVersionResult{}, nil
}

func (f *fakeContainerCreateCompatibilityClient) ContainerCreate(_ context.Context, options client.ContainerCreateOptions) (client.ContainerCreateResult, error) {
	f.containerCreateCalls = append(f.containerCreateCalls, options)
	if f.containerCreateErr != nil {
		return client.ContainerCreateResult{}, f.containerCreateErr
	}
	return f.containerCreateResult, nil
}

func (f *fakeContainerCreateCompatibilityClient) NetworkConnect(_ context.Context, networkName string, options client.NetworkConnectOptions) (client.NetworkConnectResult, error) {
	f.networkConnectCalls = append(f.networkConnectCalls, fakeNetworkConnectCall{network: networkName, options: options})
	if err := f.networkConnectErrs[networkName]; err != nil {
		return client.NetworkConnectResult{}, err
	}
	return client.NetworkConnectResult{}, nil
}

func (f *fakeContainerCreateCompatibilityClient) ContainerRemove(_ context.Context, containerID string, options client.ContainerRemoveOptions) (client.ContainerRemoveResult, error) {
	f.containerRemoveCalls = append(f.containerRemoveCalls, fakeContainerRemoveCall{containerID: containerID, options: options})
	if f.containerRemoveErr != nil {
		return client.ContainerRemoveResult{}, f.containerRemoveErr
	}
	return client.ContainerRemoveResult{}, nil
}

func mustHardwareAddr(t *testing.T, addr string) network.HardwareAddr {
	t.Helper()
	parsed, err := net.ParseMAC(addr)
	require.NoError(t, err)
	return network.HardwareAddr(parsed)
}

func TestIsDockerAPIVersionAtLeast(t *testing.T) {
	tests := []struct {
		name     string
		current  string
		minimum  string
		expected bool
	}{
		{name: "equal", current: "1.44", minimum: "1.44", expected: true},
		{name: "greater minor", current: "1.45", minimum: "1.44", expected: true},
		{name: "lesser minor", current: "1.43", minimum: "1.44", expected: false},
		{name: "patch still greater", current: "1.44.1", minimum: "1.44", expected: true},
		{name: "podman api", current: "1.41", minimum: "1.44", expected: false},
		{name: "trims v prefix", current: "v1.44", minimum: "1.44", expected: true},
		{name: "invalid current", current: "invalid", minimum: "1.44", expected: false},
		{name: "invalid minimum", current: "1.44", minimum: "invalid", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, IsDockerAPIVersionAtLeast(tt.current, tt.minimum))
		})
	}
}

func TestSupportsDockerCreatePerNetworkMACAddress(t *testing.T) {
	require.True(t, SupportsDockerCreatePerNetworkMACAddress("1.44"))
	require.True(t, SupportsDockerCreatePerNetworkMACAddress("1.46"))
	require.False(t, SupportsDockerCreatePerNetworkMACAddress("1.43"))
	require.False(t, SupportsDockerCreatePerNetworkMACAddress("1.41"))
}

func TestSupportsDockerCreateMultiEndpointNetworking(t *testing.T) {
	require.True(t, SupportsDockerCreateMultiEndpointNetworking("1.44"))
	require.True(t, SupportsDockerCreateMultiEndpointNetworking("1.46"))
	require.False(t, SupportsDockerCreateMultiEndpointNetworking("1.43"))
	require.False(t, SupportsDockerCreateMultiEndpointNetworking("1.41"))
}

func TestSanitizeContainerCreateEndpointSettingsForDockerAPI(t *testing.T) {
	input := map[string]*network.EndpointSettings{
		"bridge": {
			MacAddress: mustHardwareAddr(t, "02:42:ac:11:00:02"),
			IPAddress:  netip.MustParseAddr("172.17.0.2"),
			Aliases:    []string{"svc", "svc-1"},
		},
		"custom": {
			MacAddress: mustHardwareAddr(t, "02:42:ac:11:00:03"),
			IPAddress:  netip.MustParseAddr("10.0.0.10"),
			Aliases:    []string{"custom-svc"},
		},
	}

	t.Run("strips mac for api below 1.44", func(t *testing.T) {
		out := SanitizeContainerCreateEndpointSettingsForDockerAPI(input, "1.43")
		require.Len(t, out, 2)
		require.Empty(t, out["bridge"].MacAddress.String())
		require.Empty(t, out["custom"].MacAddress.String())
		require.Equal(t, netip.MustParseAddr("172.17.0.2"), out["bridge"].IPAddress)
		require.Equal(t, []string{"svc", "svc-1"}, out["bridge"].Aliases)

		// Ensure original map entries are untouched
		require.Equal(t, "02:42:ac:11:00:02", input["bridge"].MacAddress.String())
		require.Equal(t, "02:42:ac:11:00:03", input["custom"].MacAddress.String())
	})

	t.Run("preserves mac for api at or above 1.44", func(t *testing.T) {
		out := SanitizeContainerCreateEndpointSettingsForDockerAPI(input, "1.44")
		require.Len(t, out, 2)
		require.Equal(t, "02:42:ac:11:00:02", out["bridge"].MacAddress.String())
		require.Equal(t, "02:42:ac:11:00:03", out["custom"].MacAddress.String())
	})

	t.Run("nil or empty input", func(t *testing.T) {
		require.Nil(t, SanitizeContainerCreateEndpointSettingsForDockerAPI(nil, "1.44"))
		require.Nil(t, SanitizeContainerCreateEndpointSettingsForDockerAPI(map[string]*network.EndpointSettings{}, "1.44"))
	})
}

func TestPrepareContainerCreateOptionsForDockerAPI(t *testing.T) {
	options := client.ContainerCreateOptions{
		HostConfig: &container.HostConfig{
			NetworkMode: container.NetworkMode("synobridge"),
		},
		NetworkingConfig: &network.NetworkingConfig{
			EndpointsConfig: map[string]*network.EndpointSettings{
				"synobridge": {
					Aliases: []string{"app"},
				},
				"nginx-proxy-manager_zbridge": {
					Aliases: []string{"proxy"},
				},
			},
		},
	}

	t.Run("splits legacy multi-network create into primary plus extras", func(t *testing.T) {
		adjusted, extras := PrepareContainerCreateOptionsForDockerAPI(options, "1.43")
		require.NotNil(t, adjusted.NetworkingConfig)
		require.Len(t, adjusted.NetworkingConfig.EndpointsConfig, 1)
		require.Contains(t, adjusted.NetworkingConfig.EndpointsConfig, "synobridge")
		require.Equal(t, []string{"app"}, adjusted.NetworkingConfig.EndpointsConfig["synobridge"].Aliases)

		require.Len(t, extras, 1)
		require.Contains(t, extras, "nginx-proxy-manager_zbridge")
		require.Equal(t, []string{"proxy"}, extras["nginx-proxy-manager_zbridge"].Aliases)

		adjusted.NetworkingConfig.EndpointsConfig["synobridge"].Aliases[0] = "changed"
		extras["nginx-proxy-manager_zbridge"].Aliases[0] = "changed-too"

		require.Equal(t, []string{"app"}, options.NetworkingConfig.EndpointsConfig["synobridge"].Aliases)
		require.Equal(t, []string{"proxy"}, options.NetworkingConfig.EndpointsConfig["nginx-proxy-manager_zbridge"].Aliases)
	})

	t.Run("leaves create options unchanged on newer daemon apis", func(t *testing.T) {
		adjusted, extras := PrepareContainerCreateOptionsForDockerAPI(options, "1.44")
		require.Len(t, adjusted.NetworkingConfig.EndpointsConfig, 2)
		require.Nil(t, extras)
	})

	t.Run("uses deterministic fallback primary network when mode is unset", func(t *testing.T) {
		adjusted, extras := PrepareContainerCreateOptionsForDockerAPI(client.ContainerCreateOptions{
			NetworkingConfig: &network.NetworkingConfig{
				EndpointsConfig: map[string]*network.EndpointSettings{
					"znet": {Aliases: []string{"z"}},
					"anet": {Aliases: []string{"a"}},
				},
			},
		}, "1.43")

		require.NotNil(t, adjusted.HostConfig)
		require.Equal(t, container.NetworkMode("anet"), adjusted.HostConfig.NetworkMode)
		require.Len(t, adjusted.NetworkingConfig.EndpointsConfig, 1)
		require.Contains(t, adjusted.NetworkingConfig.EndpointsConfig, "anet")
		require.Len(t, extras, 1)
		require.Contains(t, extras, "znet")
	})

	t.Run("leaves create options unchanged when named network mode is missing from endpoints", func(t *testing.T) {
		options := client.ContainerCreateOptions{
			HostConfig: &container.HostConfig{
				NetworkMode: container.NetworkMode("mynetwork"),
			},
			NetworkingConfig: &network.NetworkingConfig{
				EndpointsConfig: map[string]*network.EndpointSettings{
					"anet": {Aliases: []string{"a"}},
					"znet": {Aliases: []string{"z"}},
				},
			},
		}

		adjusted, extras := PrepareContainerCreateOptionsForDockerAPI(options, "1.43")

		require.Equal(t, options.HostConfig.NetworkMode, adjusted.HostConfig.NetworkMode)
		require.Len(t, adjusted.NetworkingConfig.EndpointsConfig, 2)
		require.Contains(t, adjusted.NetworkingConfig.EndpointsConfig, "anet")
		require.Contains(t, adjusted.NetworkingConfig.EndpointsConfig, "znet")
		require.Nil(t, extras)
	})
}

func TestContainerCreateWithCompatibilityForAPIVersion(t *testing.T) {
	t.Run("uses provided api version without re-detecting it", func(t *testing.T) {
		fakeClient := &fakeContainerCreateCompatibilityClient{
			containerCreateResult: client.ContainerCreateResult{ID: "created"},
		}

		result, err := ContainerCreateWithCompatibilityForAPIVersion(t.Context(), fakeClient, client.ContainerCreateOptions{
			Config: &container.Config{Image: "nginx:latest"},
		}, "1.44")

		require.NoError(t, err)
		require.Equal(t, "created", result.ID)
		require.Zero(t, fakeClient.clientVersionCalls)
		require.Zero(t, fakeClient.serverVersionCalls)
		require.Len(t, fakeClient.containerCreateCalls, 1)
	})

	t.Run("removes created container when a later network attach fails", func(t *testing.T) {
		fakeClient := &fakeContainerCreateCompatibilityClient{
			containerCreateResult: client.ContainerCreateResult{ID: "created-container"},
			networkConnectErrs: map[string]error{
				"cnet": errors.New("boom"),
			},
		}

		_, err := ContainerCreateWithCompatibilityForAPIVersion(t.Context(), fakeClient, client.ContainerCreateOptions{
			Config: &container.Config{Image: "nginx:latest"},
			HostConfig: &container.HostConfig{
				NetworkMode: container.NetworkMode("anet"),
			},
			NetworkingConfig: &network.NetworkingConfig{
				EndpointsConfig: map[string]*network.EndpointSettings{
					"anet": {Aliases: []string{"a"}},
					"bnet": {Aliases: []string{"b"}},
					"cnet": {Aliases: []string{"c"}},
					"dnet": {Aliases: []string{"d"}},
				},
			},
		}, "1.43")

		require.ErrorContains(t, err, "connect network cnet")
		require.Len(t, fakeClient.containerCreateCalls, 1)
		require.Len(t, fakeClient.networkConnectCalls, 2)
		require.Equal(t, "bnet", fakeClient.networkConnectCalls[0].network)
		require.Equal(t, "cnet", fakeClient.networkConnectCalls[1].network)
		require.Len(t, fakeClient.containerRemoveCalls, 1)
		require.Equal(t, "created-container", fakeClient.containerRemoveCalls[0].containerID)
		require.True(t, fakeClient.containerRemoveCalls[0].options.Force)
	})

	t.Run("propagates network attach error when rollback remove also fails", func(t *testing.T) {
		connectErr := errors.New("connect-boom")
		removeErr := errors.New("remove-boom")
		fakeClient := &fakeContainerCreateCompatibilityClient{
			containerCreateResult: client.ContainerCreateResult{ID: "created-container"},
			networkConnectErrs: map[string]error{
				"bnet": connectErr,
			},
			containerRemoveErr: removeErr,
		}

		_, err := ContainerCreateWithCompatibilityForAPIVersion(t.Context(), fakeClient, client.ContainerCreateOptions{
			HostConfig: &container.HostConfig{
				NetworkMode: container.NetworkMode("anet"),
			},
			NetworkingConfig: &network.NetworkingConfig{
				EndpointsConfig: map[string]*network.EndpointSettings{
					"anet": {},
					"bnet": {},
				},
			},
		}, "1.43")

		require.ErrorContains(t, err, "connect network bnet")
		require.ErrorIs(t, err, connectErr)
		require.NotErrorIs(t, err, removeErr)
		require.Len(t, fakeClient.containerRemoveCalls, 1)
	})
}
