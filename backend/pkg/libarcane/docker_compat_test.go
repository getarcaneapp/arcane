package libarcane

import (
	"net"
	"net/netip"
	"testing"

	"github.com/moby/moby/api/types/network"
	"github.com/stretchr/testify/require"
)

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
