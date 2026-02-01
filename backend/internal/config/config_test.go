package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfig_LoadPermissions(t *testing.T) {
	// Save original env and common perms
	origFilePerm := os.Getenv("FILE_PERM")
	origDirPerm := os.Getenv("DIR_PERM")
	origCommonFilePerm := common.FilePerm
	origCommonDirPerm := common.DirPerm

	defer func() {
		restoreEnv("FILE_PERM", origFilePerm)
		restoreEnv("DIR_PERM", origDirPerm)
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

func TestConfig_DockerSecretsFileSupport(t *testing.T) {
	// Save original env vars
	origEncryptionKey := os.Getenv("ENCRYPTION_KEY")
	origEncryptionKeyFile := os.Getenv("ENCRYPTION_KEY_FILE")
	origEncryptionKeyDoubleFile := os.Getenv("ENCRYPTION_KEY__FILE")
	origJWTSecret := os.Getenv("JWT_SECRET")
	origJWTSecretFile := os.Getenv("JWT_SECRET_FILE")
	origJWTSecretDoubleFile := os.Getenv("JWT_SECRET__FILE")

	defer func() {
		restoreEnv("ENCRYPTION_KEY", origEncryptionKey)
		restoreEnv("ENCRYPTION_KEY_FILE", origEncryptionKeyFile)
		restoreEnv("ENCRYPTION_KEY__FILE", origEncryptionKeyDoubleFile)
		restoreEnv("JWT_SECRET", origJWTSecret)
		restoreEnv("JWT_SECRET_FILE", origJWTSecretFile)
		restoreEnv("JWT_SECRET__FILE", origJWTSecretDoubleFile)
	}()

	t.Run("Load sensitive field from _FILE env var", func(t *testing.T) {
		// Create a temp file with the secret
		tmpDir := t.TempDir()
		secretFile := filepath.Join(tmpDir, "encryption_key")
		secretValue := "my-super-secret-encryption-key-32chars!"
		err := os.WriteFile(secretFile, []byte(secretValue), 0600)
		require.NoError(t, err)

		// Clear direct env var and set _FILE variant
		os.Unsetenv("ENCRYPTION_KEY")
		os.Unsetenv("ENCRYPTION_KEY__FILE")
		os.Setenv("ENCRYPTION_KEY_FILE", secretFile)

		cfg := Load()
		assert.Equal(t, secretValue, cfg.EncryptionKey)
	})

	t.Run("Falls back to default when _FILE points to non-existent file", func(t *testing.T) {
		os.Unsetenv("ENCRYPTION_KEY")
		os.Setenv("ENCRYPTION_KEY_FILE", "/nonexistent/path/to/secret")
		os.Unsetenv("ENCRYPTION_KEY__FILE")

		cfg := Load()
		assert.Equal(t, "arcane-dev-key-32-characters!!!", cfg.EncryptionKey)
	})

	t.Run("Load sensitive field from __FILE env var (double underscore)", func(t *testing.T) {
		// Create a temp file with the secret
		tmpDir := t.TempDir()
		secretFile := filepath.Join(tmpDir, "jwt_secret")
		testJWTValue := "test-jwt-stored-in-file"
		err := os.WriteFile(secretFile, []byte(testJWTValue+"\n"), 0600) // Include trailing newline
		require.NoError(t, err)

		// Clear direct env var and set __FILE variant
		os.Unsetenv("JWT_SECRET")
		os.Unsetenv("JWT_SECRET_FILE")
		os.Setenv("JWT_SECRET__FILE", secretFile)

		cfg := Load()
		assert.Equal(t, testJWTValue, cfg.JWTSecret) // Should be trimmed
	})

	t.Run("Direct env var is used when no _FILE variant exists", func(t *testing.T) {
		directValue := "direct-encryption-key-value-32chars!!"
		os.Setenv("ENCRYPTION_KEY", directValue)
		os.Unsetenv("ENCRYPTION_KEY_FILE")
		os.Unsetenv("ENCRYPTION_KEY__FILE")

		cfg := Load()
		assert.Equal(t, directValue, cfg.EncryptionKey)
	})

	t.Run("_FILE takes precedence over direct env var", func(t *testing.T) {
		// Create a temp file with the secret
		tmpDir := t.TempDir()
		secretFile := filepath.Join(tmpDir, "encryption_key")
		fileValue := "value-from-file-takes-precedence!!"
		err := os.WriteFile(secretFile, []byte(fileValue), 0600)
		require.NoError(t, err)

		// Set both direct and _FILE variants
		os.Setenv("ENCRYPTION_KEY", "direct-value-should-be-ignored!!!")
		os.Unsetenv("ENCRYPTION_KEY__FILE")
		os.Setenv("ENCRYPTION_KEY_FILE", secretFile)

		cfg := Load()
		assert.Equal(t, fileValue, cfg.EncryptionKey)
	})

	t.Run("__FILE takes precedence over _FILE", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create single underscore file
		singleFile := filepath.Join(tmpDir, "single")
		err := os.WriteFile(singleFile, []byte("single-underscore-value-32chars!!"), 0600)
		require.NoError(t, err)

		// Create double underscore file
		doubleFile := filepath.Join(tmpDir, "double")
		err = os.WriteFile(doubleFile, []byte("double-underscore-value-32chars!!"), 0600)
		require.NoError(t, err)

		os.Unsetenv("JWT_SECRET")
		os.Setenv("JWT_SECRET_FILE", singleFile)
		os.Setenv("JWT_SECRET__FILE", doubleFile)

		cfg := Load()
		assert.Equal(t, "double-underscore-value-32chars!!", cfg.JWTSecret)
	})

	t.Run("Non-sensitive fields do not support _FILE suffix", func(t *testing.T) {
		// Create a temp file
		tmpDir := t.TempDir()
		portFile := filepath.Join(tmpDir, "port")
		err := os.WriteFile(portFile, []byte("9999"), 0600)
		require.NoError(t, err)

		// PORT is not marked with options:"file", so _FILE should not work
		os.Unsetenv("PORT")
		os.Setenv("PORT_FILE", portFile)

		cfg := Load()
		assert.Equal(t, "3552", cfg.Port) // Should use default, not file content
	})
}

func TestConfig_OptionsToLower(t *testing.T) {
	origLogLevel := os.Getenv("LOG_LEVEL")
	defer restoreEnv("LOG_LEVEL", origLogLevel)

	t.Run("LogLevel is converted to lowercase", func(t *testing.T) {
		os.Setenv("LOG_LEVEL", "DEBUG")

		cfg := Load()
		assert.Equal(t, "debug", cfg.LogLevel)
	})

	t.Run("LogLevel mixed case is converted to lowercase", func(t *testing.T) {
		os.Setenv("LOG_LEVEL", "WaRn")

		cfg := Load()
		assert.Equal(t, "warn", cfg.LogLevel)
	})
}

func restoreEnv(key, value string) {
	if value == "" {
		os.Unsetenv(key)
	} else {
		os.Setenv(key, value)
	}
}
