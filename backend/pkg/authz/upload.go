package authz

import uploadtypes "github.com/getarcaneapp/arcane/types/v2/upload"

// UploadKindPermission maps an upload-session kind to the permission required
// to create, write to, inspect, or delete sessions of that kind. The boolean
// is false for unknown kinds so callers (and remote proxies) fail closed.
func UploadKindPermission(kind string) (string, bool) {
	switch kind {
	case uploadtypes.KindImage:
		return PermImagesUpload, true
	case uploadtypes.KindVolumeBackup:
		return PermVolumesUpload, true
	case uploadtypes.KindBuildWorkspace:
		return PermBuildWorkspacesManage, true
	}
	return "", false
}
