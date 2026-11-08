package workspace

import (
	"strings"
	"time"

	"kit.getarcane.app"
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

func EntryFromACFS(entry acfstypes.Entry, maxFileSizeBytes int64) types.WorkspaceFileEntry {
	fileEntry := FileEntry(entry)
	fileEntry.RelativePath = strings.TrimPrefix(entry.Path, "/")
	switch {
	case entry.IsDirectory:
		fileEntry.Size = 0
		fileEntry.Editable = true
	case entry.IsSymlink:
		fileEntry.ReadOnlyReason = types.FileReadOnlySymlink
	case strings.HasPrefix(entry.Mode, "-"):
		if entry.Size > maxFileSizeBytes {
			fileEntry.ReadOnlyReason = types.FileReadOnlyTooLarge
		} else {
			fileEntry.Editable = true
		}
	default:
		fileEntry.ReadOnlyReason = types.FileReadOnlySpecial
	}
	return fileEntry
}

func CompareEntries(a, b types.WorkspaceFileEntry) int {
	if a.IsDirectory != b.IsDirectory {
		return kit.Ternary(a.IsDirectory, -1, 1)
	}
	return strings.Compare(a.RelativePath, b.RelativePath)
}
