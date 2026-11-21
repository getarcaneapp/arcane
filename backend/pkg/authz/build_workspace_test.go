package authz

import (
	"testing"

	"github.com/stretchr/testify/require"

	"getarcane.dev/types/v3"
)

func TestBuildWorkspaceRequiredPermissions(t *testing.T) {
	op := func(c types.WorkspaceFileChange) string { return c.Operation }
	permissions, valid := WorkspaceRequiredPermissions(BuildWorkspaceConditions, []types.WorkspaceFileChange{
		{Operation: types.FileOpCreateFile},
		{Operation: types.FileOpDelete},
		{Operation: types.FileOpMove},
	}, op)
	require.True(t, valid)
	require.Equal(t, []string{PermBuildWorkspacesCreate, PermBuildWorkspacesDelete}, permissions)

	permissions, valid = WorkspaceRequiredPermissions(BuildWorkspaceConditions, []types.WorkspaceFileChange{{Operation: "unknown"}}, op)
	require.False(t, valid)
	require.Nil(t, permissions)
}
