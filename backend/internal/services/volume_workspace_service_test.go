package services

import (
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

func TestParseVolumeWorkspaceTreeInternalSortsAndPreservesSymlinks(t *testing.T) {
	stdout := "0\x00" +
		volumeWorkspaceTreeRecordInternal("z.txt", "f", "4", "1700000000.25", "-rw-r--r--", "") +
		volumeWorkspaceTreeRecordInternal("folder/link", "l", "7", "1700000001.5", "lrwxrwxrwx", "../z.txt") +
		volumeWorkspaceTreeRecordInternal("folder", "d", "4096", "1700000002", "drwxr-xr-x", "")

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

	reordered, err := parseVolumeWorkspaceTreeInternal("0\x00"+
		volumeWorkspaceTreeRecordInternal("folder", "d", "4096", "1700000002", "drwxr-xr-x", "")+
		volumeWorkspaceTreeRecordInternal("z.txt", "f", "4", "1700000000.25", "-rw-r--r--", "")+
		volumeWorkspaceTreeRecordInternal("folder/link", "l", "7", "1700000001.5", "lrwxrwxrwx", "../z.txt"), 10)
	require.NoError(t, err)
	require.Equal(t, workspace.FileTreeRevision, reordered.FileTreeRevision)
}

func TestParseVolumeWorkspaceTreeInternalReportsBothLimits(t *testing.T) {
	entries := volumeWorkspaceTreeRecordInternal("b", "f", "1", "1", "-rw-r--r--", "") +
		volumeWorkspaceTreeRecordInternal("a", "f", "1", "1", "-rw-r--r--", "")

	depthLimited, err := parseVolumeWorkspaceTreeInternal("1\x00"+entries, 10)
	require.NoError(t, err)
	require.True(t, depthLimited.FileTreeTruncated)

	entryLimited, err := parseVolumeWorkspaceTreeInternal("0\x00"+entries, 1)
	require.NoError(t, err)
	require.True(t, entryLimited.FileTreeTruncated)
	require.Len(t, entryLimited.Files, 1)
	require.Equal(t, "a", entryLimited.Files[0].RelativePath)

	_, err = parseVolumeWorkspaceTreeInternal("invalid\x00"+entries, 10)
	require.Error(t, err)
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
