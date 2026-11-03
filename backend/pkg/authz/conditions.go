package authz

import (
	"slices"

	"getarcane.dev/types/v3"
)

const (
	ConditionInQuery    = "query"
	ConditionInPath     = "path"
	ConditionInBody     = "body"
	ConditionInMessage  = "message"
	ConditionInResource = "resource"
)

type (
	PermissionCondition struct {
		In         string `json:"in"`
		Field      string `json:"field"`
		Value      string `json:"value"`
		Permission string `json:"permission"`
	}

	PermissionConditions []PermissionCondition
)

var (
	BuildPushConditions = PermissionConditions{
		{
			In:         ConditionInBody,
			Field:      "push",
			Value:      "true",
			Permission: PermImagesPush,
		},
	}

	GitOpsPreDeployConditions = PermissionConditions{
		{
			In:         ConditionInBody,
			Field:      "preDeploy",
			Value:      "true",
			Permission: PermGitOpsLifecycle,
		},
	}

	GitOpsModeConditions = PermissionConditions{
		{
			In:         ConditionInBody,
			Field:      "mode",
			Value:      types.GitOpsSyncModeBackup,
			Permission: PermGitOpsBackup,
		},
	}

	GitOpsSyncModeConditions = PermissionConditions{
		{
			In:         ConditionInResource,
			Field:      "mode",
			Value:      types.GitOpsSyncModeBackup,
			Permission: PermGitOpsBackup,
		},
	}

	UploadKindConditions = PermissionConditions{
		{
			In:         ConditionInPath,
			Field:      "kind",
			Value:      types.UploadKindImage,
			Permission: PermImagesUpload,
		},
		{
			In:         ConditionInPath,
			Field:      "kind",
			Value:      types.UploadKindVolumeBackup,
			Permission: PermVolumesUpload,
		},
		{
			In:         ConditionInPath,
			Field:      "kind",
			Value:      types.UploadKindBuildWorkspace,
			Permission: PermBuildWorkspacesCreate,
		},
	}

	VolumeWorkspaceConditions = PermissionConditions{
		{
			In:         ConditionInBody,
			Field:      "fileChanges.operation",
			Value:      types.FileOpCreateFile,
			Permission: PermVolumesUpload,
		},
		{
			In:         ConditionInBody,
			Field:      "fileChanges.operation",
			Value:      types.FileOpCreateFolder,
			Permission: PermVolumesUpload,
		},
		{
			In:         ConditionInBody,
			Field:      "fileChanges.operation",
			Value:      types.FileOpUpdateFile,
			Permission: PermVolumesUpload,
		},
		{
			In:         ConditionInBody,
			Field:      "fileChanges.operation",
			Value:      types.FileOpRename,
			Permission: PermVolumesUpload,
		},
		{
			In:         ConditionInBody,
			Field:      "fileChanges.operation",
			Value:      types.FileOpRename,
			Permission: PermVolumesDelete,
		},
		{
			In:         ConditionInBody,
			Field:      "fileChanges.operation",
			Value:      types.FileOpMove,
			Permission: PermVolumesUpload,
		},
		{
			In:         ConditionInBody,
			Field:      "fileChanges.operation",
			Value:      types.FileOpMove,
			Permission: PermVolumesDelete,
		},
		{
			In:         ConditionInBody,
			Field:      "fileChanges.operation",
			Value:      types.FileOpDelete,
			Permission: PermVolumesDelete,
		},
		{
			In:         ConditionInBody,
			Field:      "fileChanges.operation",
			Value:      types.FileOpRestoreFile,
			Permission: PermVolumesBackup,
		},
	}
)

func (c PermissionConditions) Required(field, value string) []string {
	var perms []string
	for _, condition := range c {
		if condition.Field == field && condition.Value == value && !slices.Contains(perms, condition.Permission) {
			perms = append(perms, condition.Permission)
		}
	}
	return perms
}
