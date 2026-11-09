package types

import (
	"time"
)

type (
	WorkspaceFileEntry struct {
		ModTime        time.Time `json:"modTime" doc:"Last modification time"`
		Name           string    `json:"name" doc:"Name of the file or directory"`
		Path           string    `json:"path" doc:"Full path to the file"`
		RelativePath   string    `json:"relativePath" doc:"Path relative to the workspace root"`
		Mode           string    `json:"mode,omitempty" doc:"File mode/permissions"`
		LinkTarget     string    `json:"linkTarget,omitempty" doc:"Target of the symbolic link"`
		ReadOnlyReason string    `json:"readOnlyReason,omitempty" enum:"binary,too_large,symlink,special,gitops_managed"`
		Size           int64     `json:"size" doc:"Size of the file in bytes"`
		IsDirectory    bool      `json:"isDirectory" doc:"Whether this entry is a directory"`
		IsSymlink      bool      `json:"isSymlink" doc:"Whether this entry is a symbolic link"`
		Editable       bool      `json:"editable" doc:"Whether this entry can be edited"`
	}

	WorkspaceFileChange struct {
		UploadIndex   *int   `json:"uploadIndex,omitempty" minimum:"0"`
		BaselineIndex *int   `json:"baselineIndex,omitempty" minimum:"0"`
		Operation     string `json:"operation" binding:"required" enum:"create_file,create_folder,update_file,rename,move,delete"`
		RelativePath  string `json:"relativePath" binding:"required"`
		NewName       string `json:"newName,omitempty"`
		NewParentPath string `json:"newParentPath,omitempty"`
		Recursive     bool   `json:"recursive,omitempty"`
	}

	Workspace struct {
		ActivityID        *string              `json:"activityId,omitempty"`
		FileTreeRevision  string               `json:"fileTreeRevision"`
		Files             []WorkspaceFileEntry `json:"files"`
		FileTreeTruncated bool                 `json:"fileTreeTruncated"`
	}

	WorkspaceUploadRequest struct {
		UploadID   string `json:"uploadId" required:"true" doc:"ID of a complete upload session"`
		ParentPath string `json:"parentPath,omitempty" doc:"Folder relative to the workspace root that receives the file"`
	}

	WorkspaceUpdateManifest struct {
		FileTreeRevision string                `json:"fileTreeRevision" binding:"required"`
		FileChanges      []WorkspaceFileChange `json:"fileChanges" binding:"required" minItems:"1" maxItems:"500"`
	}

	WorkspaceFileContent struct {
		Path           string `json:"path"`
		RelativePath   string `json:"relativePath"`
		Name           string `json:"name"`
		Content        string `json:"content,omitempty"`
		MimeType       string `json:"mimeType"`
		ReadOnlyReason string `json:"readOnlyReason,omitempty" enum:"binary,too_large,symlink,special,gitops_managed"`
		Size           int64  `json:"size"`
		Editable       bool   `json:"editable"`
	}
)

const (
	FileOpCreateFile          = "create_file"
	FileOpCreateFolder        = "create_folder"
	FileOpUpdateFile          = "update_file"
	FileOpRename              = "rename"
	FileOpMove                = "move"
	FileOpDelete              = "delete"
	FileReadOnlyBinary        = "binary"
	FileReadOnlyTooLarge      = "too_large"
	FileReadOnlySymlink       = "symlink"
	FileReadOnlySpecial       = "special"
	FileReadOnlyGitOpsManaged = "gitops_managed"
)
