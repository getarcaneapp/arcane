package workspace

import (
	"context"
	"mime/multipart"
	"testing"

	"github.com/stretchr/testify/require"

	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
	"getarcane.dev/types/v3"
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
		"manifest": {`{"fileChanges":[{"operation":"delete","relativePath":"old.txt"}]}`},
	}}, "manifest")
	require.NoError(t, err)
	require.Equal(t, types.FileOpDelete, manifest.FileChanges[0].Operation)
}

func TestRequireVolumeWorkspacePermissions(t *testing.T) {
	const (
		environmentID = "env-1"
		volumeName    = "vol-1"
	)
	h := &Handler{}
	create := []types.VolumeWorkspaceFileChange{{Operation: types.FileOpCreateFile}}
	rename := []types.VolumeWorkspaceFileChange{{Operation: types.FileOpRename}}
	remove := []types.VolumeWorkspaceFileChange{{Operation: types.FileOpDelete}}
	restore := []types.VolumeWorkspaceFileChange{{Operation: types.FileOpRestoreFile}}

	require.Error(t, h.requireVolumeWorkspacePermissions(t.Context(), environmentID, volumeName, create))
	require.NoError(t, h.requireVolumeWorkspacePermissions(
		volumeWorkspacePermissionContext(t, environmentID, authz.PermVolumesUpload), environmentID, volumeName, create,
	))
	require.Error(t, h.requireVolumeWorkspacePermissions(
		volumeWorkspacePermissionContext(t, environmentID, authz.PermVolumesUpload), environmentID, volumeName, rename,
	))
	require.NoError(t, h.requireVolumeWorkspacePermissions(
		volumeWorkspacePermissionContext(t, environmentID, authz.PermVolumesUpload, authz.PermVolumesDelete), environmentID, volumeName, rename,
	))
	require.NoError(t, h.requireVolumeWorkspacePermissions(
		volumeWorkspacePermissionContext(t, environmentID, authz.PermVolumesDelete), environmentID, volumeName, remove,
	))
	require.NoError(t, h.requireVolumeWorkspacePermissions(
		volumeWorkspacePermissionContext(t, environmentID, authz.PermVolumesBackup), environmentID, volumeName, restore,
	))
	require.Error(t, h.requireVolumeWorkspacePermissions(
		volumeWorkspacePermissionContext(t, "env-2", authz.PermVolumesUpload), environmentID, volumeName, create,
	))
}
