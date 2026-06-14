package projects

import (
	"testing"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPathMapper_MatchingMount_NoTranslation(t *testing.T) {
	pm := NewPathMapper("/app/data/projects", "")
	result, err := pm.ContainerToHost("/app/data/projects/test/data")
	require.NoError(t, err)
	assert.Equal(t, "/app/data/projects/test/data", result)
}

func TestPathMapper_WindowsMount_Translation(t *testing.T) {
	pm := NewPathMapper("/app/data/projects", "D:/arcane/projects")
	result, err := pm.ContainerToHost("/app/data/projects/test/data")
	require.NoError(t, err)
	assert.Equal(t, "D:/arcane/projects/test/data", result)
}

func TestPathMapper_PathOutsidePrefix_NoTranslation(t *testing.T) {
	pm := NewPathMapper("/app/data/projects", "D:/arcane/projects")
	result, err := pm.ContainerToHost("/etc/hosts")
	require.NoError(t, err)
	assert.Equal(t, "/etc/hosts", result)
}

func TestPathMapper_PathTraversalPrevention(t *testing.T) {
	pm := NewPathMapper("/app/data/projects", "/host/projects")
	result, err := pm.ContainerToHost("/app/data/projects/../../etc/passwd")
	require.NoError(t, err)
	assert.Equal(t, "/app/etc/passwd", result)
}

func TestPathMapper_FromMounts_NestedIndependentMount(t *testing.T) {
	// Reproduces the nested independent-mount bug: a project directory is bind-mounted
	// into Arcane's projects directory from an unrelated host path.
	pm := NewPathMapperFromMounts([]HostMount{
		{Destination: "/app/data", Source: "/home/user/.arcane/data"},
		{Destination: "/app/data/projects/homeassistant", Source: "/home/user/homeassistant"},
	})
	require.True(t, pm.IsNonMatchingMount())

	// Independently-mounted project resolves to its own host path (longest-prefix wins).
	got, err := pm.ContainerToHost("/app/data/projects/homeassistant/service_postgresql/postgres_data")
	require.NoError(t, err)
	assert.Equal(t, "/home/user/homeassistant/service_postgresql/postgres_data", got)

	// A project without its own mount still re-bases under /app/data.
	got, err = pm.ContainerToHost("/app/data/projects/other/data")
	require.NoError(t, err)
	assert.Equal(t, "/home/user/.arcane/data/projects/other/data", got)
}

func TestPathMapper_FromMounts_NoMatchAndMatchingMounts(t *testing.T) {
	pm := NewPathMapperFromMounts([]HostMount{
		{Destination: "/app/data", Source: "/host/data"},
	})

	// Path outside every mount is returned unchanged.
	got, err := pm.ContainerToHost("/etc/hosts")
	require.NoError(t, err)
	assert.Equal(t, "/etc/hosts", got)

	// A table whose mounts all match (source == destination) needs no translation.
	matching := NewPathMapperFromMounts([]HostMount{
		{Destination: "/var/run/docker.sock", Source: "/var/run/docker.sock"},
	})
	assert.False(t, matching.IsNonMatchingMount())
}

func TestPathMapper_TranslateVolumeSources(t *testing.T) {
	pm := NewPathMapper("/app/data/projects", "C:/User/arcane/projects")

	project := &composetypes.Project{
		Services: composetypes.Services{
			"app": {
				Name: "app",
				Volumes: []composetypes.ServiceVolumeConfig{
					{
						Type:   composetypes.VolumeTypeBind,
						Source: "/app/data/projects/myproj/data",
						Target: "/data",
					},
					{
						Type:   composetypes.VolumeTypeVolume,
						Source: "named-vol",
						Target: "/vol",
					},
				},
			},
		},
		Secrets: composetypes.Secrets{
			"my-secret": {
				File: "/app/data/projects/myproj/secret.txt",
			},
		},
		Configs: composetypes.Configs{
			"my-config": {
				File: "/app/data/projects/myproj/config.yaml",
			},
		},
	}

	err := pm.TranslateVolumeSources(project)
	require.NoError(t, err)

	assert.Equal(t, "C:/User/arcane/projects/myproj/data", project.Services["app"].Volumes[0].Source)
	assert.Equal(t, "named-vol", project.Services["app"].Volumes[1].Source)
	assert.Equal(t, "C:/User/arcane/projects/myproj/secret.txt", project.Secrets["my-secret"].File)
	assert.Equal(t, "C:/User/arcane/projects/myproj/config.yaml", project.Configs["my-config"].File)
}
