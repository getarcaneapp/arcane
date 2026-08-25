package backup

// BackupFileEntry describes one file or directory under a backup's logical
// restore root.
type BackupFileEntry struct {
	Path        string `json:"path"`
	Name        string `json:"name"`
	IsDirectory bool   `json:"isDirectory"`
}

// RestoreSelection selects explicit paths or every eligible backup entry.
type RestoreSelection struct {
	Paths     []string `json:"paths,omitempty"`
	SelectAll bool     `json:"selectAll,omitempty"`
	Search    string   `json:"search,omitempty"`
}
