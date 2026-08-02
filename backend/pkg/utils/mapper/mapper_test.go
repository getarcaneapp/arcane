package mapper

import (
	"net/netip"
	"testing"

	networktypes "github.com/getarcaneapp/arcane/types/v2/network"
	dockernetwork "github.com/moby/moby/api/types/network"
	"github.com/stretchr/testify/require"
)

func TestMapOneConvertsNetworkIPAMConfigInternal(t *testing.T) {
	source := dockernetwork.Inspect{
		Network: dockernetwork.Network{
			IPAM: dockernetwork.IPAM{
				Driver: "default",
				Config: []dockernetwork.IPAMConfig{
					{
						Subnet:  netip.MustParsePrefix("172.16.5.0/24"),
						Gateway: netip.MustParseAddr("172.16.5.1"),
						AuxAddress: map[string]netip.Addr{
							"dns":   netip.MustParseAddr("172.16.5.53"),
							"unset": {},
						},
					},
				},
			},
		},
	}

	mapped, err := MapOne[dockernetwork.Inspect, networktypes.Inspect](source)
	require.NoError(t, err)
	require.Equal(t, "default", mapped.IPAM.Driver)
	require.Len(t, mapped.IPAM.Config, 1)

	config := mapped.IPAM.Config[0]
	require.Equal(t, "172.16.5.0/24", config.Subnet)
	require.Equal(t, "172.16.5.1", config.Gateway)
	require.Empty(t, config.IPRange)
	require.Equal(t, map[string]string{
		"dns":   "172.16.5.53",
		"unset": "",
	}, config.AuxAddress)
}
