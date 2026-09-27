package authz

import (
	volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"
	kit "go.getarcane.app/kit/pkg"
)

// VolumeWorkspaceRequiredPermissions returns the distinct mutation permissions
// required by a Volume Workspace manifest. The boolean is false when a change
// uses an unknown operation so remote proxies can fail closed.
func VolumeWorkspaceRequiredPermissions(changes []volumetypes.WorkspaceFileChange) ([]string, bool) {
	required := make([]string, 0, 3)

	for _, change := range changes {
		switch change.Operation {
		case volumetypes.FileOpCreateFile, volumetypes.FileOpCreateFolder, volumetypes.FileOpUpdateFile:
			required = append(required, PermVolumesUpload)
		case volumetypes.FileOpRename, volumetypes.FileOpMove:
			required = append(required, PermVolumesUpload)
			required = append(required, PermVolumesDelete)
		case volumetypes.FileOpDelete:
			required = append(required, PermVolumesDelete)
		case volumetypes.FileOpRestoreFile:
			required = append(required, PermVolumesBackup)
		default:
			return nil, false
		}
	}
	return kit.Unique(required), true
}
