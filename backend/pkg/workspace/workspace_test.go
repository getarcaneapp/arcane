package workspace

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	acfstypes "kit.getarcane.app/acfs/types"

	"getarcane.dev/types/v3"
)

func TestMaxFileSizeBytes(t *testing.T) {
	require.Equal(t, int64(DefaultMaxFileSizeMB*1024*1024), MaxFileSizeBytes(0))
	require.Equal(t, int64(DefaultMaxFileSizeMB*1024*1024), MaxFileSizeBytes(-1))
	require.Equal(t, int64(24*1024*1024), MaxFileSizeBytes(24))
}

func TestValidateContentSize(t *testing.T) {
	limit := int64(4)
	require.NoError(t, ValidateContentSize([]byte("text"), limit))
	require.NoError(t, ValidateContentSize([]byte{0xff, 0, 0x1f}, limit))
	require.ErrorContains(t, ValidateContentSize([]byte("large"), limit), "file exceeds")
}

func TestIsTextContent(t *testing.T) {
	require.True(t, IsTextContent([]byte("hello\n")))
	require.False(t, IsTextContent([]byte{0xff}))
	require.False(t, IsTextContent([]byte("hello\x00world")))
}

func TestValidateContentSizeUsesDynamicMiBLimit(t *testing.T) {
	limit := int64(2 * 1024 * 1024)
	err := ValidateContentSize([]byte(strings.Repeat("a", int(limit+1))), limit)
	require.ErrorContains(t, err, "2 MiB")
}

func TestValidateUploadIndices(t *testing.T) {
	valid := []UploadReference{
		{Operation: "create_file", UploadIndex: new(1)},
		{Operation: "create_folder"},
		{Operation: "update_file", UploadIndex: new(0)},
	}
	require.NoError(t, ValidateUploadIndices(valid, 2, "create_file", "update_file"))

	tests := []struct {
		name        string
		message     string
		changes     []UploadReference
		uploadCount int
	}{
		{name: "missing", changes: []UploadReference{{Operation: "create_file"}}, uploadCount: 0, message: "required"},
		{name: "unexpected", changes: []UploadReference{{Operation: "delete", UploadIndex: new(0)}}, uploadCount: 1, message: "not allowed"},
		{name: "negative", changes: []UploadReference{{Operation: "create_file", UploadIndex: new(-1)}}, uploadCount: 1, message: "out of range"},
		{name: "out of range", changes: []UploadReference{{Operation: "create_file", UploadIndex: new(1)}}, uploadCount: 1, message: "out of range"},
		{
			name: "duplicate",
			changes: []UploadReference{
				{
					Operation:   "create_file",
					UploadIndex: new(0),
				},
				{
					Operation:   "update_file",
					UploadIndex: new(0),
				},
			},
			uploadCount: 1,
			message:     "duplicated",
		},
		{name: "unused", changes: []UploadReference{{Operation: "create_file", UploadIndex: new(0)}}, uploadCount: 2, message: "unused"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateUploadIndices(test.changes, test.uploadCount, "create_file", "update_file")
			require.ErrorContains(t, err, test.message)
		})
	}
}

func TestValidateUpdateManifest(t *testing.T) {
	require.NoError(t, ValidateUpdateManifest(1, 500))
	require.ErrorContains(t, ValidateUpdateManifest(0, 500), "between 1 and 500")
	require.ErrorContains(t, ValidateUpdateManifest(501, 500), "between 1 and 500")
}

func TestEntryFromACFS(t *testing.T) {
	directory := EntryFromACFS(acfstypes.Entry{Name: "config", Path: "/config", Mode: "drwxr-xr-x", Size: 4096, IsDirectory: true}, 4)
	require.Equal(t, "config", directory.RelativePath)
	require.Zero(t, directory.Size)
	require.True(t, directory.Editable)

	text := EntryFromACFS(acfstypes.Entry{Name: "a.txt", Path: "/config/a.txt", Mode: "-rw-r--r--", Size: 4}, 4)
	require.Equal(t, "config/a.txt", text.RelativePath)
	require.True(t, text.Editable)

	large := EntryFromACFS(acfstypes.Entry{Name: "b.bin", Path: "/b.bin", Mode: "-rw-r--r--", Size: 5}, 4)
	require.False(t, large.Editable)
	require.Equal(t, types.FileReadOnlyTooLarge, large.ReadOnlyReason)

	link := EntryFromACFS(acfstypes.Entry{Name: "link", Path: "/link", Mode: "Lrwxrwxrwx", IsSymlink: true}, 4)
	require.Equal(t, types.FileReadOnlySymlink, link.ReadOnlyReason)

	pipe := EntryFromACFS(acfstypes.Entry{Name: "pipe", Path: "/pipe", Mode: "prw-r--r--"}, 4)
	require.Equal(t, types.FileReadOnlySpecial, pipe.ReadOnlyReason)
}

func TestCompareEntriesListsFoldersFirst(t *testing.T) {
	entries := []types.WorkspaceFileEntry{
		{RelativePath: "b.txt"},
		{RelativePath: "z", IsDirectory: true},
		{RelativePath: "a.txt"},
		{RelativePath: "c", IsDirectory: true},
	}
	slices.SortFunc(entries, CompareEntries)
	require.Equal(t, []types.WorkspaceFileEntry{
		{RelativePath: "c", IsDirectory: true},
		{RelativePath: "z", IsDirectory: true},
		{RelativePath: "a.txt"},
		{RelativePath: "b.txt"},
	}, entries)
}
