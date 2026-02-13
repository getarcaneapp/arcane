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
		unsetEnv(t, "FILE_PERM")
		unsetEnv(t, "DIR_PERM")

		cfg := Load()
		assert.Equal(t, os.FileMode(0o644), cfg.FilePerm)
		assert.Equal(t, os.FileMode(0o755), cfg.DirPerm)
		assert.Equal(t, os.FileMode(0o644), common.FilePerm)
		assert.Equal(t, os.FileMode(0o755), common.DirPerm)
	})

	t.Run("Custom permissions", func(t *testing.T) {
		setEnv(t, "FILE_PERM", "0664")
		setEnv(t, "DIR_PERM", "0775")

		cfg := Load()
		assert.Equal(t, os.FileMode(0o664), cfg.FilePerm)
		assert.Equal(t, os.FileMode(0o775), cfg.DirPerm)
		assert.Equal(t, os.FileMode(0o664), common.FilePerm)
		assert.Equal(t, os.FileMode(0o775), common.DirPerm)
	})

	t.Run("Restrictive permissions", func(t *testing.T) {
		setEnv(t, "FILE_PERM", "0600")
		setEnv(t, "DIR_PERM", "0700")

		cfg := Load()
		assert.Equal(t, os.FileMode(0o600), cfg.FilePerm)
		assert.Equal(t, os.FileMode(0o700), cfg.DirPerm)
		assert.Equal(t, os.FileMode(0o600), common.FilePerm)
		assert.Equal(t, os.FileMode(0o700), common.DirPerm)
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
		err := os.WriteFile(secretFile, []byte(secretValue), 0o600)
		require.NoError(t, err)

		// Clear direct env var and set _FILE variant
		unsetEnv(t, "ENCRYPTION_KEY")
		unsetEnv(t, "ENCRYPTION_KEY__FILE")
		setEnv(t, "ENCRYPTION_KEY_FILE", secretFile)

		cfg := Load()
		assert.Equal(t, secretValue, cfg.EncryptionKey)
	})

	t.Run("Falls back to default when _FILE points to non-existent file", func(t *testing.T) {
		unsetEnv(t, "ENCRYPTION_KEY")
		setEnv(t, "ENCRYPTION_KEY_FILE", "/nonexistent/path/to/secret")
		unsetEnv(t, "ENCRYPTION_KEY__FILE")

		cfg := Load()
		assert.Equal(t, "arcane-dev-key-32-characters!!!", cfg.EncryptionKey)
	})

	t.Run("Load sensitive field from __FILE env var (double underscore)", func(t *testing.T) {
		// Create a temp file with the secret
		tmpDir := t.TempDir()
		secretFile := filepath.Join(tmpDir, "jwt_secret")
		testJWTValue := "test-jwt-stored-in-file"
		err := os.WriteFile(secretFile, []byte(testJWTValue+"\n"), 0o600) // Include trailing newline
		require.NoError(t, err)

		// Clear direct env var and set __FILE variant
		unsetEnv(t, "JWT_SECRET")
		unsetEnv(t, "JWT_SECRET_FILE")
		setEnv(t, "JWT_SECRET__FILE", secretFile)

		cfg := Load()
		assert.Equal(t, testJWTValue, cfg.JWTSecret) // Should be trimmed
	})

	t.Run("Direct env var is used when no _FILE variant exists", func(t *testing.T) {
		directValue := "direct-encryption-key-value-32chars!!"
		setEnv(t, "ENCRYPTION_KEY", directValue)
		unsetEnv(t, "ENCRYPTION_KEY_FILE")
		unsetEnv(t, "ENCRYPTION_KEY__FILE")

		cfg := Load()
		assert.Equal(t, directValue, cfg.EncryptionKey)
	})

	t.Run("_FILE takes precedence over direct env var", func(t *testing.T) {
		// Create a temp file with the secret
		tmpDir := t.TempDir()
		secretFile := filepath.Join(tmpDir, "encryption_key")
		fileValue := "value-from-file-takes-precedence!!"
		err := os.WriteFile(secretFile, []byte(fileValue), 0o600)
		require.NoError(t, err)

		// Set both direct and _FILE variants
		setEnv(t, "ENCRYPTION_KEY", "direct-value-should-be-ignored!!!")
		unsetEnv(t, "ENCRYPTION_KEY__FILE")
		setEnv(t, "ENCRYPTION_KEY_FILE", secretFile)

		cfg := Load()
		assert.Equal(t, fileValue, cfg.EncryptionKey)
	})

	t.Run("__FILE takes precedence over _FILE", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create single underscore file
		singleFile := filepath.Join(tmpDir, "single")
		err := os.WriteFile(singleFile, []byte("single-underscore-value-32chars!!"), 0o600)
		require.NoError(t, err)

		// Create double underscore file
		doubleFile := filepath.Join(tmpDir, "double")
		err = os.WriteFile(doubleFile, []byte("double-underscore-value-32chars!!"), 0o600)
		require.NoError(t, err)

		unsetEnv(t, "JWT_SECRET")
		setEnv(t, "JWT_SECRET_FILE", singleFile)
		setEnv(t, "JWT_SECRET__FILE", doubleFile)

		cfg := Load()
		assert.Equal(t, "double-underscore-value-32chars!!", cfg.JWTSecret)
	})

	t.Run("Non-sensitive fields do not support _FILE suffix", func(t *testing.T) {
		// Create a temp file
		tmpDir := t.TempDir()
		portFile := filepath.Join(tmpDir, "port")
		err := os.WriteFile(portFile, []byte("9999"), 0o600)
		require.NoError(t, err)

		// PORT is not marked with options:"file", so _FILE should not work
		unsetEnv(t, "PORT")
		setEnv(t, "PORT_FILE", portFile)

		cfg := Load()
		assert.Equal(t, "3552", cfg.Port) // Should use default, not file content
	})
}

func TestConfig_OptionsToLower(t *testing.T) {
	origLogLevel := os.Getenv("LOG_LEVEL")
	origEdgeTransport := os.Getenv("EDGE_TRANSPORT")
	defer restoreEnv("LOG_LEVEL", origLogLevel)
	defer restoreEnv("EDGE_TRANSPORT", origEdgeTransport)

	t.Run("LogLevel is converted to lowercase", func(t *testing.T) {
		setEnv(t, "LOG_LEVEL", "DEBUG")

		cfg := Load()
		assert.Equal(t, "debug", cfg.LogLevel)
	})

	t.Run("LogLevel mixed case is converted to lowercase", func(t *testing.T) {
		setEnv(t, "LOG_LEVEL", "WaRn")

		cfg := Load()
		assert.Equal(t, "warn", cfg.LogLevel)
	})

	t.Run("EdgeTransport is converted to lowercase", func(t *testing.T) {
		setEnv(t, "EDGE_TRANSPORT", "GRPC")

		cfg := Load()
		assert.Equal(t, "grpc", cfg.EdgeTransport)
	})

	t.Run("EdgeTransport defaults to auto", func(t *testing.T) {
		unsetEnv(t, "EDGE_TRANSPORT")

		cfg := Load()
		assert.Equal(t, "auto", cfg.EdgeTransport)
	})
}

func TestConfig_ListenAddr(t *testing.T) {
	tests := []struct {
		name     string
		listen   string
		port     string
		expected string
	}{
		{
			name:     "empty listen uses all interfaces",
			listen:   "",
			port:     "3553",
			expected: ":3553",
		},
		{
			name:     "ipv4 listen",
			listen:   "127.0.0.1",
			port:     "3553",
			expected: "127.0.0.1:3553",
		},
		{
			name:     "ipv6 listen",
			listen:   "::1",
			port:     "3553",
			expected: "[::1]:3553",
		},
		{
			name:     "empty port falls back to default",
			listen:   "127.0.0.1",
			port:     "",
			expected: "127.0.0.1:3552",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			cfg := &Config{
				Listen: testCase.listen,
				Port:   testCase.port,
			}
			assert.Equal(t, testCase.expected, cfg.ListenAddr())
		})
	}
}

func TestConfig_GetManagerGRPCAddr(t *testing.T) {
	t.Run("uses manager api url explicit port when present", func(t *testing.T) {
		cfg := &Config{
			ManagerApiUrl: "https://manager.example.com:8443/api",
		}
		assert.Equal(t, "manager.example.com:8443", cfg.GetManagerGRPCAddr())
	})

	t.Run("defaults to manager api https port when port is not set", func(t *testing.T) {
		cfg := &Config{
			ManagerApiUrl: "https://manager.example.com/api",
		}
		assert.Equal(t, "manager.example.com:443", cfg.GetManagerGRPCAddr())
	})

	t.Run("defaults to manager api http port when port is not set", func(t *testing.T) {
		cfg := &Config{
			ManagerApiUrl: "http://manager.example.com/api",
		}
		assert.Equal(t, "manager.example.com:80", cfg.GetManagerGRPCAddr())
	})

	t.Run("supports reverse-proxy path prefixes", func(t *testing.T) {
		cfg := &Config{
			ManagerApiUrl: "https://manager.example.com/arcane/api/",
		}
		assert.Equal(t, "manager.example.com:443", cfg.GetManagerGRPCAddr())
	})

	t.Run("supports ipv6 hosts behind reverse proxies", func(t *testing.T) {
		cfg := &Config{
			ManagerApiUrl: "https://[2001:db8::1]/arcane/api",
		}
		assert.Equal(t, "[2001:db8::1]:443", cfg.GetManagerGRPCAddr())
	})

	t.Run("returns empty for invalid manager url", func(t *testing.T) {
		cfg := &Config{
			ManagerApiUrl: "://bad-url",
		}
		assert.Equal(t, "", cfg.GetManagerGRPCAddr())
	})
}

func TestConfig_GetManagerBaseURL(t *testing.T) {
	t.Run("strips trailing slash and api suffix", func(t *testing.T) {
		cfg := &Config{
			ManagerApiUrl: "https://manager.example.com/api/",
		}
		assert.Equal(t, "https://manager.example.com", cfg.GetManagerBaseURL())
	})

	t.Run("keeps reverse-proxy path prefix", func(t *testing.T) {
		cfg := &Config{
			ManagerApiUrl: "https://manager.example.com/arcane/api/",
		}
		assert.Equal(t, "https://manager.example.com/arcane", cfg.GetManagerBaseURL())
	})
}

func restoreEnv(key, value string) {
	if value == "" {
		_ = os.Unsetenv(key)
	} else {
		_ = os.Setenv(key, value)
	}
}

func setEnv(t *testing.T, key, value string) {
	t.Helper()
	require.NoError(t, os.Setenv(key, value))
}

func unsetEnv(t *testing.T, key string) {
	t.Helper()
	require.NoError(t, os.Unsetenv(key))
}
