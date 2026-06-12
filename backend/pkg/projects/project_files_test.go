package projects

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getarcaneapp/arcane/types/v2/project"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadProjectFileTree_ExcludesProtectedFilesAndReturnsFolders(t *testing.T) {
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

	files, revision, err := ReadProjectFileTree(projectDir, 3, "", "compose.yaml")
	require.NoError(t, err)
	require.NotEmpty(t, revision)

	relativePaths := make([]string, 0, len(files))
	for _, file := range files {
		relativePaths = append(relativePaths, file.RelativePath)
	}

	assert.ElementsMatch(t, []string{"README.md", "config", filepath.ToSlash(filepath.Join("config", "app.yaml")), filepath.ToSlash(filepath.Join("config", "nested"))}, relativePaths)
}

func TestReadProjectFileTree_ZeroMaxDepthDisablesExpansion(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "config"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "config", "app.yaml"), []byte("value: true\n"), 0o644))

	files, revision, err := ReadProjectFileTree(projectDir, 0, "", "compose.yaml")
	require.NoError(t, err)
	assert.NotEmpty(t, revision)
	assert.Empty(t, files)
}

func TestReadProjectFileTree_UseScanDepthSentinelUsesFileTreeMaxDepth(t *testing.T) {
	projectDir := t.TempDir()
	t.Setenv("PROJECT_FILE_TREE_MAX_DEPTH", "2")

	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "level1", "level2"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "level1", "visible.txt"), []byte("visible\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "level1", "level2", "hidden.txt"), []byte("hidden\n"), 0o644))

	files, _, err := ReadProjectFileTree(projectDir, ProjectFileTreeUseScanDepth, "", "compose.yaml")
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

func TestApplyProjectFileChanges_RejectsUnsafePathsAndProtectedFiles(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o644))

	content := "safe\n"
	protectedDescendantContent := "bad\n"
	binaryContent := string([]byte{0})
	testCases := []struct {
		name   string
		change project.ProjectFileChange
	}{
		{
			name: "traversal",
			change: project.ProjectFileChange{
				Operation:    "create_file",
				RelativePath: "../escape.txt",
				Content:      &content,
			},
		},
		{
			name: "protected compose",
			change: project.ProjectFileChange{
				Operation:    "delete",
				RelativePath: "compose.yaml",
			},
		},
		{
			name: "move protected compose",
			change: project.ProjectFileChange{
				Operation:     "move",
				RelativePath:  "compose.yaml",
				NewParentPath: "config",
			},
		},
		{
			name: "protected compose descendant",
			change: project.ProjectFileChange{
				Operation:    "create_file",
				RelativePath: "compose.yaml/child.yaml",
				Content:      &protectedDescendantContent,
			},
		},
		{
			name: "binary content",
			change: project.ProjectFileChange{
				Operation:    "create_file",
				RelativePath: "binary.txt",
				Content:      &binaryContent,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := ApplyProjectFileChanges(projectDir, []project.ProjectFileChange{tc.change}, ProjectFileApplyOptions{ComposeFileName: "compose.yaml"})
			require.Error(t, err)
		})
	}
}

func TestApplyProjectFileChanges_WrapsForbiddenSentinelErrors(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o644))

	err := ApplyProjectFileChanges(projectDir, []project.ProjectFileChange{
		{Operation: "delete", RelativePath: "compose.yaml"},
	}, ProjectFileApplyOptions{ComposeFileName: "compose.yaml"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrProjectFileProtectedPath)

	targetPath := filepath.Join(projectDir, "target.txt")
	linkPath := filepath.Join(projectDir, "link.txt")
	require.NoError(t, os.WriteFile(targetPath, []byte("target\n"), 0o644))
	if err := os.Symlink(targetPath, linkPath); err != nil {
		t.Skipf("symlink creation is unavailable: %v", err)
	}

	content := "updated\n"
	err = ApplyProjectFileChanges(projectDir, []project.ProjectFileChange{
		{Operation: "update_file", RelativePath: "link.txt", Content: &content},
	}, ProjectFileApplyOptions{ComposeFileName: "compose.yaml"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrProjectFileSymlinkPath)

	outsideDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outsideDir, "outside.txt"), []byte("outside\n"), 0o644))
	linkDirPath := filepath.Join(projectDir, "link-dir")
	if err := os.Symlink(outsideDir, linkDirPath); err != nil {
		t.Skipf("symlink creation is unavailable: %v", err)
	}

	err = ApplyProjectFileChanges(projectDir, []project.ProjectFileChange{
		{Operation: "update_file", RelativePath: "link-dir/outside.txt", Content: &content},
	}, ProjectFileApplyOptions{ComposeFileName: "compose.yaml"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrProjectFileSymlinkPath)
}

func TestMapProjectRootErrorInternal_DoesNotClassifyByErrorMessage(t *testing.T) {
	t.Parallel()

	err := mapProjectRootErrorInternal("inspect project path", &os.PathError{
		Op:   "lstat",
		Path: "notes.txt",
		Err:  errors.New("disk check escapes normal retry path"),
	})

	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrProjectFileOutsideProjectDirectory)
}

func TestApplyProjectFileChanges_UsesRevisionConflictDetection(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "notes.txt"), []byte("old\n"), 0o644))

	_, revision, err := ReadProjectFileTree(projectDir, 3, "", "compose.yaml")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "notes.txt"), []byte("changed elsewhere\n"), 0o644))

	content := "new\n"
	err = ApplyProjectFileChanges(projectDir, []project.ProjectFileChange{
		{Operation: "update_file", RelativePath: "notes.txt", Content: &content},
	}, ProjectFileApplyOptions{
		ExpectedRevision: revision,
		ComposeFileName:  "compose.yaml",
	})

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrProjectFileRevisionConflict))
}

func TestApplyProjectFileChanges_AppliesOrderedTextFileOperations(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o644))

	content := "hello\n"
	updated := "updated\n"
	err := ApplyProjectFileChanges(projectDir, []project.ProjectFileChange{
		{Operation: "create_folder", RelativePath: "config"},
		{Operation: "create_file", RelativePath: "config/app.yaml", Content: &content},
		{Operation: "update_file", RelativePath: "config/app.yaml", Content: &updated},
		{Operation: "rename", RelativePath: "config/app.yaml", NewName: "renamed.yaml"},
	}, ProjectFileApplyOptions{ComposeFileName: "compose.yaml"})
	require.NoError(t, err)

	bytes, err := os.ReadFile(filepath.Join(projectDir, "config", "renamed.yaml"))
	require.NoError(t, err)
	assert.Equal(t, updated, string(bytes))
}

func TestApplyProjectFileChanges_MovesProjectPaths(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "config", "nested"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "archive"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "config", "app.yaml"), []byte("value: true\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "config", "nested", "child.txt"), []byte("child\n"), 0o644))

	err := ApplyProjectFileChanges(projectDir, []project.ProjectFileChange{
		{Operation: "move", RelativePath: "config/app.yaml", NewParentPath: "archive"},
		{Operation: "move", RelativePath: "config/nested"},
	}, ProjectFileApplyOptions{ComposeFileName: "compose.yaml"})
	require.NoError(t, err)

	assert.NoFileExists(t, filepath.Join(projectDir, "config", "app.yaml"))
	assert.FileExists(t, filepath.Join(projectDir, "archive", "app.yaml"))
	assert.NoDirExists(t, filepath.Join(projectDir, "config", "nested"))
	assert.FileExists(t, filepath.Join(projectDir, "nested", "child.txt"))
}

func TestApplyProjectFileChanges_RejectsInvalidMoves(t *testing.T) {
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
			wantError:     "project path already exists",
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

			err := ApplyProjectFileChanges(projectDir, []project.ProjectFileChange{
				{Operation: "move", RelativePath: tc.relativePath, NewParentPath: tc.newParentPath},
			}, ProjectFileApplyOptions{ComposeFileName: "compose.yaml"})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantError)
		})
	}
}

func TestApplyProjectFileChanges_RequiresRecursiveForNonEmptyFolderDelete(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("services: {}\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "config"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "config", "app.yaml"), []byte("value: true\n"), 0o644))

	err := ApplyProjectFileChanges(projectDir, []project.ProjectFileChange{
		{Operation: "delete", RelativePath: "config"},
	}, ProjectFileApplyOptions{ComposeFileName: "compose.yaml"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "folder is not empty")

	err = ApplyProjectFileChanges(projectDir, []project.ProjectFileChange{
		{Operation: "delete", RelativePath: "config", Recursive: true},
	}, ProjectFileApplyOptions{ComposeFileName: "compose.yaml"})
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(projectDir, "config"))
	assert.True(t, os.IsNotExist(err))
}

func TestValidateProjectFileName_RejectsPathSeparators(t *testing.T) {
	t.Parallel()

	_, err := ValidateProjectFileName(strings.Join([]string{"folder", "name"}, string(filepath.Separator)))
	require.Error(t, err)
}
