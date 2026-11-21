package authz

import (
	"testing"

	"github.com/stretchr/testify/require"

	"getarcane.dev/types/v3"
)

func TestVolumeWorkspaceRequiredPermissions(t *testing.T) {
	op := func(c types.VolumeWorkspaceFileChange) string { return c.Operation }
	permissions, valid := WorkspaceRequiredPermissions(VolumeWorkspaceConditions, []types.VolumeWorkspaceFileChange{
		{Operation: types.FileOpCreateFile},
		{Operation: types.FileOpRename},
		{Operation: types.FileOpRestoreFile},
	}, op)
	require.True(t, valid)
	require.Equal(t, []string{PermVolumesUpload, PermVolumesDelete, PermVolumesBackup}, permissions)

	permissions, valid = WorkspaceRequiredPermissions(VolumeWorkspaceConditions, []types.VolumeWorkspaceFileChange{{Operation: "unknown"}}, op)
	require.False(t, valid)
	require.Nil(t, permissions)
}
