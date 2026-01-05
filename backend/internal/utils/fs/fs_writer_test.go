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
		common.FilePerm = 0600
		common.DirPerm = 0700

		err := WriteComposeFile(projectsRoot, projectDir, "services: {}")
		require.NoError(t, err)

		composePath := filepath.Join(projectDir, "compose.yaml")
		info, err := os.Stat(composePath)
		require.NoError(t, err)

		if runtime.GOOS != "windows" {
			assert.Equal(t, os.FileMode(0600), info.Mode().Perm())

			dirInfo, err := os.Stat(projectDir)
			require.NoError(t, err)
			assert.Equal(t, os.FileMode(0700), dirInfo.Mode().Perm())
		}
	})

	t.Run("WriteEnvFile uses custom permissions", func(t *testing.T) {
		common.FilePerm = 0600
		common.DirPerm = 0700

		err := WriteEnvFile(projectsRoot, projectDir, "VAR=VAL")
		require.NoError(t, err)

		envPath := filepath.Join(projectDir, ".env")
		info, err := os.Stat(envPath)
		require.NoError(t, err)

		if runtime.GOOS != "windows" {
			assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
		}
	})
}
