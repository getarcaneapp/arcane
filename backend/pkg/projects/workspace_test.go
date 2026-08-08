package projects

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/types/v2/project"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadProjectWorkspace_ExcludesProtectedFilesAndReturnsFolders(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, ".env"), []byte("APP_VALUE=sample\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, ".env.git"), []byte("APP_VALUE=git\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "project.env"), []byte("APP_VALUE=local\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "README.md"), []byte("hello\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "config", "nested"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "config", "app.yaml"), []byte("value: true\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, ".git"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, ".git", "config"), []byte("private"), 0o644))

	files, revision, _, err := ReadProjectWorkspace(projectDir, 3, "", "compose.yaml", 0, 0)
	require.NoError(t, err)
	require.NotEmpty(t, revision)

	relativePaths := make([]string, 0, len(files))
	for _, file := range files {
		relativePaths = append(relativePaths, file.RelativePath)
	}

	assert.ElementsMatch(t, []string{"README.md", "config", filepath.ToSlash(filepath.Join("config", "app.yaml")), filepath.ToSlash(filepath.Join("config", "nested"))}, relativePaths)
}

func TestReadProjectWorkspace_ZeroMaxDepthDisablesExpansion(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "config"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "config", "app.yaml"), []byte("value: true\n"), 0o644))

	files, revision, _, err := ReadProjectWorkspace(projectDir, 0, "", "compose.yaml", 0, 0)
	require.NoError(t, err)
	assert.NotEmpty(t, revision)
	assert.Empty(t, files)
}

func TestReadProjectWorkspace_UseScanDepthSentinelUsesWorkspaceMaxDepth(t *testing.T) {
	projectDir := t.TempDir()
	t.Setenv("PROJECT_WORKSPACE_MAX_DEPTH", "2")

	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "level1", "level2"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "level1", "visible.txt"), []byte("visible\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "level1", "level2", "hidden.txt"), []byte("hidden\n"), 0o644))

	files, _, _, err := ReadProjectWorkspace(projectDir, ProjectWorkspaceUseScanDepth, "", "compose.yaml", 0, 0)
	require.NoError(t, err)

	relativePaths := make([]string, 0, len(files))
	for _, file := range files {
		relativePaths = append(relativePaths, file.RelativePath)
	}

	assert.Contains(t, relativePaths, "level1")
	assert.Contains(t, relativePaths, filepath.ToSlash(filepath.Join("level1", "visible.txt")))
	assert.Contains(t, relativePaths, filepath.ToSlash(filepath.Join("level1", "level2")))
	assert.NotContains(t, relativePaths, filepath.ToSlash(filepath.Join("level1", "level2", "hidden.txt")))
}

func TestApplyWorkspaceFileChanges_RejectsUnsafePathsAndProtectedFiles(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o644))

	testCases := []struct {
		name   string
		change project.WorkspaceFileChange
		upload []byte
	}{
		{
			name: "traversal",
			change: project.WorkspaceFileChange{
				Operation:    "create_file",
				RelativePath: "../escape.txt",
				UploadIndex:  new(0),
			},
			upload: []byte("safe\n"),
		},
		{
			name: "protected compose",
			change: project.WorkspaceFileChange{
				Operation:    "delete",
				RelativePath: "compose.yaml",
			},
		},
		{
			name: "move protected compose",
			change: project.WorkspaceFileChange{
				Operation:     "move",
				RelativePath:  "compose.yaml",
				NewParentPath: "config",
			},
		},
		{
			name: "protected compose descendant",
			change: project.WorkspaceFileChange{
				Operation:    "create_file",
				RelativePath: "compose.yaml/child.yaml",
				UploadIndex:  new(0),
			},
			upload: []byte("bad\n"),
		},
		{
			name: "binary content",
			change: project.WorkspaceFileChange{
				Operation:    "create_file",
				RelativePath: "binary.txt",
				UploadIndex:  new(0),
			},
			upload: []byte{0},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			uploads := map[int][]byte{}
			if tc.upload != nil {
				uploads[0] = tc.upload
			}
			err := ApplyProjectWorkspaceChanges(projectDir, []project.WorkspaceFileChange{tc.change}, uploads, ProjectWorkspaceApplyOptions{ComposeFileName: "compose.yaml"})
			require.Error(t, err)
		})
	}
}

func TestApplyWorkspaceFileChanges_WrapsForbiddenSentinelErrors(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o644))

	err := ApplyProjectWorkspaceChanges(projectDir, []project.WorkspaceFileChange{
		{Operation: "delete", RelativePath: "compose.yaml"},
	}, nil, ProjectWorkspaceApplyOptions{ComposeFileName: "compose.yaml"})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrProjectWorkspaceProtectedPath)

	targetPath := filepath.Join(projectDir, "target.txt")
	linkPath := filepath.Join(projectDir, "link.txt")
	require.NoError(t, os.WriteFile(targetPath, []byte("target\n"), 0o644))
	if err := os.Symlink(targetPath, linkPath); err != nil {
		t.Skipf("symlink creation is unavailable: %v", err)
	}

	content := []byte("updated\n")
	err = ApplyProjectWorkspaceChanges(projectDir, []project.WorkspaceFileChange{
		{Operation: "update_file", RelativePath: "link.txt", UploadIndex: new(0)},
	}, map[int][]byte{0: content}, ProjectWorkspaceApplyOptions{ComposeFileName: "compose.yaml"})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrProjectWorkspaceSymlinkPath)

	outsideDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outsideDir, "outside.txt"), []byte("outside\n"), 0o644))
	linkDirPath := filepath.Join(projectDir, "link-dir")
	if err := os.Symlink(outsideDir, linkDirPath); err != nil {
		t.Skipf("symlink creation is unavailable: %v", err)
	}

	err = ApplyProjectWorkspaceChanges(projectDir, []project.WorkspaceFileChange{
		{Operation: "update_file", RelativePath: "link-dir/outside.txt", UploadIndex: new(0)},
	}, map[int][]byte{0: content}, ProjectWorkspaceApplyOptions{ComposeFileName: "compose.yaml"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrProjectWorkspaceSymlinkPath)
}

func TestApplyWorkspaceFileChanges_UsesRevisionConflictDetection(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "notes.txt"), []byte("old\n"), 0o644))

	_, revision, _, err := ReadProjectWorkspace(projectDir, 3, "", "compose.yaml", 0, 0)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "notes.txt"), []byte("changed elsewhere\n"), 0o644))

	err = ApplyProjectWorkspaceChanges(projectDir, []project.WorkspaceFileChange{
		{Operation: "update_file", RelativePath: "notes.txt", UploadIndex: new(0)},
	}, map[int][]byte{0: []byte("new\n")}, ProjectWorkspaceApplyOptions{
		ExpectedRevision: revision,
		ComposeFileName:  "compose.yaml",
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrProjectWorkspaceRevisionConflict)
}

func TestApplyWorkspaceFileChanges_AppliesOrderedTextFileOperations(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o644))

	updated := []byte("updated\n")
	err := ApplyProjectWorkspaceChanges(projectDir, []project.WorkspaceFileChange{
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

	err := ApplyProjectWorkspaceChanges(projectDir, []project.WorkspaceFileChange{
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

			err := ApplyProjectWorkspaceChanges(projectDir, []project.WorkspaceFileChange{
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

	err := ApplyProjectWorkspaceChanges(projectDir, []project.WorkspaceFileChange{
		{Operation: "delete", RelativePath: "config"},
	}, nil, ProjectWorkspaceApplyOptions{ComposeFileName: "compose.yaml"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "folder is not empty")

	err = ApplyProjectWorkspaceChanges(projectDir, []project.WorkspaceFileChange{
		{Operation: "delete", RelativePath: "config", Recursive: true},
	}, nil, ProjectWorkspaceApplyOptions{ComposeFileName: "compose.yaml"})
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(projectDir, "config"))
	assert.True(t, os.IsNotExist(err))
}

func TestApplyProjectWorkspaceChangesRejectsUnusedUploadsForEmptyManifest(t *testing.T) {
	t.Parallel()

	err := ApplyProjectWorkspaceChanges(t.TempDir(), nil, map[int][]byte{0: []byte("unused")}, ProjectWorkspaceApplyOptions{})
	require.ErrorContains(t, err, "unused")
}

func TestValidateProjectWorkspaceFileName_RejectsPathSeparators(t *testing.T) {
	t.Parallel()

	_, err := utils.ValidateFileName(strings.Join([]string{"folder", "name"}, string(filepath.Separator)))
	require.Error(t, err)
}

func TestReadProjectWorkspace_CapsEntriesWithStableRevision(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o644))
	for i := range 5 {
		require.NoError(t, os.WriteFile(filepath.Join(projectDir, fmt.Sprintf("file-%d.txt", i)), []byte("x\n"), 0o644))
	}

	files, revision, truncated, err := ReadProjectWorkspace(projectDir, 3, "", "compose.yaml", 3, 0)
	require.NoError(t, err)
	assert.True(t, truncated)
	assert.Len(t, files, 3)

	_, again, truncatedAgain, err := ReadProjectWorkspace(projectDir, 3, "", "compose.yaml", 3, 0)
	require.NoError(t, err)
	assert.True(t, truncatedAgain)
	assert.Equal(t, revision, again)
}

func TestApplyWorkspaceFileChanges_SucceedsWithCappedRevision(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o644))
	for i := range 5 {
		require.NoError(t, os.WriteFile(filepath.Join(projectDir, fmt.Sprintf("file-%d.txt", i)), []byte("x\n"), 0o644))
	}

	_, revision, truncated, err := ReadProjectWorkspace(projectDir, 3, "", "compose.yaml", 3, 0)
	require.NoError(t, err)
	require.True(t, truncated)

	err = ApplyProjectWorkspaceChanges(projectDir, []project.WorkspaceFileChange{
		{Operation: "update_file", RelativePath: "file-0.txt", UploadIndex: new(0)},
	}, map[int][]byte{0: []byte("new\n")}, ProjectWorkspaceApplyOptions{
		ExpectedRevision: revision,
		MaxDepth:         3,
		MaxEntries:       3,
		ComposeFileName:  "compose.yaml",
	})
	require.NoError(t, err)
}

func TestProtectedProjectFilePaths_IncludesComposeOverrideCandidates(t *testing.T) {
	t.Parallel()

	protected := ProtectedProjectFilePaths("compose.yaml")

	for _, candidate := range ComposeOverrideFileCandidates() {
		assert.Truef(t, protected[candidate], "expected %q to be protected", candidate)
	}
}
