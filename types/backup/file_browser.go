package backup

// BackupFileEntry describes one file or directory under a backup's logical
// restore root.
type BackupFileEntry struct {
	Path        string `json:"path"`
	Name        string `json:"name"`
	IsDirectory bool   `json:"isDirectory"`
}

// BackupFilePage is one deterministic page of backup browser entries.
type BackupFilePage struct {
	Entries   []BackupFileEntry `json:"entries"`
	NextStart *int              `json:"nextStart,omitempty"`
}

// BrowseBackupFilesRequest selects a folder or a repository-wide search page.
type BrowseBackupFilesRequest struct {
	Path   string `json:"path,omitempty"`
	Search string `json:"search,omitempty"`
	Start  int    `json:"start,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// RestoreSelection selects explicit paths or every eligible backup entry.
type RestoreSelection struct {
	Paths     []string `json:"paths,omitempty"`
	SelectAll bool     `json:"selectAll,omitempty"`
	Search    string   `json:"search,omitempty"`
}
