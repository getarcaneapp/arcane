package fs

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteFilesPermissions(t *testing.T) {
	// Save original perms
	origFilePerm := common.FilePerm
	origDirPerm := common.DirPerm
	defer func() {
		common.FilePerm = origFilePerm
		common.DirPerm = origDirPerm
	}()

	tmpDir := t.TempDir()
	projectsRoot := tmpDir
	projectDir := filepath.Join(tmpDir, "test-project")

	t.Run("WriteComposeFile uses custom permissions", func(t *testing.T) {
		common.FilePerm = 0o600
		common.DirPerm = 0o700

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
		common.FilePerm = 0o600
		common.DirPerm = 0o700

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
