package system

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPruneAllRequestUnmarshalJSON_LegacyDanglingPayload(t *testing.T) {
	var req PruneAllRequest

	err := json.Unmarshal([]byte(`{
		"containers": true,
		"images": true,
		"volumes": true,
		"networks": true,
		"buildCache": true,
		"dangling": true
	}`), &req)
	require.NoError(t, err)

	require.Equal(t, &PruneContainersOptions{Mode: PruneContainerModeStopped}, req.Containers)
	require.Equal(t, &PruneImagesOptions{Mode: PruneImageModeDangling}, req.Images)
	require.Equal(t, &PruneVolumesOptions{Mode: PruneVolumeModeAnonymous}, req.Volumes)
	require.Equal(t, &PruneNetworksOptions{Mode: PruneNetworkModeUnused}, req.Networks)
	require.Equal(t, &PruneBuildCacheOptions{Mode: PruneBuildCacheModeUnused}, req.BuildCache)
}

func TestPruneAllRequestUnmarshalJSON_LegacyAllPayload(t *testing.T) {
	var req PruneAllRequest

	err := json.Unmarshal([]byte(`{
		"containers": true,
		"images": true,
		"volumes": true,
		"networks": true,
		"buildCache": true,
		"dangling": false
	}`), &req)
	require.NoError(t, err)

	require.Equal(t, &PruneContainersOptions{Mode: PruneContainerModeStopped}, req.Containers)
	require.Equal(t, &PruneImagesOptions{Mode: PruneImageModeAll}, req.Images)
	require.Equal(t, &PruneVolumesOptions{Mode: PruneVolumeModeAll}, req.Volumes)
	require.Equal(t, &PruneNetworksOptions{Mode: PruneNetworkModeUnused}, req.Networks)
	require.Equal(t, &PruneBuildCacheOptions{Mode: PruneBuildCacheModeAll}, req.BuildCache)
}

func TestPruneAllRequestUnmarshalJSON_StructuredPayloadPreserved(t *testing.T) {
	var req PruneAllRequest

	err := json.Unmarshal([]byte(`{
		"containers": {"mode": "olderThan", "until": "24h"},
		"images": {"mode": "all"},
		"volumes": {"mode": "anonymous"},
		"networks": {"mode": "none"},
		"buildCache": {"mode": "olderThan", "until": "30m"}
	}`), &req)
	require.NoError(t, err)

	require.Equal(t, &PruneContainersOptions{
		Mode:  PruneContainerModeOlderThan,
		Until: "24h",
	}, req.Containers)
	require.Equal(t, &PruneImagesOptions{Mode: PruneImageModeAll}, req.Images)
	require.Equal(t, &PruneVolumesOptions{Mode: PruneVolumeModeAnonymous}, req.Volumes)
	require.Equal(t, &PruneNetworksOptions{Mode: PruneNetworkModeNone}, req.Networks)
	require.Equal(t, &PruneBuildCacheOptions{
		Mode:  PruneBuildCacheModeOlderThan,
		Until: "30m",
	}, req.BuildCache)
}
