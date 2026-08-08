package authz

import (
	"testing"

	volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/stretchr/testify/require"
)

func TestVolumeWorkspaceRequiredPermissions(t *testing.T) {
	permissions, valid := VolumeWorkspaceRequiredPermissions([]volumetypes.WorkspaceFileChange{
		{Operation: volumetypes.FileOpCreateFile},
		{Operation: volumetypes.FileOpRename},
		{Operation: volumetypes.FileOpRestoreFile},
	})
	require.True(t, valid)
	require.Equal(t, []string{PermVolumesUpload, PermVolumesDelete, PermVolumesBackup}, permissions)

	permissions, valid = VolumeWorkspaceRequiredPermissions([]volumetypes.WorkspaceFileChange{{Operation: "unknown"}})
	require.False(t, valid)
	require.Nil(t, permissions)
}
