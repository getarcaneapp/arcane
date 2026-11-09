package authz

import (
	"testing"

	"github.com/stretchr/testify/require"

	"getarcane.dev/types/v3"
)

func TestBuildWorkspaceRequiredPermissions(t *testing.T) {
	permissions, valid := BuildWorkspaceRequiredPermissions([]types.WorkspaceFileChange{
		{Operation: types.FileOpCreateFile},
		{Operation: types.FileOpDelete},
		{Operation: types.FileOpMove},
	})
	require.True(t, valid)
	require.Equal(t, []string{PermBuildWorkspacesCreate, PermBuildWorkspacesDelete}, permissions)

	permissions, valid = BuildWorkspaceRequiredPermissions([]types.WorkspaceFileChange{{Operation: "unknown"}})
	require.False(t, valid)
	require.Nil(t, permissions)
}
