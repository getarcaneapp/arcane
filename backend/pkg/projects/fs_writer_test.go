package projects

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	pkgutils "github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteFilesPermissions(t *testing.T) {
	// Save original perms
	origFilePerm := pkgutils.FilePerm
	origDirPerm := pkgutils.DirPerm
	defer func() {
		pkgutils.FilePerm = origFilePerm
		pkgutils.DirPerm = origDirPerm
	}()

	tmpDir := t.TempDir()
	projectsRoot := tmpDir
	projectDir := filepath.Join(tmpDir, "test-project")

	t.Run("WriteComposeFile uses custom permissions", func(t *testing.T) {
		pkgutils.FilePerm = 0o600
		pkgutils.DirPerm = 0o700

		err := WriteComposeFile(projectsRoot, projectDir, "services: {}")
		require.NoError(t, err)

		composePath := filepath.Join(projectDir, "compose.yaml")
		info, err := os.Stat(composePath)
		require.NoError(t, err)

		if runtime.GOOS != "windows" {
			assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

			dirInfo, err := os.Stat(projectDir)
			require.NoError(t, err)
			assert.Equal(t, os.FileMode(0o700), dirInfo.Mode().Perm())
		}
	})

	t.Run("WriteEnvFile uses custom permissions", func(t *testing.T) {
		pkgutils.FilePerm = 0o600
		pkgutils.DirPerm = 0o700

		err := WriteEnvFile(projectsRoot, projectDir, "VAR=VAL")
		require.NoError(t, err)

		envPath := filepath.Join(projectDir, ".env")
		info, err := os.Stat(envPath)
		require.NoError(t, err)

		if runtime.GOOS != "windows" {
			assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
		}
	})
}

func TestWriteProjectFiles(t *testing.T) {
	tmpDir := t.TempDir()
	projectsRoot := tmpDir
	projectDir := filepath.Join(tmpDir, "test-project")

	t.Run("creates new project with empty env when envContent is nil", func(t *testing.T) {
		err := WriteProjectFiles(projectsRoot, projectDir, "services: {}", nil)
		require.NoError(t, err)

		envPath := filepath.Join(projectDir, ".env")
		content, err := os.ReadFile(envPath)
		require.NoError(t, err)
		assert.Empty(t, string(content))
	})

	t.Run("preserves existing env when envContent is nil", func(t *testing.T) {
		envPath := filepath.Join(projectDir, ".env")
		expected := "EXISTING=true"
		err := os.WriteFile(envPath, []byte(expected), 0o600)
		require.NoError(t, err)

		err = WriteProjectFiles(projectsRoot, projectDir, "services: { updated: true }", nil)
		require.NoError(t, err)

		content, err := os.ReadFile(envPath)
		require.NoError(t, err)
		assert.Equal(t, expected, string(content))
	})

	t.Run("overwrites env when envContent is provided", func(t *testing.T) {
		envPath := filepath.Join(projectDir, ".env")
		newContent := "NEW=true"
		err := WriteProjectFiles(projectsRoot, projectDir, "services: {}", &newContent)
		require.NoError(t, err)

		content, err := os.ReadFile(envPath)
		require.NoError(t, err)
		assert.Equal(t, newContent, string(content))
	})
}

func TestWriteComposeFile_PreservesExistingPodmanComposeNames(t *testing.T) {
	testCases := []struct {
		name     string
		fileName string
	}{
		{name: "podman-compose.yaml", fileName: "podman-compose.yaml"},
		{name: "podman-compose.yml", fileName: "podman-compose.yml"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tmpDir := t.TempDir()
			projectsRoot := tmpDir
			projectDir := filepath.Join(tmpDir, "test-project")
			require.NoError(t, os.MkdirAll(projectDir, 0o755))

			existingComposePath := filepath.Join(projectDir, tc.fileName)
			require.NoError(t, os.WriteFile(existingComposePath, []byte("services: {}"), 0o600))

			expectedContent := "services:\n  app:\n    image: nginx:alpine\n"
			err := WriteComposeFile(projectsRoot, projectDir, expectedContent)
			require.NoError(t, err)

			actualContent, err := os.ReadFile(existingComposePath)
			require.NoError(t, err)
			assert.Equal(t, expectedContent, string(actualContent))

			_, err = os.Stat(filepath.Join(projectDir, "compose.yaml"))
			assert.True(t, os.IsNotExist(err), "compose.yaml should not be created when existing podman-compose file is present")
		})
	}
}

func TestWriteSyncedDirectory_HonorsExecutableBit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file mode bits are not represented on Windows FS")
	}

	root := t.TempDir()
	project := filepath.Join(root, "myproject")
	files := []SyncFile{
		{RelativePath: "compose.yml", Content: []byte("services:\n  app:\n    image: alpine\n"), Executable: false},
		{RelativePath: "scripts/pre-deploy.sh", Content: []byte("#!/bin/sh\necho hi\n"), Executable: true},
		{RelativePath: "README.md", Content: []byte("readme"), Executable: false},
	}

	_, err := WriteSyncedDirectory(root, project, files)
	require.NoError(t, err)

	composeInfo, err := os.Stat(filepath.Join(project, "compose.yml"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0), composeInfo.Mode().Perm()&0o111, "compose.yml should not be executable")

	scriptInfo, err := os.Stat(filepath.Join(project, "scripts/pre-deploy.sh"))
	require.NoError(t, err)
	assert.NotEqual(t, os.FileMode(0), scriptInfo.Mode().Perm()&0o111, "scripts/pre-deploy.sh should be executable")
}

func TestWriteSyncedDirectory_DowngradesExecutableBit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file mode bits are not represented on Windows FS")
	}

	root := t.TempDir()
	project := filepath.Join(root, "myproject")
	// First write: script committed as +x.
	_, err := WriteSyncedDirectory(root, project, []SyncFile{
		{RelativePath: "scripts/hook.sh", Content: []byte("#!/bin/sh\n"), Executable: true},
	})
	require.NoError(t, err)
	first, err := os.Stat(filepath.Join(project, "scripts/hook.sh"))
	require.NoError(t, err)
	require.NotEqual(t, os.FileMode(0), first.Mode().Perm()&0o111)

	// Second write: same file, now without +x (e.g. the repo dropped the bit).
	// The write path must re-chmod so the on-disk mode tracks the repo, not
	// the previous write.
	_, err = WriteSyncedDirectory(root, project, []SyncFile{
		{RelativePath: "scripts/hook.sh", Content: []byte("#!/bin/sh\necho updated\n"), Executable: false},
	})
	require.NoError(t, err)
	second, err := os.Stat(filepath.Join(project, "scripts/hook.sh"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0), second.Mode().Perm()&0o111, "executable bit should clear on update when repo no longer marks +x")
}

func TestWriteSyncedDirectory_UpgradesExecutableBitOnUpdate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file mode bits are not represented on Windows FS")
	}

	root := t.TempDir()
	project := filepath.Join(root, "myproject")

	// First write: file committed without +x.
	_, err := WriteSyncedDirectory(root, project, []SyncFile{
		{RelativePath: "scripts/hook.sh", Content: []byte("#!/bin/sh\n"), Executable: false},
	})
	require.NoError(t, err)
	first, err := os.Stat(filepath.Join(project, "scripts/hook.sh"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0), first.Mode().Perm()&0o111)

	// Second write: same file, now with +x (e.g. the repo added the bit).
	// The write path must apply the new mode on an update, not just on create.
	_, err = WriteSyncedDirectory(root, project, []SyncFile{
		{RelativePath: "scripts/hook.sh", Content: []byte("#!/bin/sh\necho updated\n"), Executable: true},
	})
	require.NoError(t, err)
	second, err := os.Stat(filepath.Join(project, "scripts/hook.sh"))
	require.NoError(t, err)
	assert.NotEqual(t, os.FileMode(0), second.Mode().Perm()&0o111, "executable bit should set on update when repo marks +x")
}

func TestWriteComposeFile_PreservesExistingCustomComposeNames(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	projectsRoot := tmpDir
	projectDir := filepath.Join(tmpDir, "Radarr-3")
	require.NoError(t, os.MkdirAll(projectDir, 0o755))

	existingComposePath := filepath.Join(projectDir, "radarr.yaml")
	require.NoError(t, os.WriteFile(existingComposePath, []byte("services: {}"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "config.yaml"), []byte("x-config: true\n"), 0o600))

	expectedContent := "services:\n  app:\n    image: nginx:alpine\n"
	err := WriteComposeFile(projectsRoot, projectDir, expectedContent)
	require.NoError(t, err)

	actualContent, err := os.ReadFile(existingComposePath)
	require.NoError(t, err)
	assert.Equal(t, expectedContent, string(actualContent))

	_, err = os.Stat(filepath.Join(projectDir, "compose.yaml"))
	assert.True(t, os.IsNotExist(err), "compose.yaml should not be created when an existing custom compose file is present")
}
