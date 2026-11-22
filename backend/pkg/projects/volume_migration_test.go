package projects

import (
	"os"
	"path/filepath"
	"testing"

	composeTypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/moby/moby/api/types/volume"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"getarcane.dev/types/v3"
)

func TestBuildProjectRenamedVolumeConfig(t *testing.T) {
	source := composeTypes.VolumeConfig{
		Name:   "nginx_data",
		Driver: "local",
		Labels: composeTypes.Labels{
			"existing": "kept",
		},
		CustomLabels: composeTypes.Labels{
			api.ProjectLabel: "nginx",
			api.VolumeLabel:  "data",
		},
	}

	got := ComposeVolumeConfig(source, "data", "web_data", "web")

	require.Equal(t, "web_data", got.Name)
	require.Equal(t, "local", got.Driver)
	require.Equal(t, "kept", got.Labels["existing"])
	require.Equal(t, "web", got.CustomLabels[api.ProjectLabel])
	require.Equal(t, "data", got.CustomLabels[api.VolumeLabel])
	require.Equal(t, api.ComposeVersion, got.CustomLabels[api.VersionLabel])
}

func TestRenamedVolumeCreateOptionsInternal_UsesPendingComposeDriverOptions(t *testing.T) {
	entry := struct {
		NewConfig composeTypes.VolumeConfig
		OldVolume volume.Volume
	}{
		OldVolume: volume.Volume{
			Driver:  "old-driver",
			Options: map[string]string{"old": "true"},
			Labels:  map[string]string{"existing": "kept"},
		},
		NewConfig: composeTypes.VolumeConfig{
			Name:       "web_data",
			Driver:     "new-driver",
			DriverOpts: map[string]string{"new": "true"},
			CustomLabels: composeTypes.Labels{
				api.ProjectLabel: "web",
				api.VolumeLabel:  "data",
			},
		},
	}

	options, err := ComposeVolumeCreateOptions(entry.NewConfig, entry.OldVolume.Labels)
	require.NoError(t, err)
	require.Equal(t, "web_data", options.Name)
	require.Equal(t, "new-driver", options.Driver)
	require.Equal(t, map[string]string{"new": "true"}, options.DriverOpts)
	require.Equal(t, "kept", options.Labels["existing"])
	require.Equal(t, "web", options.Labels[api.ProjectLabel])
	require.Equal(t, "data", options.Labels[api.VolumeLabel])
	require.NotEmpty(t, options.Labels[api.ConfigHashLabel])
}

func TestRenamedVolumeCreateOptions_ConfigHashMatchesCompose(t *testing.T) {
	tests := []struct {
		name   string
		config composeTypes.VolumeConfig
		want   string
	}{
		{
			name:   "default driver",
			config: composeTypes.VolumeConfig{Name: "x"},
			want:   "4a2b2b84ef7d42b2ada07a9b41a782aa389aec6a62317db8ef67fc35d81d025e",
		},
		{
			name: "driver options and labels",
			config: composeTypes.VolumeConfig{
				Name:       "proj_data",
				Driver:     "nfs",
				DriverOpts: map[string]string{"type": "nfs", "o": "addr=10.0.0.1,rw"},
				Labels:     composeTypes.Labels{"tier": "db"},
			},
			want: "23ef8380bd7145a59addc7c4d07af41103571d5c7cd39b6caf64e818e80fe93a",
		},
		{
			name: "external with html characters",
			config: composeTypes.VolumeConfig{
				Name:     "e",
				External: true,
				Labels:   composeTypes.Labels{"q": "a<b&c"},
			},
			want: "7459d64fc51178737aeb36808725efcb1943dce34b2abd14eb24e28b8f17f0f2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options, err := ComposeVolumeCreateOptions(tt.config, nil)
			require.NoError(t, err)
			require.Equal(t, tt.want, options.Labels[api.ConfigHashLabel])
		})
	}
}

func TestComposeVolumeKeysWithExplicitName(t *testing.T) {
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

	explicit, err := composeVolumeKeysWithExplicitName(t.Context(), []string{rootCompose, includeCompose})
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

func TestProjectRenameJournalTargetsCopied(t *testing.T) {
	require.False(t, renameJournalTargetsCopied(types.ProjectRenameJournalPhaseStarted))
	require.False(t, renameJournalTargetsCopied(types.ProjectRenameJournalPhaseProjectStateRolledBack))
	require.True(t, renameJournalTargetsCopied(types.ProjectRenameJournalPhaseSourceCleanupPending))
	require.True(t, renameJournalTargetsCopied(types.ProjectRenameJournalPhaseTargetsCopied))
	require.True(t, renameJournalTargetsCopied(types.ProjectRenameJournalPhaseOldVolumesRemoved))
	require.True(t, renameJournalTargetsCopied(types.ProjectRenameJournalPhaseProjectStateCommitted))
}
