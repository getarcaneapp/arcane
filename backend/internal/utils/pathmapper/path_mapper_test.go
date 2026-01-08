package pathmapper

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
