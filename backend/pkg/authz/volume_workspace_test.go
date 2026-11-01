package authz

import (
	"testing"

	"github.com/stretchr/testify/require"

	"getarcane.dev/types/v3"
)

func TestVolumeWorkspaceRequiredPermissions(t *testing.T) {
	permissions, valid := VolumeWorkspaceRequiredPermissions([]types.VolumeWorkspaceFileChange{
		{Operation: types.FileOpCreateFile},
		{Operation: types.FileOpRename},
		{Operation: types.FileOpRestoreFile},
	})
	require.True(t, valid)
	require.Equal(t, []string{PermVolumesUpload, PermVolumesDelete, PermVolumesBackup}, permissions)

	permissions, valid = VolumeWorkspaceRequiredPermissions([]types.VolumeWorkspaceFileChange{{Operation: "unknown"}})
	require.False(t, valid)
	require.Nil(t, permissions)
}
