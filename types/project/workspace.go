package project

import workspacetypes "github.com/getarcaneapp/arcane/types/v2/workspace"

const (
	FileOpCreateFile   = workspacetypes.FileOpCreateFile
	FileOpCreateFolder = workspacetypes.FileOpCreateFolder
	FileOpUpdateFile   = workspacetypes.FileOpUpdateFile
	FileOpRename       = workspacetypes.FileOpRename
	FileOpMove         = workspacetypes.FileOpMove
	FileOpDelete       = workspacetypes.FileOpDelete
)

type WorkspaceFileChange = workspacetypes.FileChange

type WorkspaceUpdateManifest struct {
	FileTreeRevision string                `json:"fileTreeRevision" binding:"required"`
	FileChanges      []WorkspaceFileChange `json:"fileChanges" binding:"required" minItems:"1" maxItems:"500"`
}

type CreateProjectWorkspaceManifest struct {
	FileChanges []WorkspaceFileChange `json:"fileChanges" binding:"required" maxItems:"500"`
}
