package handlers

import (
	"context"
	"mime/multipart"
	"testing"

	humamw "github.com/getarcaneapp/arcane/backend/v2/api/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/stretchr/testify/require"
)

func volumeWorkspacePermissionContextInternal(environmentID string, permissions ...string) context.Context {
	permissionSet := authz.NewPermissionSet()
	permissionSet.AddEnv(environmentID, permissions...)
	return context.WithValue(context.Background(), humamw.ContextKeyUserPermissions, permissionSet)
}

func TestParseVolumeWorkspaceManifestInternal(t *testing.T) {
	_, err := parseVolumeWorkspaceManifestInternal(multipart.Form{})
	require.Error(t, err)
	_, err = parseVolumeWorkspaceManifestInternal(multipart.Form{Value: map[string][]string{"manifest": {"{"}}})
	require.Error(t, err)
	_, err = parseVolumeWorkspaceManifestInternal(multipart.Form{Value: map[string][]string{"manifest": {"{}", "{}"}}})
	require.Error(t, err)

	manifest, err := parseVolumeWorkspaceManifestInternal(multipart.Form{Value: map[string][]string{
		"manifest": {`{"fileTreeRevision":"revision","fileChanges":[{"operation":"delete","relativePath":"old.txt"}]}`},
	}})
	require.NoError(t, err)
	require.Equal(t, "revision", manifest.FileTreeRevision)
	require.Equal(t, volumetypes.FileOpDelete, manifest.FileChanges[0].Operation)
}

func TestRequireVolumeWorkspacePermissionsInternal(t *testing.T) {
	const environmentID = "env-1"
	create := []volumetypes.FileChange{{Operation: volumetypes.FileOpCreateFile}}
	rename := []volumetypes.FileChange{{Operation: volumetypes.FileOpRename}}
	remove := []volumetypes.FileChange{{Operation: volumetypes.FileOpDelete}}
	restore := []volumetypes.FileChange{{Operation: volumetypes.FileOpRestoreFile}}

	require.Error(t, requireVolumeWorkspacePermissionsInternal(context.Background(), environmentID, create))
	require.NoError(t, requireVolumeWorkspacePermissionsInternal(
		volumeWorkspacePermissionContextInternal(environmentID, authz.PermVolumesUpload), environmentID, create,
	))
	require.Error(t, requireVolumeWorkspacePermissionsInternal(
		volumeWorkspacePermissionContextInternal(environmentID, authz.PermVolumesUpload), environmentID, rename,
	))
	require.NoError(t, requireVolumeWorkspacePermissionsInternal(
		volumeWorkspacePermissionContextInternal(environmentID, authz.PermVolumesUpload, authz.PermVolumesDelete), environmentID, rename,
	))
	require.NoError(t, requireVolumeWorkspacePermissionsInternal(
		volumeWorkspacePermissionContextInternal(environmentID, authz.PermVolumesDelete), environmentID, remove,
	))
	require.NoError(t, requireVolumeWorkspacePermissionsInternal(
		volumeWorkspacePermissionContextInternal(environmentID, authz.PermVolumesBackup), environmentID, restore,
	))
	require.Error(t, requireVolumeWorkspacePermissionsInternal(
		volumeWorkspacePermissionContextInternal("env-2", authz.PermVolumesUpload), environmentID, create,
	))
}
