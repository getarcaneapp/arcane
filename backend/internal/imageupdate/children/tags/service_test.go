package tags

import (
	"testing"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/stretchr/testify/require"
	"go.getarcane.app/updater"
)

func TestContainerAggregationPreservesImageResult(t *testing.T) {
	original := &types.ImageUpdateResponse{
		HasUpdate:      false,
		UpdateType:     string(updater.UpdateTypeDigest),
		CurrentVersion: "3.20.0",
		LatestVersion:  "3.20.0",
	}
	results := map[string]*types.ImageUpdateResponse{"alpine:3.20.0": original}
	scoped := &types.ImageUpdateResponse{
		ImageRef:       "docker.io/library/alpine:3.20.0",
		HasUpdate:      true,
		UpdateType:     string(updater.UpdateTypeTag),
		CurrentVersion: "3.20.0",
		LatestVersion:  "3.20.1",
	}
	attached := AttachContainerUpdates(results, map[string]*types.ImageUpdateResponse{"tagged": scoped})["alpine:3.20.0"]
	require.True(t, attached.HasUpdate)
	require.Equal(t, string(updater.UpdateTypeTag), attached.UpdateType)
	require.Equal(t, "3.20.1", attached.LatestVersion)
	require.Same(t, scoped, attached.ContainerUpdates["tagged"])
	require.NotNil(t, attached.ImageUpdate)
	require.False(t, attached.ImageUpdate.HasUpdate, "untagged siblings must keep the original digest result")
	require.Equal(t, string(updater.UpdateTypeDigest), attached.ImageUpdate.UpdateType)
	require.Nil(t, attached.ImageUpdate.ContainerUpdates)
	require.Nil(t, attached.ImageUpdate.ImageUpdate, "snapshot must not create recursive JSON")
}
