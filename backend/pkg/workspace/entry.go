package workspace

import (
	"strings"
	"time"

	"kit.getarcane.app"
	acfstypes "kit.getarcane.app/acfs/types"

	"getarcane.dev/types/v3"
)

func EntryFromACFS(entry acfstypes.Entry, maxFileSizeBytes int64) types.WorkspaceFileEntry {
	readOnlyReason := func() string {
		switch {
		case entry.IsDirectory:
			return ""
		case entry.IsSymlink:
			return types.FileReadOnlySymlink
		case !strings.HasPrefix(entry.Mode, "-"):
			return types.FileReadOnlySpecial
		case entry.Size > maxFileSizeBytes:
			return types.FileReadOnlyTooLarge
		}
		return ""
	}()
	return types.WorkspaceFileEntry{
		ModTime:        kit.Ternary(entry.ModTimeUnixNano != 0, time.Unix(0, entry.ModTimeUnixNano), entry.ModTime),
		Name:           entry.Name,
		Path:           entry.Path,
		RelativePath:   strings.TrimPrefix(entry.Path, "/"),
		Mode:           entry.Mode,
		LinkTarget:     entry.LinkTarget,
		ReadOnlyReason: readOnlyReason,
		Size:           kit.Ternary(entry.IsDirectory, 0, entry.Size),
		IsDirectory:    entry.IsDirectory,
		IsSymlink:      entry.IsSymlink,
		Editable:       readOnlyReason == "",
	}
}

func CompareEntries(a, b types.WorkspaceFileEntry) int {
	if a.IsDirectory != b.IsDirectory {
		return kit.Ternary(a.IsDirectory, -1, 1)
	}
	return strings.Compare(a.RelativePath, b.RelativePath)
}
