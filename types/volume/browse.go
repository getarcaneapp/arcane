package volume

import "time"

type FileEntry struct {
	ModTime      time.Time `json:"modTime" doc:"Last modification time"`
	Name         string    `json:"name" doc:"Name of the file or directory"`
	Path         string    `json:"path" doc:"Full path to the file"`
	RelativePath string    `json:"relativePath,omitempty" doc:"Path relative to the volume root"`
	Mode         string    `json:"mode" doc:"File mode/permissions"`
	LinkTarget   string    `json:"linkTarget,omitempty" doc:"Target of the symbolic link"`
	Size         int64     `json:"size" doc:"Size of the file in bytes"`
	IsDirectory  bool      `json:"isDirectory" doc:"Whether this entry is a directory"`
	IsSymlink    bool      `json:"isSymlink" doc:"Whether this entry is a symbolic link"`
}

const (
	FileOpCreateFile   = "create_file"
	FileOpCreateFolder = "create_folder"
	FileOpUpdateFile   = "update_file"
	FileOpRename       = "rename"
	FileOpMove         = "move"
	FileOpDelete       = "delete"
	FileOpRestoreFile  = "restore_file"
)

const (
	FileReadOnlyBinary   = "binary"
	FileReadOnlyTooLarge = "too_large"
	FileReadOnlySymlink  = "symlink"
	FileReadOnlySpecial  = "special"
)

type FileChange struct {
	Operation     string  `json:"operation" binding:"required" enum:"create_file,create_folder,update_file,rename,move,delete,restore_file"`
	RelativePath  string  `json:"relativePath" binding:"required"`
	NewName       string  `json:"newName,omitempty"`
	NewParentPath string  `json:"newParentPath,omitempty"`
	Content       *string `json:"content,omitempty"`
	UploadIndex   *int    `json:"uploadIndex,omitempty" minimum:"0"`
	BackupID      string  `json:"backupId,omitempty"`
	Recursive     bool    `json:"recursive,omitempty"`
}

type FileUpdateManifest struct {
	FileTreeRevision string       `json:"fileTreeRevision" binding:"required"`
	FileChanges      []FileChange `json:"fileChanges" binding:"required" minItems:"1" maxItems:"500"`
}

type Workspace struct {
	Files             []FileEntry `json:"files"`
	FileTreeRevision  string      `json:"fileTreeRevision"`
	FileTreeTruncated bool        `json:"fileTreeTruncated"`
	ActivityID        *string     `json:"activityId,omitempty"`
}

type WorkspaceFileContent struct {
	Path           string `json:"path"`
	RelativePath   string `json:"relativePath"`
	Name           string `json:"name"`
	Content        string `json:"content,omitempty"`
	MimeType       string `json:"mimeType"`
	Size           int64  `json:"size"`
	Editable       bool   `json:"editable"`
	ReadOnlyReason string `json:"readOnlyReason,omitempty" enum:"binary,too_large,symlink,special"`
}

type FileMetadata struct {
	FileEntry

	MimeType string `json:"mimeType" doc:"MIME type of the file"`
	IsText   bool   `json:"isText" doc:"Whether the file is a text file"`
	IsBinary bool   `json:"isBinary" doc:"Whether the file is a binary file"`
}
