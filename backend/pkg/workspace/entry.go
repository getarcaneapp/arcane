package workspace

import (
	"time"

	acfstypes "kit.getarcane.app/acfs/types"

	"getarcane.dev/types/v3"
)

func FileEntry(entry acfstypes.Entry) types.WorkspaceFileEntry {
	modTime := entry.ModTime
	if entry.ModTimeUnixNano != 0 {
		modTime = time.Unix(0, entry.ModTimeUnixNano)
	}

	return types.WorkspaceFileEntry{
		ModTime:      modTime,
		Name:         entry.Name,
		Path:         entry.Path,
		RelativePath: entry.Path,
		Mode:         entry.Mode,
		LinkTarget:   entry.LinkTarget,
		Size:         entry.Size,
		IsDirectory:  entry.IsDirectory,
		IsSymlink:    entry.IsSymlink,
	}
}
