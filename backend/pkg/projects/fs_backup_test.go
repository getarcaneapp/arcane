package projects

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRestoreProjectUpdateBackup_UndoesRenameWhenSourceWasRecreated(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "a"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "a", "keep.txt"), []byte("keep\n"), 0o644))

	// Batch: rename a -> b, create_folder a, then a later change fails.
	scope := ProjectUpdateBackupScope{
		Paths:       []string{"a"},
		RenamedDirs: [][2]string{{"a", "b"}},
	}
	backup, err := BackupProjectUpdateScope(projectDir, t.TempDir(), scope)
	require.NoError(t, err)

	require.NoError(t, os.Rename(filepath.Join(projectDir, "a"), filepath.Join(projectDir, "b")))
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "a"), 0o755))

	require.NoError(t, RestoreProjectUpdateBackup(projectDir, backup))

	require.NoDirExists(t, filepath.Join(projectDir, "b"))
	require.FileExists(t, filepath.Join(projectDir, "a", "keep.txt"))
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

	backup, err := BackupProjectUpdateScope(projectDir, t.TempDir(), ProjectUpdateBackupScope{TopLevelFiles: true})
	require.NoError(t, err)
	require.NotNil(t, backup.envSymlink)

	require.NoError(t, WriteEnvFile(projectDir, projectDir, "VALUE=updated\n"))
	require.NoError(t, RestoreProjectUpdateBackup(projectDir, backup))

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
	backup, err := BackupProjectUpdateScope(projectDir, t.TempDir(), ProjectUpdateBackupScope{TopLevelFiles: true})
	require.NoError(t, err)

	require.NoError(t, WriteEnvFile(projectDir, projectDir, "VALUE=updated\n"))
	require.NoError(t, os.Remove(envPath))
	require.NoError(t, os.Symlink(newTarget, envPath))

	err = RestoreProjectUpdateBackup(projectDir, backup)
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
}
