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
	size, editable, readOnlyReason := entry.Size, false, ""
	switch {
	case entry.IsDirectory:
		size, editable = 0, true
	case entry.IsSymlink:
		readOnlyReason = types.FileReadOnlySymlink
	case !strings.HasPrefix(entry.Mode, "-"):
		readOnlyReason = types.FileReadOnlySpecial
	case entry.Size > maxFileSizeBytes:
		readOnlyReason = types.FileReadOnlyTooLarge
	default:
		editable = true
	}
	return types.WorkspaceFileEntry{
		ModTime:        FileEntry(entry).ModTime,
		Name:           entry.Name,
		Path:           entry.Path,
		RelativePath:   strings.TrimPrefix(entry.Path, "/"),
		Mode:           entry.Mode,
		LinkTarget:     entry.LinkTarget,
		ReadOnlyReason: readOnlyReason,
		Size:           size,
		IsDirectory:    entry.IsDirectory,
		IsSymlink:      entry.IsSymlink,
		Editable:       editable,
	}
}

func CompareEntries(a, b types.WorkspaceFileEntry) int {
	if a.IsDirectory != b.IsDirectory {
		return kit.Ternary(a.IsDirectory, -1, 1)
	}
	return strings.Compare(a.RelativePath, b.RelativePath)
}
