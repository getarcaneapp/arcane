package projects

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"getarcane.dev/types/v3"
)

func projectWorkspaceFolderPaths(t *testing.T, projectDir, relativePath string, opts ProjectWorkspaceListOptions) []string {
	t.Helper()
	folder, err := ListProjectWorkspaceFolder(t.Context(), projectDir, relativePath, opts)
	require.NoError(t, err)
	paths := make([]string, 0, len(folder.Files))
	for _, file := range folder.Files {
		paths = append(paths, file.RelativePath)
	}
	return paths
}

func TestListProjectWorkspaceFolder_ExcludesProtectedFilesAndSkippedFolders(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, ".env"), []byte("APP_VALUE=sample\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, ".env.git"), []byte("APP_VALUE=git\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "project.env"), []byte("APP_VALUE=local\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "README.md"), []byte("hello\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "config", "nested"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "config", "node_modules"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "config", "app.yaml"), []byte("value: true\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "config", "compose.yaml"), []byte("services: {}\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, ".git"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, ".git", "config"), []byte("private"), 0o644))
	opts := ProjectWorkspaceListOptions{SkipDirectories: "node_modules", ComposeFileName: "compose.yaml", MaxEntries: 100, MaxFileSizeBytes: 1024}

	assert.Equal(t, []string{"config", "README.md"}, projectWorkspaceFolderPaths(t, projectDir, "", opts))
	assert.Equal(t, []string{"config/nested", "config/app.yaml", "config/compose.yaml"}, projectWorkspaceFolderPaths(t, projectDir, "config", opts))
	assert.Empty(t, projectWorkspaceFolderPaths(t, projectDir, "config/nested", opts))

	_, err := ListProjectWorkspaceFolder(t.Context(), projectDir, ".git", opts)
	require.ErrorIs(t, err, fs.ErrNotExist)
	_, err = ListProjectWorkspaceFolder(t.Context(), projectDir, "config/node_modules", opts)
	require.ErrorIs(t, err, fs.ErrNotExist)
	_, err = ListProjectWorkspaceFolder(t.Context(), projectDir, "missing", opts)
	require.ErrorIs(t, err, fs.ErrNotExist)
}

func TestListProjectWorkspaceFolder_ClassifiesBySizeWithoutReadingContent(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "db.sqlite"), []byte{0x00, 0xff, 0xfe}, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "large.txt"), []byte("12345"), 0o644))

	folder, err := ListProjectWorkspaceFolder(t.Context(), projectDir, "", ProjectWorkspaceListOptions{ComposeFileName: "compose.yaml", MaxEntries: 100, MaxFileSizeBytes: 4})
	require.NoError(t, err)
	require.Len(t, folder.Files, 2)
	assert.Equal(t, "db.sqlite", folder.Files[0].RelativePath)
	assert.True(t, folder.Files[0].Editable)
	assert.Equal(t, "large.txt", folder.Files[1].RelativePath)
	assert.Equal(t, types.FileReadOnlyTooLarge, folder.Files[1].ReadOnlyReason)
}

func TestListProjectWorkspaceFolder_ListsSymlinksWithoutFollowing(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o644))
	require.NoError(t, os.Mkdir(filepath.Join(projectDir, "config"), 0o755))
	if err := os.Symlink("config", filepath.Join(projectDir, "link")); err != nil {
		t.Skipf("symlink creation is unavailable: %v", err)
	}

	folder, err := ListProjectWorkspaceFolder(t.Context(), projectDir, "", ProjectWorkspaceListOptions{ComposeFileName: "compose.yaml", MaxEntries: 100, MaxFileSizeBytes: 1024})
	require.NoError(t, err)
	require.Len(t, folder.Files, 2)
	link := folder.Files[1]
	assert.Equal(t, "link", link.RelativePath)
	assert.True(t, link.IsSymlink)
	assert.False(t, link.IsDirectory)
	assert.Equal(t, "config", link.LinkTarget)
	assert.Equal(t, types.FileReadOnlySymlink, link.ReadOnlyReason)
}

func TestApplyWorkspaceFileChanges_RejectsUnsafePathsAndProtectedFiles(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o644))

	testCases := []struct {
		name   string
		change types.WorkspaceFileChange
		upload []byte
	}{
		{
			name: "traversal",
			change: types.WorkspaceFileChange{
				Operation:    "create_file",
				RelativePath: "../escape.txt",
				UploadIndex:  new(0),
			},
			upload: []byte("safe\n"),
		},
		{
			name: "protected compose",
			change: types.WorkspaceFileChange{
				Operation:    "delete",
				RelativePath: "compose.yaml",
			},
		},
		{
			name: "move protected compose",
			change: types.WorkspaceFileChange{
				Operation:     "move",
				RelativePath:  "compose.yaml",
				NewParentPath: "config",
			},
		},
		{
			name: "protected compose descendant",
			change: types.WorkspaceFileChange{
				Operation:    "create_file",
				RelativePath: "compose.yaml/child.yaml",
				UploadIndex:  new(0),
			},
			upload: []byte("bad\n"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			uploads := map[int][]byte{}
			if tc.upload != nil {
				uploads[0] = tc.upload
			}
			err := ApplyProjectWorkspaceChanges(t.Context(), projectDir, []types.WorkspaceFileChange{tc.change}, uploads, ProjectWorkspaceApplyOptions{ComposeFileName: "compose.yaml"})
			require.Error(t, err)
		})
	}
}

func TestApplyWorkspaceFileChanges_CreatesBinaryFileVerbatim(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o644))

	binary := []byte{0x1f, 0x8b, 0x08, 0x00, 0xff, 0x00, 0xde, 0xad, 0xbe, 0xef}
	err := ApplyProjectWorkspaceChanges(t.Context(), projectDir, []types.WorkspaceFileChange{
		{Operation: "create_file", RelativePath: "db.sqlite", UploadIndex: new(0)},
	}, map[int][]byte{0: binary}, ProjectWorkspaceApplyOptions{ComposeFileName: "compose.yaml"})
	require.NoError(t, err)

	stored, err := os.ReadFile(filepath.Join(projectDir, "db.sqlite"))
	require.NoError(t, err)
	require.Equal(t, binary, stored)
}

func TestApplyWorkspaceFileChanges_WrapsForbiddenSentinelErrors(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o644))

	err := ApplyProjectWorkspaceChanges(t.Context(), projectDir, []types.WorkspaceFileChange{
		{Operation: "delete", RelativePath: "compose.yaml"},
	}, nil, ProjectWorkspaceApplyOptions{ComposeFileName: "compose.yaml"})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrProjectWorkspaceProtectedPath)

	targetPath := filepath.Join(projectDir, "target.txt")
	linkPath := filepath.Join(projectDir, "link.txt")
	require.NoError(t, os.WriteFile(targetPath, []byte("target\n"), 0o644))
	if symlinkErr := os.Symlink(targetPath, linkPath); symlinkErr != nil {
		t.Skipf("symlink creation is unavailable: %v", symlinkErr)
	}

	content := []byte("updated\n")
	err = ApplyProjectWorkspaceChanges(t.Context(), projectDir, []types.WorkspaceFileChange{
		{Operation: "update_file", RelativePath: "link.txt", UploadIndex: new(0)},
	}, map[int][]byte{0: content}, ProjectWorkspaceApplyOptions{ComposeFileName: "compose.yaml"})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrProjectWorkspaceSymlinkPath)

	outsideDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outsideDir, "outside.txt"), []byte("outside\n"), 0o644))
	linkDirPath := filepath.Join(projectDir, "link-dir")
	if symlinkErr2 := os.Symlink(outsideDir, linkDirPath); symlinkErr2 != nil {
		t.Skipf("symlink creation is unavailable: %v", symlinkErr2)
	}

	err = ApplyProjectWorkspaceChanges(t.Context(), projectDir, []types.WorkspaceFileChange{
		{Operation: "update_file", RelativePath: "link-dir/outside.txt", UploadIndex: new(0)},
	}, map[int][]byte{0: content}, ProjectWorkspaceApplyOptions{ComposeFileName: "compose.yaml"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrProjectWorkspaceSymlinkPath)
}

func TestApplyWorkspaceFileChanges_AppliesOrderedTextFileOperations(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o644))

	updated := []byte("updated\n")
	err := ApplyProjectWorkspaceChanges(t.Context(), projectDir, []types.WorkspaceFileChange{
		{Operation: "create_folder", RelativePath: "config"},
		{Operation: "create_file", RelativePath: "config/app.yaml", UploadIndex: new(0)},
		{Operation: "update_file", RelativePath: "config/app.yaml", UploadIndex: new(1)},
		{Operation: "rename", RelativePath: "config/app.yaml", NewName: "renamed.yaml"},
	}, map[int][]byte{0: []byte("hello\n"), 1: updated}, ProjectWorkspaceApplyOptions{ComposeFileName: "compose.yaml"})
	require.NoError(t, err)

	bytes, err := os.ReadFile(filepath.Join(projectDir, "config", "renamed.yaml"))
	require.NoError(t, err)
	assert.Equal(t, string(updated), string(bytes))
}

func TestApplyWorkspaceFileChanges_MovesProjectPaths(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "config", "nested"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "archive"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "config", "app.yaml"), []byte("value: true\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "config", "nested", "child.txt"), []byte("child\n"), 0o644))

	err := ApplyProjectWorkspaceChanges(t.Context(), projectDir, []types.WorkspaceFileChange{
		{Operation: "move", RelativePath: "config/app.yaml", NewParentPath: "archive"},
		{Operation: "move", RelativePath: "config/nested"},
	}, nil, ProjectWorkspaceApplyOptions{ComposeFileName: "compose.yaml"})
	require.NoError(t, err)

	assert.NoFileExists(t, filepath.Join(projectDir, "config", "app.yaml"))
	assert.FileExists(t, filepath.Join(projectDir, "archive", "app.yaml"))
	assert.NoDirExists(t, filepath.Join(projectDir, "config", "nested"))
	assert.FileExists(t, filepath.Join(projectDir, "nested", "child.txt"))
}

func TestApplyWorkspaceFileChanges_RejectsInvalidMoves(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		relativePath  string
		newParentPath string
		wantError     string
	}{
		{
			name:          "duplicate destination",
			relativePath:  "config/app.yaml",
			newParentPath: "archive",
			wantError:     "project workspace path already exists",
		},
		{
			name:          "folder into descendant",
			relativePath:  "config",
			newParentPath: "config/nested",
			wantError:     "folder cannot be moved into itself or a descendant",
		},
		{
			name:          "missing destination folder",
			relativePath:  "config/app.yaml",
			newParentPath: "missing",
			wantError:     "destination folder not found",
		},
		{
			name:          "same destination",
			relativePath:  "config/app.yaml",
			newParentPath: "config",
			wantError:     "already in the destination folder",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			projectDir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o644))
			require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "config", "nested"), 0o755))
			require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "archive"), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(projectDir, "config", "app.yaml"), []byte("value: true\n"), 0o644))
			require.NoError(t, os.WriteFile(filepath.Join(projectDir, "archive", "app.yaml"), []byte("existing\n"), 0o644))

			err := ApplyProjectWorkspaceChanges(t.Context(), projectDir, []types.WorkspaceFileChange{
				{Operation: "move", RelativePath: tc.relativePath, NewParentPath: tc.newParentPath},
			}, nil, ProjectWorkspaceApplyOptions{ComposeFileName: "compose.yaml"})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantError)
		})
	}
}

func TestApplyWorkspaceFileChanges_RequiresRecursiveForNonEmptyFolderDelete(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "config"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "config", "app.yaml"), []byte("value: true\n"), 0o644))

	err := ApplyProjectWorkspaceChanges(t.Context(), projectDir, []types.WorkspaceFileChange{
		{Operation: "delete", RelativePath: "config"},
	}, nil, ProjectWorkspaceApplyOptions{ComposeFileName: "compose.yaml"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "folder is not empty")

	err = ApplyProjectWorkspaceChanges(t.Context(), projectDir, []types.WorkspaceFileChange{
		{Operation: "delete", RelativePath: "config", Recursive: true},
	}, nil, ProjectWorkspaceApplyOptions{ComposeFileName: "compose.yaml"})
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(projectDir, "config"))
	assert.True(t, os.IsNotExist(err))
}

func TestApplyProjectWorkspaceChangesRejectsUnusedUploadsForEmptyManifest(t *testing.T) {
	t.Parallel()

	err := ApplyProjectWorkspaceChanges(t.Context(), t.TempDir(), nil, map[int][]byte{0: []byte("unused")}, ProjectWorkspaceApplyOptions{})
	require.ErrorContains(t, err, "unused")
}

func TestListProjectWorkspaceFolder_CapsEntriesPerFolder(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o644))
	require.NoError(t, os.Mkdir(filepath.Join(projectDir, "a-large"), 0o755))
	for i := range 5 {
		require.NoError(t, os.WriteFile(filepath.Join(projectDir, "a-large", fmt.Sprintf("file-%d.txt", i)), []byte("x\n"), 0o644))
	}
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "z-later.txt"), []byte("x\n"), 0o644))
	opts := ProjectWorkspaceListOptions{ComposeFileName: "compose.yaml", MaxEntries: 3, MaxFileSizeBytes: 1024}

	root, err := ListProjectWorkspaceFolder(t.Context(), projectDir, "", opts)
	require.NoError(t, err)
	assert.False(t, root.FileTreeTruncated, "a large subfolder must not truncate its parent")
	assert.Len(t, root.Files, 2)

	large, err := ListProjectWorkspaceFolder(t.Context(), projectDir, "a-large", opts)
	require.NoError(t, err)
	assert.True(t, large.FileTreeTruncated)
	assert.Equal(t, []string{"a-large/file-0.txt", "a-large/file-1.txt", "a-large/file-2.txt"}, []string{large.Files[0].RelativePath, large.Files[1].RelativePath, large.Files[2].RelativePath})
}

func TestProtectedProjectFilePaths_IncludesComposeOverrideCandidates(t *testing.T) {
	t.Parallel()

	protected := ProtectedProjectFilePaths("compose.yaml")

	for _, candidate := range ComposeOverrideFileCandidates {
		assert.Truef(t, protected[candidate], "expected %q to be protected", candidate)
	}
}

func TestApplyProjectWorkspaceChanges_BaselineGuardsConcurrentEdit(t *testing.T) {
	projectDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "config.txt"), []byte("original\n"), 0o644))

	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "config.txt"), []byte("external newer edit\n"), 0o644))
	err := ApplyProjectWorkspaceChanges(t.Context(), projectDir, []types.WorkspaceFileChange{
		{Operation: types.FileOpUpdateFile, RelativePath: "config.txt", UploadIndex: new(0), BaselineIndex: new(1)},
	}, map[int][]byte{0: []byte("stale draft\n"), 1: []byte("original\n")}, ProjectWorkspaceApplyOptions{ComposeFileName: "compose.yaml"})
	require.ErrorIs(t, err, ErrProjectWorkspaceFileChanged)
	content, err := os.ReadFile(filepath.Join(projectDir, "config.txt"))
	require.NoError(t, err)
	require.Equal(t, "external newer edit\n", string(content))

	err = ApplyProjectWorkspaceChanges(t.Context(), projectDir, []types.WorkspaceFileChange{
		{Operation: types.FileOpUpdateFile, RelativePath: "config.txt", UploadIndex: new(0), BaselineIndex: new(1)},
	}, map[int][]byte{
		0: []byte("fresh draft\n"),
		1: []byte("external newer edit\n"),
	}, ProjectWorkspaceApplyOptions{ComposeFileName: "compose.yaml"})
	require.NoError(t, err)
	content, err = os.ReadFile(filepath.Join(projectDir, "config.txt"))
	require.NoError(t, err)
	require.Equal(t, "fresh draft\n", string(content))
}

func TestListProjectWorkspaceFolder_ListsEnvDirectoryContents(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "project.env"), []byte("APP_VALUE=local\n"), 0o644))
	require.NoError(t, os.Mkdir(filepath.Join(projectDir, ".env"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, ".env", "app.env"), []byte("APP_VALUE=sub\n"), 0o644))
	opts := ProjectWorkspaceListOptions{ComposeFileName: "compose.yaml", MaxEntries: 100, MaxFileSizeBytes: 1024}

	assert.Equal(t, []string{".env"}, projectWorkspaceFolderPaths(t, projectDir, "", opts))
	assert.Equal(t, []string{".env/app.env"}, projectWorkspaceFolderPaths(t, projectDir, ".env", opts))
}

func TestApplyWorkspaceFileChanges_EnvDirectory(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o644))
	require.NoError(t, os.Mkdir(filepath.Join(projectDir, ".env"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, ".env", "app.env"), []byte("A=1\n"), 0o644))
	opts := ProjectWorkspaceApplyOptions{ComposeFileName: "compose.yaml"}

	require.NoError(t, ApplyProjectWorkspaceChanges(t.Context(), projectDir, []types.WorkspaceFileChange{
		{Operation: "update_file", RelativePath: ".env/app.env", UploadIndex: new(0)},
	}, map[int][]byte{0: []byte("A=2\n")}, opts))
	content, err := os.ReadFile(filepath.Join(projectDir, ".env", "app.env"))
	require.NoError(t, err)
	assert.Equal(t, "A=2\n", string(content))

	// Once the directory is gone, the name is protected configuration again,
	// even within the same batch.
	err = ApplyProjectWorkspaceChanges(t.Context(), projectDir, []types.WorkspaceFileChange{
		{Operation: "delete", RelativePath: ".env", Recursive: true},
		{Operation: "create_file", RelativePath: ".env", UploadIndex: new(0)},
	}, map[int][]byte{0: []byte("B=1\n")}, opts)
	require.ErrorIs(t, err, ErrProjectWorkspaceProtectedPath)
}
