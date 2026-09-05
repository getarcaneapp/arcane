package projects

import (
	"os"
	"path/filepath"
	"testing"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"
	projecttypes "github.com/getarcaneapp/arcane/types/v2/project"
	"github.com/moby/moby/api/types/volume"
	"github.com/stretchr/testify/assert"
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

func TestRenamedVolumeCreateOptionsInternal_UsesPendingComposeDriverOptions(t *testing.T) {
	entry := struct {
		NewConfig composetypes.VolumeConfig
		OldVolume volume.Volume
	}{
		OldVolume: volume.Volume{
			Driver:  "old-driver",
			Options: map[string]string{"old": "true"},
			Labels:  map[string]string{"existing": "kept"},
		},
		NewConfig: composetypes.VolumeConfig{
			Name:       "web_data",
			Driver:     "new-driver",
			DriverOpts: map[string]string{"new": "true"},
			CustomLabels: composetypes.Labels{
				api.ProjectLabel: "web",
				api.VolumeLabel:  "data",
			},
		},
	}

	options, err := renamedVolumeCreateOptionsInternal(entry.NewConfig, entry.OldVolume.Labels)
	require.NoError(t, err)
	require.Equal(t, "web_data", options.Name)
	require.Equal(t, "new-driver", options.Driver)
	require.Equal(t, map[string]string{"new": "true"}, options.DriverOpts)
	require.Equal(t, "kept", options.Labels["existing"])
	require.Equal(t, "web", options.Labels[api.ProjectLabel])
	require.Equal(t, "data", options.Labels[api.VolumeLabel])
	require.NotEmpty(t, options.Labels[api.ConfigHashLabel])
}

func TestComposeVolumeKeysWithExplicitNameInternal(t *testing.T) {
	projectPath := t.TempDir()
	rootCompose := filepath.Join(projectPath, "compose.yaml")
	includeCompose := filepath.Join(projectPath, "included.yaml")

	require.NoError(t, os.WriteFile(rootCompose, []byte(`
services:
  app:
    image: nginx:alpine
    volumes:
      - data:/data
      - fixed:/fixed
      - env_named:/env
      - env_default:/env-default
      - env_unbraced:/env-unbraced
      - escaped:/escaped
      - scalar:/scalar
      - null_named:/null
      - numeric_named:/numeric
volumes:
  data:
    driver: local
  fixed:
    name: app-data
  env_named:
    name: ${APP_VOLUME}
  env_default:
    name: ${APP_VOLUME:-app-data}
  env_unbraced:
    name: $APP_VOLUME
  escaped:
    name: $${APP_VOLUME}
  scalar:
  null_named:
    name: null
  numeric_named:
    name: 123
  inline: {}
`), 0o644))
	require.NoError(t, os.WriteFile(includeCompose, []byte(`
volumes:
  included:
    name: included-fixed
  included_implicit:
    driver: local
`), 0o644))

	explicit, err := composeVolumeKeysWithExplicitNameInternal([]string{rootCompose, includeCompose})
	require.NoError(t, err)

	assert.Contains(t, explicit, "fixed")
	assert.Contains(t, explicit, "escaped")
	assert.Contains(t, explicit, "included")
	assert.NotContains(t, explicit, "data")
	assert.NotContains(t, explicit, "env_named")
	assert.NotContains(t, explicit, "env_default")
	assert.NotContains(t, explicit, "env_unbraced")
	assert.NotContains(t, explicit, "scalar")
	assert.NotContains(t, explicit, "null_named")
	assert.NotContains(t, explicit, "numeric_named")
	assert.NotContains(t, explicit, "inline")
	assert.NotContains(t, explicit, "included_implicit")
}

func TestProjectRenameJournalTargetsCopiedInternal(t *testing.T) {
	require.False(t, renameJournalTargetsCopiedInternal(projecttypes.RenameJournalPhaseStarted))
	require.False(t, renameJournalTargetsCopiedInternal(projecttypes.RenameJournalPhaseProjectStateRolledBack))
	require.True(t, renameJournalTargetsCopiedInternal(projecttypes.RenameJournalPhaseSourceCleanupPending))
	require.True(t, renameJournalTargetsCopiedInternal(projecttypes.RenameJournalPhaseTargetsCopied))
	require.True(t, renameJournalTargetsCopiedInternal(projecttypes.RenameJournalPhaseOldVolumesRemoved))
	require.True(t, renameJournalTargetsCopiedInternal(projecttypes.RenameJournalPhaseProjectStateCommitted))
}
