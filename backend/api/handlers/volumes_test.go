package handlers

import (
	"context"
	"mime/multipart"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	humamw "github.com/getarcaneapp/arcane/backend/v2/api/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func volumeWorkspacePermissionContextInternal(environmentID string, permissions ...string) context.Context {
	permissionSet := authz.NewPermissionSet()
	permissionSet.AddEnv(environmentID, permissions...)
	return context.WithValue(context.Background(), humamw.ContextKeyUserPermissions, permissionSet)
}

func TestUploadAndRestoreReturnsBadRequestWhenNoFileProvided(t *testing.T) {
	h := &VolumeHandler{volumeService: &services.VolumeService{}}

	ctx := context.WithValue(adminTestContextInternal(), models.CurrentUserContextKey{}, &models.User{BaseModel: models.BaseModel{ID: "u-1"}})

	_, err := h.UploadAndRestore(ctx, &UploadAndRestoreInput{
		EnvironmentID: "0",
		VolumeName:    "vol-1",
		RawBody:       multipart.Form{},
	})

	require.Error(t, err)

	var statusErr huma.StatusError
	require.ErrorAs(t, err, &statusErr)
	assert.Equal(t, http.StatusBadRequest, statusErr.GetStatus())
}

func TestParseVolumeWorkspaceManifestInternal(t *testing.T) {
	_, err := parseWorkspaceJSONPartInternal[volumetypes.WorkspaceUpdateManifest](multipart.Form{}, "manifest")
	require.Error(t, err)
	_, err = parseWorkspaceJSONPartInternal[volumetypes.WorkspaceUpdateManifest](multipart.Form{Value: map[string][]string{"manifest": {"{"}}}, "manifest")
	require.Error(t, err)
	_, err = parseWorkspaceJSONPartInternal[volumetypes.WorkspaceUpdateManifest](multipart.Form{Value: map[string][]string{"manifest": {"{}", "{}"}}}, "manifest")
	require.Error(t, err)

	manifest, err := parseWorkspaceJSONPartInternal[volumetypes.WorkspaceUpdateManifest](multipart.Form{Value: map[string][]string{
		"manifest": {`{"fileTreeRevision":"revision","fileChanges":[{"operation":"delete","relativePath":"old.txt"}]}`},
	}}, "manifest")
	require.NoError(t, err)
	require.Equal(t, "revision", manifest.FileTreeRevision)
	require.Equal(t, volumetypes.FileOpDelete, manifest.FileChanges[0].Operation)
}

func TestRequireVolumeWorkspacePermissionsInternal(t *testing.T) {
	const environmentID = "env-1"
	create := []volumetypes.WorkspaceFileChange{{Operation: volumetypes.FileOpCreateFile}}
	rename := []volumetypes.WorkspaceFileChange{{Operation: volumetypes.FileOpRename}}
	remove := []volumetypes.WorkspaceFileChange{{Operation: volumetypes.FileOpDelete}}
	restore := []volumetypes.WorkspaceFileChange{{Operation: volumetypes.FileOpRestoreFile}}

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
