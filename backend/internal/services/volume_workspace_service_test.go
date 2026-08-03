package services

import (
	"archive/tar"
	"errors"
	"mime/multipart"
	"strings"
	"testing"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/stretchr/testify/require"
)

func volumeWorkspaceTreeRecordInternal(relativePath, kind, size, modTime, mode, linkTarget string) string {
	return strings.Join([]string{relativePath, kind, size, modTime, mode, linkTarget}, "\x00") + "\x00"
}

func volumeWorkspaceTreeOutputInternal(truncated bool, records string) string {
	marker := "0"
	if truncated {
		marker = "1"
	}
	return records + "ARCANE_TREE_END\x00" + marker + "\x00"
}

func TestParseVolumeWorkspaceTreeInternalSortsAndPreservesSymlinks(t *testing.T) {
	stdout := volumeWorkspaceTreeOutputInternal(false,
		volumeWorkspaceTreeRecordInternal("z.txt", "f", "4", "1700000000.25", "-rw-r--r--", "")+
			volumeWorkspaceTreeRecordInternal("folder/link", "l", "7", "1700000001.5", "lrwxrwxrwx", "../z.txt")+
			volumeWorkspaceTreeRecordInternal("folder", "d", "4096", "1700000002", "drwxr-xr-x", ""))

	workspace, err := parseVolumeWorkspaceTreeInternal(stdout, 10)
	require.NoError(t, err)
	require.False(t, workspace.FileTreeTruncated)
	require.Equal(t, []string{"folder", "folder/link", "z.txt"}, []string{
		workspace.Files[0].RelativePath,
		workspace.Files[1].RelativePath,
		workspace.Files[2].RelativePath,
	})
	require.Zero(t, workspace.Files[0].Size)
	require.True(t, workspace.Files[1].IsSymlink)
	require.Equal(t, "../z.txt", workspace.Files[1].LinkTarget)
	require.NotEmpty(t, workspace.FileTreeRevision)

	reordered, err := parseVolumeWorkspaceTreeInternal(volumeWorkspaceTreeOutputInternal(false,
		volumeWorkspaceTreeRecordInternal("folder", "d", "4096", "1700000002", "drwxr-xr-x", "")+
			volumeWorkspaceTreeRecordInternal("z.txt", "f", "4", "1700000000.25", "-rw-r--r--", "")+
			volumeWorkspaceTreeRecordInternal("folder/link", "l", "7", "1700000001.5", "lrwxrwxrwx", "../z.txt")), 10)
	require.NoError(t, err)
	require.Equal(t, workspace.FileTreeRevision, reordered.FileTreeRevision)
}

func TestParseVolumeWorkspaceTreeInternalReportsBothLimits(t *testing.T) {
	entries := volumeWorkspaceTreeRecordInternal("b", "f", "1", "1", "-rw-r--r--", "") +
		volumeWorkspaceTreeRecordInternal("a", "f", "1", "1", "-rw-r--r--", "")

	depthLimited, err := parseVolumeWorkspaceTreeInternal(volumeWorkspaceTreeOutputInternal(true, entries), 10)
	require.NoError(t, err)
	require.True(t, depthLimited.FileTreeTruncated)

	entryLimited, err := parseVolumeWorkspaceTreeInternal(volumeWorkspaceTreeOutputInternal(false, entries), 1)
	require.NoError(t, err)
	require.True(t, entryLimited.FileTreeTruncated)
	require.Len(t, entryLimited.Files, 1)
	require.Equal(t, "a", entryLimited.Files[0].RelativePath)

	_, err = parseVolumeWorkspaceTreeInternal(entries+"invalid\x000\x00", 10)
	require.Error(t, err)
}

func TestParseVolumeWorkspaceModTimeInternalPreservesNanoseconds(t *testing.T) {
	modTime, err := parseVolumeWorkspaceModTimeInternal("2026-08-02 16:09:00.123456789 +0000")
	require.NoError(t, err)
	require.Equal(t, 123456789, modTime.Nanosecond())
}

func TestParseVolumeWorkspaceTreeInternalPreservesUnusualNamesAndSpecialFiles(t *testing.T) {
	workspace, err := parseVolumeWorkspaceTreeInternal(volumeWorkspaceTreeOutputInternal(false,
		volumeWorkspaceTreeRecordInternal(".hidden", "f", "1", "1", "-rw-r--r--", "")+
			volumeWorkspaceTreeRecordInternal("line\nbreak", "s", "0", "2", "prw-------", "")), 10)
	require.NoError(t, err)
	require.Equal(t, []string{".hidden", "line\nbreak"}, []string{
		workspace.Files[0].RelativePath,
		workspace.Files[1].RelativePath,
	})
	require.NotEmpty(t, workspace.FileTreeRevision)
}

func TestVolumeWorkspaceFileContentResponseInternal(t *testing.T) {
	text := []byte("hello")
	below, err := volumeWorkspaceFileContentResponseInternal("notes.txt", "regular", maxEditableVolumeFileBytes-1, text)
	require.NoError(t, err)
	require.True(t, below.Editable)
	require.Equal(t, "hello", below.Content)

	atLimit, err := volumeWorkspaceFileContentResponseInternal("notes.txt", "regular", maxEditableVolumeFileBytes, text)
	require.NoError(t, err)
	require.True(t, atLimit.Editable)

	above, err := volumeWorkspaceFileContentResponseInternal("notes.txt", "regular", maxEditableVolumeFileBytes+1, text)
	require.NoError(t, err)
	require.False(t, above.Editable)
	require.Equal(t, volumetypes.FileReadOnlyTooLarge, above.ReadOnlyReason)

	binary, err := volumeWorkspaceFileContentResponseInternal("data.bin", "regular", 2, []byte{0xff, 0x00})
	require.NoError(t, err)
	require.Equal(t, volumetypes.FileReadOnlyBinary, binary.ReadOnlyReason)

	special, err := volumeWorkspaceFileContentResponseInternal("pipe", "special", 0, nil)
	require.NoError(t, err)
	require.Equal(t, volumetypes.FileReadOnlySpecial, special.ReadOnlyReason)
}

func TestValidateVolumeDownloadHeaderInternalRejectsUnsafeEntries(t *testing.T) {
	require.NoError(t, validateVolumeDownloadHeaderInternal(&tar.Header{Typeflag: tar.TypeReg, Mode: 0o644}))
	require.EqualError(t, validateVolumeDownloadHeaderInternal(&tar.Header{Typeflag: tar.TypeDir, Mode: 0o755}), "path is a directory")
	require.EqualError(t, validateVolumeDownloadHeaderInternal(&tar.Header{Typeflag: tar.TypeSymlink, Mode: 0o777}), "symlink downloads are not supported")
	require.EqualError(t, validateVolumeDownloadHeaderInternal(&tar.Header{Typeflag: tar.TypeFifo, Mode: 0o644}), "path is not a regular file")
}

func TestValidateVolumeFileChangeInternalOperationsAndMultipartMapping(t *testing.T) {
	empty := ""
	first := 0
	second := 1
	uploads := []*multipart.FileHeader{{Filename: "first.bin"}, {Filename: "second.bin"}}
	tests := []volumetypes.FileChange{
		{Operation: volumetypes.FileOpCreateFile, RelativePath: "empty.txt", Content: &empty},
		{Operation: volumetypes.FileOpUpdateFile, RelativePath: "first.bin", UploadIndex: &first},
		{Operation: volumetypes.FileOpCreateFile, RelativePath: "second.bin", UploadIndex: &second},
		{Operation: volumetypes.FileOpCreateFolder, RelativePath: "folder"},
		{Operation: volumetypes.FileOpRename, RelativePath: "old.txt", NewName: "new.txt"},
		{Operation: volumetypes.FileOpMove, RelativePath: "new.txt", NewParentPath: "folder"},
		{Operation: volumetypes.FileOpDelete, RelativePath: "folder", Recursive: true},
		{Operation: volumetypes.FileOpRestoreFile, RelativePath: "restored.txt", BackupID: "backup-id"},
	}
	for _, change := range tests {
		require.NoError(t, validateVolumeFileChangeInternal(change, uploads), change.Operation)
	}

	require.Error(t, validateVolumeFileChangeInternal(volumetypes.FileChange{
		Operation: volumetypes.FileOpCreateFile, RelativePath: "both.txt", Content: &empty, UploadIndex: &first,
	}, uploads))
	invalidIndex := 2
	require.Error(t, validateVolumeFileChangeInternal(volumetypes.FileChange{
		Operation: volumetypes.FileOpUpdateFile, RelativePath: "missing.bin", UploadIndex: &invalidIndex,
	}, uploads))
	require.Error(t, validateVolumeFileChangeInternal(volumetypes.FileChange{
		Operation: volumetypes.FileOpRestoreFile, RelativePath: "file.txt",
	}, uploads))
	require.Error(t, validateVolumeFileChangeInternal(volumetypes.FileChange{
		Operation: "unknown", RelativePath: "file.txt",
	}, uploads))
	require.Error(t, validateVolumeFileChangeInternal(volumetypes.FileChange{
		Operation: volumetypes.FileOpDelete, RelativePath: "../escape",
	}, uploads))
}

func TestVolumeWorkspaceBackupScopeInternalCoversSourcesAndDestinations(t *testing.T) {
	changes := []volumetypes.FileChange{
		{Operation: volumetypes.FileOpRename, RelativePath: "docs/a.txt", NewName: "b.txt"},
		{Operation: volumetypes.FileOpMove, RelativePath: "cache", NewParentPath: "docs"},
		{Operation: volumetypes.FileOpCreateFile, RelativePath: "docs/a.txt/child", Content: new(string)},
	}
	scope, err := volumeWorkspaceBackupScopeInternal(changes)
	require.NoError(t, err)
	require.Equal(t, []string{"cache", "docs/a.txt", "docs/b.txt", "docs/cache"}, scope)
}

func TestNormalizeVolumeWorkspaceScopeInternalCollapsesAbsentAncestors(t *testing.T) {
	require.Equal(t, []string{"cache", "docs"}, normalizeVolumeWorkspaceScopeInternal([]string{
		"docs/b.txt",
		"docs",
		"cache/item",
		"cache",
		"docs/a.txt",
		"cache",
	}))
}

func TestVolumeWorkspaceRollbackPathsInternalRemovesDeepestFirst(t *testing.T) {
	backup := &volumeWorkspaceBackupInternal{
		archives: []volumeWorkspaceBackupArchiveInternal{
			{relativePath: "docs/file.txt", archivePath: "/tmp/file.tar"},
			{relativePath: "cache", archivePath: "/tmp/cache.tar"},
		},
		absentEntries: []string{"docs/new/deep", "other"},
	}
	require.Equal(t, []string{"docs/new/deep", "docs/file.txt", "other", "cache"}, volumeWorkspaceRollbackPathsInternal(backup))
}

func TestVolumeWorkspaceHelperScriptsUseSupportedTooling(t *testing.T) {
	scripts := strings.Join([]string{
		volumeWorkspaceTreeScriptInternal,
		volumeWorkspaceBackupInspectScriptInternal,
		volumeWorkspaceCreateFileScriptInternal,
		volumeWorkspaceCreateFolderScriptInternal,
		volumeWorkspaceUpdateFileScriptInternal,
		volumeWorkspaceRenameScriptInternal,
		volumeWorkspaceMoveScriptInternal,
		volumeWorkspaceDeleteScriptInternal,
	}, "\n")
	for _, unsupported := range []string{
		"find -P",
		"-printf",
		"sort -z",
		"xargs",
		"cat --",
		"dirname",
		"install ",
		"--files-from",
		"tar -r",
		"$((",
	} {
		require.NotContains(t, scripts, unsupported)
	}
	require.Contains(t, volumeWorkspaceCreateFileScriptInternal, "head -c")
	require.Contains(t, volumeWorkspaceCreateFolderScriptInternal, "mkdir -m 0755")
	require.Contains(t, volumeWorkspaceUpdateFileScriptInternal, "head -c")
}

func TestClassifyVolumeWorkspaceExecErrorInternal(t *testing.T) {
	base := errors.New("exit status 1")
	require.ErrorIs(t, classifyVolumeWorkspaceExecErrorInternal(base, "ARCANE_SYMLINK", "execute"), common.ErrVolumeFileForbidden)
	require.ErrorIs(t, classifyVolumeWorkspaceExecErrorInternal(base, "ARCANE_NOT_FOUND", "execute"), common.ErrVolumeFileNotFound)
	require.ErrorIs(t, classifyVolumeWorkspaceExecErrorInternal(base, "ARCANE_COLLISION", "execute"), common.ErrVolumeFileBadRequest)
	require.ErrorIs(t, classifyVolumeWorkspaceExecErrorInternal(base, "unexpected", "execute"), base)
}

func TestClassifyVolumeWorkspaceBrowseErrorInternal(t *testing.T) {
	require.ErrorIs(t, classifyVolumeWorkspaceBrowseErrorInternal(cerrdefs.ErrNotFound), common.ErrVolumeFileNotFound)
	require.ErrorIs(t, classifyVolumeWorkspaceBrowseErrorInternal(errors.New("volume uses a custom mount configuration")), common.ErrVolumeFileBadRequest)
	unexpected := errors.New("docker unavailable")
	require.ErrorIs(t, classifyVolumeWorkspaceBrowseErrorInternal(unexpected), unexpected)
}

func TestValidateVolumeWorkspaceRevisionInternal(t *testing.T) {
	require.NoError(t, validateVolumeWorkspaceRevisionInternal(" revision ", "revision"))
	require.ErrorIs(t, validateVolumeWorkspaceRevisionInternal("stale", "current"), common.ErrVolumeFileConflict)
}

func TestApplyVolumeWorkspaceChangesTransactionInternalRollsBackEveryMutationStage(t *testing.T) {
	changes := []volumetypes.FileChange{
		{Operation: volumetypes.FileOpCreateFolder, RelativePath: "one"},
		{Operation: volumetypes.FileOpRename, RelativePath: "one", NewName: "two"},
		{Operation: volumetypes.FileOpDelete, RelativePath: "two", Recursive: true},
	}
	applyFailure := errors.New("apply failed")
	for failureIndex := range changes {
		t.Run(changes[failureIndex].Operation, func(t *testing.T) {
			applied := make([]int, 0, failureIndex+1)
			rollbackCalls := 0
			err := applyVolumeWorkspaceChangesTransactionInternal(changes, func(index int, _ volumetypes.FileChange) error {
				applied = append(applied, index)
				if index == failureIndex {
					return applyFailure
				}
				return nil
			}, func() error {
				rollbackCalls++
				return nil
			})
			require.ErrorIs(t, err, applyFailure)
			require.Equal(t, failureIndex+1, len(applied))
			require.Equal(t, 1, rollbackCalls)
		})
	}

	rollbackFailure := errors.New("rollback failed")
	err := applyVolumeWorkspaceChangesTransactionInternal(changes, func(_ int, _ volumetypes.FileChange) error {
		return applyFailure
	}, func() error {
		return rollbackFailure
	})
	require.ErrorIs(t, err, applyFailure)
	require.ErrorIs(t, err, rollbackFailure)
}
