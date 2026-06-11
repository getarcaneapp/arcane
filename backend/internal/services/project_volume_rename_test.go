package services

import (
	"testing"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/moby/moby/api/types/container"
	"github.com/stretchr/testify/require"
)

func TestBuildProjectRenamedVolumeConfigInternal(t *testing.T) {
	source := composetypes.VolumeConfig{
		Name:   "nginx_data",
		Driver: "local",
		Labels: composetypes.Labels{
			"existing": "kept",
		},
		CustomLabels: composetypes.Labels{
			api.ProjectLabel: "nginx",
			api.VolumeLabel:  "data",
		},
	}

	got := buildProjectRenamedVolumeConfigInternal(source, "data", "web_data", "web")

	require.Equal(t, "web_data", got.Name)
	require.Equal(t, "local", got.Driver)
	require.Equal(t, "kept", got.Labels["existing"])
	require.Equal(t, "web", got.CustomLabels[api.ProjectLabel])
	require.Equal(t, "data", got.CustomLabels[api.VolumeLabel])
	require.Equal(t, api.ComposeVersion, got.CustomLabels[api.VersionLabel])
}

func TestContainerSummaryMountsVolumeInternal(t *testing.T) {
	summary := container.Summary{
		Labels: map[string]string{
			libarcane.InternalResourceLabel: "true",
		},
		Mounts: []container.MountPoint{
			{Name: "web_data", Source: "/var/lib/docker/volumes/web_data/_data"},
			{Name: "other", Source: "other"},
		},
	}

	require.True(t, libarcane.IsInternalContainer(summary.Labels))
	require.True(t, containerSummaryMountsVolumeInternal(summary, "web_data"))
	require.False(t, containerSummaryMountsVolumeInternal(summary, "nginx_data"))
}
