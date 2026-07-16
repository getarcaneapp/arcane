package volume

import "time"

type FileEntry struct {
	ModTime     time.Time `json:"modTime" doc:"Last modification time"`
	Name        string    `json:"name" doc:"Name of the file or directory"`
	Path        string    `json:"path" doc:"Full path to the file"`
	Mode        string    `json:"mode" doc:"File mode/permissions"`
	LinkTarget  string    `json:"linkTarget,omitempty" doc:"Target of the symbolic link"`
	Size        int64     `json:"size" doc:"Size of the file in bytes"`
	IsDirectory bool      `json:"isDirectory" doc:"Whether this entry is a directory"`
	IsSymlink   bool      `json:"isSymlink" doc:"Whether this entry is a symbolic link"`
}

type FileMetadata struct {
	FileEntry

	MimeType string `json:"mimeType" doc:"MIME type of the file"`
	IsText   bool   `json:"isText" doc:"Whether the file is a text file"`
	IsBinary bool   `json:"isBinary" doc:"Whether the file is a binary file"`
}
