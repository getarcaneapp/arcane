package config

import (
	"os"
	"testing"

	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/stretchr/testify/assert"
)

func TestConfig_LoadPermissions(t *testing.T) {
	// Save original env and common perms
	origFilePerm := os.Getenv("FILE_PERM")
	origDirPerm := os.Getenv("DIR_PERM")
	origCommonFilePerm := common.FilePerm
	origCommonDirPerm := common.DirPerm
	
	defer func() {
		os.Setenv("FILE_PERM", origFilePerm)
		os.Setenv("DIR_PERM", origDirPerm)
		common.FilePerm = origCommonFilePerm
		common.DirPerm = origCommonDirPerm
	}()

	t.Run("Default permissions", func(t *testing.T) {
		os.Unsetenv("FILE_PERM")
		os.Unsetenv("DIR_PERM")
		
		cfg := Load()
		assert.Equal(t, os.FileMode(0644), cfg.FilePerm)
		assert.Equal(t, os.FileMode(0755), cfg.DirPerm)
		assert.Equal(t, os.FileMode(0644), common.FilePerm)
		assert.Equal(t, os.FileMode(0755), common.DirPerm)
	})

	t.Run("Custom permissions", func(t *testing.T) {
		os.Setenv("FILE_PERM", "0664")
		os.Setenv("DIR_PERM", "0775")
		
		cfg := Load()
		assert.Equal(t, os.FileMode(0664), cfg.FilePerm)
		assert.Equal(t, os.FileMode(0775), cfg.DirPerm)
		assert.Equal(t, os.FileMode(0664), common.FilePerm)
		assert.Equal(t, os.FileMode(0775), common.DirPerm)
	})

	t.Run("Restrictive permissions", func(t *testing.T) {
		os.Setenv("FILE_PERM", "0600")
		os.Setenv("DIR_PERM", "0700")
		
		cfg := Load()
		assert.Equal(t, os.FileMode(0600), cfg.FilePerm)
		assert.Equal(t, os.FileMode(0700), cfg.DirPerm)
		assert.Equal(t, os.FileMode(0600), common.FilePerm)
		assert.Equal(t, os.FileMode(0700), common.DirPerm)
	})
}
