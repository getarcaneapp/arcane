package workspace

import "time"

const (
	FileOpCreateFile   = "create_file"
	FileOpCreateFolder = "create_folder"
	FileOpUpdateFile   = "update_file"
	FileOpRename       = "rename"
	FileOpMove         = "move"
	FileOpDelete       = "delete"
)

const (
	FileReadOnlyBinary   = "binary"
	FileReadOnlyTooLarge = "too_large"
	FileReadOnlySymlink  = "symlink"
	FileReadOnlySpecial  = "special"
)

type FileEntry struct {
	ModTime        time.Time `json:"modTime" doc:"Last modification time"`
	Name           string    `json:"name" doc:"Name of the file or directory"`
	Path           string    `json:"path" doc:"Full path to the file"`
	RelativePath   string    `json:"relativePath" doc:"Path relative to the workspace root"`
	Mode           string    `json:"mode,omitempty" doc:"File mode/permissions"`
	LinkTarget     string    `json:"linkTarget,omitempty" doc:"Target of the symbolic link"`
	Size           int64     `json:"size" doc:"Size of the file in bytes"`
	IsDirectory    bool      `json:"isDirectory" doc:"Whether this entry is a directory"`
	IsSymlink      bool      `json:"isSymlink" doc:"Whether this entry is a symbolic link"`
	Editable       bool      `json:"editable" doc:"Whether this entry can be edited"`
	ReadOnlyReason string    `json:"readOnlyReason,omitempty" enum:"binary,too_large,symlink,special"`
}

type FileChange struct {
	Operation     string `json:"operation" binding:"required" enum:"create_file,create_folder,update_file,rename,move,delete"`
	RelativePath  string `json:"relativePath" binding:"required"`
	NewName       string `json:"newName,omitempty"`
	NewParentPath string `json:"newParentPath,omitempty"`
	UploadIndex   *int   `json:"uploadIndex,omitempty" minimum:"0"`
	// BaselineIndex references an uploaded copy of the content this
	// update_file change was drafted against. When set, the apply conflicts
	// if the on-disk content no longer matches it, so a concurrent edit to
	// the same file is not silently overwritten. Deliberately optional:
	// imperative clients like the CLI overwrite without ever reading the
	// file (last-write-wins), while the web editor always sends it.
	BaselineIndex *int `json:"baselineIndex,omitempty" minimum:"0"`
	Recursive     bool `json:"recursive,omitempty"`
}

type Workspace struct {
	Files             []FileEntry `json:"files"`
	FileTreeRevision  string      `json:"fileTreeRevision"`
	FileTreeTruncated bool        `json:"fileTreeTruncated"`
	ActivityID        *string     `json:"activityId,omitempty"`
}

type FileContent struct {
	Path           string `json:"path"`
	RelativePath   string `json:"relativePath"`
	Name           string `json:"name"`
	Content        string `json:"content,omitempty"`
	MimeType       string `json:"mimeType"`
	Size           int64  `json:"size"`
	Editable       bool   `json:"editable"`
	ReadOnlyReason string `json:"readOnlyReason,omitempty" enum:"binary,too_large,symlink,special"`
}
