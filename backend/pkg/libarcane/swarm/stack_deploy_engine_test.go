package swarm

import (
	"path/filepath"
	"testing"

	composegotypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/moby/moby/api/types/mount"
	"github.com/stretchr/testify/require"
)

func TestResolvePathWithinWorkingDirInternal_AllowsPathsWithinWorkingDir(t *testing.T) {
	workingDir := filepath.Join(string(filepath.Separator), "tmp", "stack")

	path, err := resolvePathWithinWorkingDirInternal(workingDir, filepath.Join("configs", "app.env"))
	require.NoError(t, err)
	require.Equal(t, filepath.Join(workingDir, "configs", "app.env"), path)
}

func TestResolvePathWithinWorkingDirInternal_RejectsEscapingPaths(t *testing.T) {
	workingDir := filepath.Join(string(filepath.Separator), "tmp", "stack")

	_, err := resolvePathWithinWorkingDirInternal(workingDir, filepath.Join("..", "..", "etc", "shadow"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "escapes the working directory")
}

func TestConvertServiceMountsScopesOnlyConfiguredNamedVolumes(t *testing.T) {
	mounts := convertServiceMounts(
		[]composegotypes.ServiceVolumeConfig{
			{Type: "volume", Source: "plain", Target: "/plain"},
			{Type: "volume", Source: "driver", Target: "/driver"},
			{Type: "volume", Source: "opts", Target: "/opts"},
			{Type: "volume", Source: "external", Target: "/external"},
		},
		"stack",
		composegotypes.Volumes{
			"plain":    {},
			"driver":   {Driver: "local"},
			"opts":     {Name: "custom", DriverOpts: map[string]string{"type": "nfs"}},
			"external": {External: true},
		},
	)

	require.Len(t, mounts, 4)
	require.Equal(t, mount.TypeVolume, mounts[0].Type)
	require.Equal(t, "plain", mounts[0].Source)
	require.Equal(t, "stack_driver", mounts[1].Source)
	require.Equal(t, "stack_custom", mounts[2].Source)
	require.Equal(t, "external", mounts[3].Source)
}
