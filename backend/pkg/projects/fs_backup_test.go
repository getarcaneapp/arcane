package projects

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
	"go.getarcane.app/acfs"
)

func TestRestoreProjectUpdateBackup_UndoesRenameWhenSourceWasRecreated(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "a"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "a", "keep.txt"), []byte("keep\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "file-transition"), []byte("original file\n"), 0o640))
	require.NoError(t, os.Mkdir(filepath.Join(projectDir, "dir-transition"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "dir-transition", "keep.txt"), []byte("original directory\n"), 0o644))
	configPath := filepath.Join(projectDir, "config.txt")
	require.NoError(t, os.WriteFile(configPath, []byte("original config\n"), 0o640))
	require.NoError(t, os.Chmod(configPath, 0o640))
	originalConfig, err := os.Stat(configPath)
	require.NoError(t, err)

	// Batch: rename a -> b, create_folder a, then a later change fails.
	scope := ProjectUpdateBackupScope{
		Paths: []string{"a", "file-transition", "file-transition/child.txt", "dir-transition", "config.txt",
			"new-parent/deep/managed.txt", "empty-parent/deep/managed.txt"},
		RenamedDirs: [][2]string{{"a", "b"}},
	}
	backup, err := BackupProjectUpdateScope(t.Context(), projectDir, t.TempDir(), scope)
	require.NoError(t, err)

	require.NoError(t, os.Rename(filepath.Join(projectDir, "a"), filepath.Join(projectDir, "b")))
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "a"), 0o755))
	require.NoError(t, os.Remove(filepath.Join(projectDir, "file-transition")))
	require.NoError(t, os.Mkdir(filepath.Join(projectDir, "file-transition"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "file-transition", "child.txt"), []byte("new child\n"), 0o644))
	require.NoError(t, os.RemoveAll(filepath.Join(projectDir, "dir-transition")))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "dir-transition"), []byte("replacement file\n"), 0o644))
	require.NoError(t, os.WriteFile(configPath, []byte("changed config\n"), 0o640))
	require.NoError(t, os.Chmod(configPath, 0o600))
	for _, parent := range []string{"new-parent", "empty-parent"} {
		require.NoError(t, os.MkdirAll(filepath.Join(projectDir, parent, "deep"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(projectDir, parent, "deep", "managed.txt"), []byte("managed\n"), 0o644))
	}
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "new-parent", "deep", "app-data.txt"), []byte("unrelated\n"), 0o644))

	require.NoError(t, RestoreProjectUpdateBackup(t.Context(), projectDir, backup))

	require.NoDirExists(t, filepath.Join(projectDir, "b"))
	require.FileExists(t, filepath.Join(projectDir, "a", "keep.txt"))
	fileContent, err := os.ReadFile(filepath.Join(projectDir, "file-transition"))
	require.NoError(t, err)
	require.Equal(t, "original file\n", string(fileContent))
	dirContent, err := os.ReadFile(filepath.Join(projectDir, "dir-transition", "keep.txt"))
	require.NoError(t, err)
	require.Equal(t, "original directory\n", string(dirContent))
	configContent, err := os.ReadFile(configPath)
	require.NoError(t, err)
	require.Equal(t, "original config\n", string(configContent))
	restoredConfig, err := os.Stat(configPath)
	require.NoError(t, err)
	require.True(t, os.SameFile(originalConfig, restoredConfig))
	if runtime.GOOS != "windows" {
		require.Equal(t, os.FileMode(0o640), restoredConfig.Mode().Perm())
	}
	require.NoFileExists(t, filepath.Join(projectDir, "new-parent", "deep", "managed.txt"))
	appContent, err := os.ReadFile(filepath.Join(projectDir, "new-parent", "deep", "app-data.txt"))
	require.NoError(t, err)
	require.Equal(t, "unrelated\n", string(appContent))
	require.NoDirExists(t, filepath.Join(projectDir, "empty-parent"))
}

func TestRestoreProjectUpdateBackup_RestoresExternalEnvSymlinkTarget(t *testing.T) {
	projectDir := t.TempDir()
	targetPath := filepath.Join(t.TempDir(), "project.env")
	originalContent := "VALUE=original\n"
	targetPerm := os.FileMode(0o640)
	require.NoError(t, os.WriteFile(targetPath, []byte(originalContent), targetPerm))
	require.NoError(t, os.Chmod(targetPath, targetPerm))

	envPath := filepath.Join(projectDir, EffectiveEnvFileName)
	if err := os.Symlink(targetPath, envPath); err != nil {
		t.Skipf("symlink creation is unavailable: %v", err)
	}
	originalLinkTarget, err := os.Readlink(envPath)
	require.NoError(t, err)

	backup, err := BackupProjectUpdateScope(t.Context(), projectDir, t.TempDir(), ProjectUpdateBackupScope{Paths: []string{EffectiveEnvFileName}})
	require.NoError(t, err)
	require.NotNil(t, backup.envSymlink)

	require.NoError(t, WriteProjectFile(t.Context(), projectDir, projectDir, ".env", "VALUE=updated\n"))
	targetBeforeRestore, err := os.Stat(targetPath)
	require.NoError(t, err)
	require.NoError(t, RestoreProjectUpdateBackup(t.Context(), projectDir, backup))
	targetAfterRestore, err := os.Stat(targetPath)
	require.NoError(t, err)
	require.True(t, os.SameFile(targetBeforeRestore, targetAfterRestore))

	restoredContent, err := os.ReadFile(targetPath)
	require.NoError(t, err)
	require.Equal(t, originalContent, string(restoredContent))
	currentLinkTarget, err := os.Readlink(envPath)
	require.NoError(t, err)
	require.Equal(t, originalLinkTarget, currentLinkTarget)
	linkInfo, err := os.Lstat(envPath)
	require.NoError(t, err)
	require.NotZero(t, linkInfo.Mode()&os.ModeSymlink)
	if runtime.GOOS != "windows" {
		targetInfo, statErr := os.Stat(targetPath)
		require.NoError(t, statErr)
		require.Equal(t, targetPerm, targetInfo.Mode().Perm())
	}
}

func TestRestoreProjectUpdateBackup_RejectsRetargetedEnvSymlink(t *testing.T) {
	projectDir := t.TempDir()
	externalDir := t.TempDir()
	originalTarget := filepath.Join(externalDir, "original.env")
	newTarget := filepath.Join(externalDir, "new.env")
	require.NoError(t, os.WriteFile(originalTarget, []byte("VALUE=original\n"), 0o600))
	require.NoError(t, os.WriteFile(newTarget, []byte("VALUE=untouched\n"), 0o600))

	envPath := filepath.Join(projectDir, EffectiveEnvFileName)
	if err := os.Symlink(originalTarget, envPath); err != nil {
		t.Skipf("symlink creation is unavailable: %v", err)
	}
	backup, err := BackupProjectUpdateScope(t.Context(), projectDir, t.TempDir(), ProjectUpdateBackupScope{TopLevelFiles: true})
	require.NoError(t, err)

	require.NoError(t, WriteProjectFile(t.Context(), projectDir, projectDir, ".env", "VALUE=updated\n"))
	require.NoError(t, os.Remove(envPath))
	require.NoError(t, os.Symlink(newTarget, envPath))

	err = RestoreProjectUpdateBackup(t.Context(), projectDir, backup)
	require.Error(t, err)
	require.Contains(t, err.Error(), "symlink target changed")

	originalContent, readErr := os.ReadFile(originalTarget)
	require.NoError(t, readErr)
	require.Equal(t, "VALUE=updated\n", string(originalContent))
	newContent, readErr := os.ReadFile(newTarget)
	require.NoError(t, readErr)
	require.Equal(t, "VALUE=untouched\n", string(newContent))
	currentLinkTarget, readlinkErr := os.Readlink(envPath)
	require.NoError(t, readlinkErr)
	require.Equal(t, newTarget, currentLinkTarget)

	configPath := filepath.Join(projectDir, "config.txt")
	require.NoError(t, os.WriteFile(configPath, []byte("original config\n"), 0o600))
	configBackup, err := BackupProjectUpdateScope(t.Context(), projectDir, t.TempDir(), ProjectUpdateBackupScope{Paths: []string{"config.txt"}})
	require.NoError(t, err)
	require.NoError(t, os.Remove(configPath))
	require.NoError(t, os.Symlink(newTarget, configPath))
	err = RestoreProjectUpdateBackup(t.Context(), projectDir, configBackup)
	require.ErrorIs(t, err, acfs.ErrSymlink)
	newContent, readErr = os.ReadFile(newTarget)
	require.NoError(t, readErr)
	require.Equal(t, "VALUE=untouched\n", string(newContent))
}
