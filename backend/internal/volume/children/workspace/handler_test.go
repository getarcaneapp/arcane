package workspace

import (
	"context"
	"mime/multipart"
	"testing"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

func volumeWorkspacePermissionContext(t *testing.T, environmentID string, permissions ...string) context.Context {
	permissionSet := authz.NewPermissionSet()
	permissionSet.AddEnv(environmentID, permissions...)
	return context.WithValue(t.Context(), middleware.ContextKeyUserPermissions, permissionSet)
}

func TestParseVolumeWorkspaceManifest(t *testing.T) {
	_, err := handlerutil.ParseMultipartJSONPart[types.VolumeWorkspaceUpdateManifest](multipart.Form{}, "manifest")
	require.Error(t, err)
	_, err = handlerutil.ParseMultipartJSONPart[types.VolumeWorkspaceUpdateManifest](multipart.Form{Value: map[string][]string{"manifest": {"{"}}}, "manifest")
	require.Error(t, err)
	_, err = handlerutil.ParseMultipartJSONPart[types.VolumeWorkspaceUpdateManifest](multipart.Form{Value: map[string][]string{"manifest": {"{}", "{}"}}}, "manifest")
	require.Error(t, err)

	manifest, err := handlerutil.ParseMultipartJSONPart[types.VolumeWorkspaceUpdateManifest](multipart.Form{Value: map[string][]string{
		"manifest": {`{"fileTreeRevision":"revision","fileChanges":[{"operation":"delete","relativePath":"old.txt"}]}`},
	}}, "manifest")
	require.NoError(t, err)
	require.Equal(t, "revision", manifest.FileTreeRevision)
	require.Equal(t, types.FileOpDelete, manifest.FileChanges[0].Operation)
}

func TestRequireVolumeWorkspacePermissions(t *testing.T) {
	const environmentID = "env-1"
	create := []types.VolumeWorkspaceFileChange{{Operation: types.FileOpCreateFile}}
	rename := []types.VolumeWorkspaceFileChange{{Operation: types.FileOpRename}}
	remove := []types.VolumeWorkspaceFileChange{{Operation: types.FileOpDelete}}
	restore := []types.VolumeWorkspaceFileChange{{Operation: types.FileOpRestoreFile}}

	require.Error(t, requireVolumeWorkspacePermissions(t.Context(), environmentID, create))
	require.NoError(t, requireVolumeWorkspacePermissions(
		volumeWorkspacePermissionContext(t, environmentID, authz.PermVolumesUpload), environmentID, create,
	))
	require.Error(t, requireVolumeWorkspacePermissions(
		volumeWorkspacePermissionContext(t, environmentID, authz.PermVolumesUpload), environmentID, rename,
	))
	require.NoError(t, requireVolumeWorkspacePermissions(
		volumeWorkspacePermissionContext(t, environmentID, authz.PermVolumesUpload, authz.PermVolumesDelete), environmentID, rename,
	))
	require.NoError(t, requireVolumeWorkspacePermissions(
		volumeWorkspacePermissionContext(t, environmentID, authz.PermVolumesDelete), environmentID, remove,
	))
	require.NoError(t, requireVolumeWorkspacePermissions(
		volumeWorkspacePermissionContext(t, environmentID, authz.PermVolumesBackup), environmentID, restore,
	))
	require.Error(t, requireVolumeWorkspacePermissions(
		volumeWorkspacePermissionContext(t, "env-2", authz.PermVolumesUpload), environmentID, create,
	))
}
