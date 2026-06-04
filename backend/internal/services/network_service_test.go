package services

import (
	"testing"

	"github.com/moby/moby/api/types/network"
	client "github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

func TestCanUseSDKNetworkCreateInternal(t *testing.T) {
	require.True(t, canUseSDKNetworkCreateInternal(client.NetworkCreateOptions{}))
	require.True(t, canUseSDKNetworkCreateInternal(client.NetworkCreateOptions{Driver: "bridge", Labels: map[string]string{"app": "arcane"}}))
	require.False(t, canUseSDKNetworkCreateInternal(client.NetworkCreateOptions{Ingress: true}))
	require.False(t, canUseSDKNetworkCreateInternal(client.NetworkCreateOptions{Options: map[string]string{"com.docker.network.bridge.name": "br-test"}}))
	require.True(t, canUseSDKNetworkCreateInternal(client.NetworkCreateOptions{IPAM: &network.IPAM{Driver: "default"}}))
}
