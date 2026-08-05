package volume

import workspacetypes "github.com/getarcaneapp/arcane/types/v2/workspace"

const (
	FileOpCreateFile   = workspacetypes.FileOpCreateFile
	FileOpCreateFolder = workspacetypes.FileOpCreateFolder
	FileOpUpdateFile   = workspacetypes.FileOpUpdateFile
	FileOpRename       = workspacetypes.FileOpRename
	FileOpMove         = workspacetypes.FileOpMove
	FileOpDelete       = workspacetypes.FileOpDelete
	FileOpRestoreFile  = "restore_file"
)

type WorkspaceFileChange struct {
	Operation     string `json:"operation" binding:"required" enum:"create_file,create_folder,update_file,rename,move,delete,restore_file"`
	RelativePath  string `json:"relativePath" binding:"required"`
	NewName       string `json:"newName,omitempty"`
	NewParentPath string `json:"newParentPath,omitempty"`
	UploadIndex   *int   `json:"uploadIndex,omitempty" minimum:"0"`
	BackupID      string `json:"backupId,omitempty"`
	Recursive     bool   `json:"recursive,omitempty"`
}

type WorkspaceUpdateManifest struct {
	FileTreeRevision string                `json:"fileTreeRevision" binding:"required"`
	FileChanges      []WorkspaceFileChange `json:"fileChanges" binding:"required" minItems:"1" maxItems:"500"`
}
