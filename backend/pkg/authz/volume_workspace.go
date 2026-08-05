package authz

import volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"

// VolumeWorkspaceRequiredPermissions returns the distinct mutation permissions
// required by a Volume Workspace manifest. The boolean is false when a change
// uses an unknown operation so remote proxies can fail closed.
func VolumeWorkspaceRequiredPermissions(changes []volumetypes.WorkspaceFileChange) ([]string, bool) {
	required := make([]string, 0, 3)
	seen := make(map[string]struct{}, 3)
	add := func(permission string) {
		if _, exists := seen[permission]; exists {
			return
		}
		seen[permission] = struct{}{}
		required = append(required, permission)
	}

	for _, change := range changes {
		switch change.Operation {
		case volumetypes.FileOpCreateFile, volumetypes.FileOpCreateFolder, volumetypes.FileOpUpdateFile:
			add(PermVolumesUpload)
		case volumetypes.FileOpRename, volumetypes.FileOpMove:
			add(PermVolumesUpload)
			add(PermVolumesDelete)
		case volumetypes.FileOpDelete:
			add(PermVolumesDelete)
		case volumetypes.FileOpRestoreFile:
			add(PermVolumesBackup)
		default:
			return nil, false
		}
	}
	return required, true
}
