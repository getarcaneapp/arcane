package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/httpx"
)

func TestConfig_LoadPermissions(t *testing.T) {
	// Save original env and global perms
	origFilePerm := os.Getenv("ARCANE_FILE_PERM")
	origDirPerm := os.Getenv("ARCANE_DIR_PERM")
	origPkgFilePerm := utils.FilePerm
	origPkgDirPerm := utils.DirPerm

	defer func() {
		restoreEnv("ARCANE_FILE_PERM", origFilePerm)
		restoreEnv("ARCANE_DIR_PERM", origDirPerm)
		utils.FilePerm = origPkgFilePerm
		utils.DirPerm = origPkgDirPerm
	}()

	t.Run("Default permissions", func(t *testing.T) {
		unsetEnv(t, "ARCANE_FILE_PERM")
		unsetEnv(t, "ARCANE_DIR_PERM")

		cfg := mustLoad(t)
		assert.Equal(t, os.FileMode(0o644), cfg.FilePerm)
		assert.Equal(t, os.FileMode(0o755), cfg.DirPerm)
		assert.Equal(t, os.FileMode(0o644), utils.FilePerm)
		assert.Equal(t, os.FileMode(0o755), utils.DirPerm)
	})

	t.Run("Custom permissions", func(t *testing.T) {
		setEnv(t, "ARCANE_FILE_PERM", "0664")
		setEnv(t, "ARCANE_DIR_PERM", "0775")

		cfg := mustLoad(t)
		assert.Equal(t, os.FileMode(0o664), cfg.FilePerm)
		assert.Equal(t, os.FileMode(0o775), cfg.DirPerm)
		assert.Equal(t, os.FileMode(0o664), utils.FilePerm)
		assert.Equal(t, os.FileMode(0o775), utils.DirPerm)
	})

	t.Run("Restrictive permissions", func(t *testing.T) {
		setEnv(t, "ARCANE_FILE_PERM", "0600")
		setEnv(t, "ARCANE_DIR_PERM", "0700")

		cfg := mustLoad(t)
		assert.Equal(t, os.FileMode(0o600), cfg.FilePerm)
		assert.Equal(t, os.FileMode(0o700), cfg.DirPerm)
		assert.Equal(t, os.FileMode(0o600), utils.FilePerm)
		assert.Equal(t, os.FileMode(0o700), utils.DirPerm)
	})
}

func TestConfig_DockerSecretsFileSupport(t *testing.T) {
	// Save original env vars
	origEncryptionKey := os.Getenv("ARCANE_ENCRYPTION_KEY")
	origEncryptionKeyFile := os.Getenv("ARCANE_ENCRYPTION_KEY_FILE")
	origEncryptionKeyDoubleFile := os.Getenv("ARCANE_ENCRYPTION_KEY__FILE")
	origJWTSecret := os.Getenv("ARCANE_AGENT_TOKEN")
	origJWTSecretFile := os.Getenv("ARCANE_AGENT_TOKEN_FILE")
	origJWTSecretDoubleFile := os.Getenv("ARCANE_AGENT_TOKEN__FILE")
	origDefaultAPIKey := os.Getenv("ARCANE_ADMIN_STATIC_API_KEY")
	origDefaultAPIKeyFile := os.Getenv("ARCANE_ADMIN_STATIC_API_KEY_FILE")
	origDefaultAPIKeyDoubleFile := os.Getenv("ARCANE_ADMIN_STATIC_API_KEY__FILE")

	defer func() {
		restoreEnv("ARCANE_ENCRYPTION_KEY", origEncryptionKey)
		restoreEnv("ARCANE_ENCRYPTION_KEY_FILE", origEncryptionKeyFile)
		restoreEnv("ARCANE_ENCRYPTION_KEY__FILE", origEncryptionKeyDoubleFile)
		restoreEnv("ARCANE_AGENT_TOKEN", origJWTSecret)
		restoreEnv("ARCANE_AGENT_TOKEN_FILE", origJWTSecretFile)
		restoreEnv("ARCANE_AGENT_TOKEN__FILE", origJWTSecretDoubleFile)
		restoreEnv("ARCANE_ADMIN_STATIC_API_KEY", origDefaultAPIKey)
		restoreEnv("ARCANE_ADMIN_STATIC_API_KEY_FILE", origDefaultAPIKeyFile)
		restoreEnv("ARCANE_ADMIN_STATIC_API_KEY__FILE", origDefaultAPIKeyDoubleFile)
	}()

	t.Run("Load sensitive field from _FILE env var", func(t *testing.T) {
		// Create a temp file with the secret
		tmpDir := t.TempDir()
		secretFile := filepath.Join(tmpDir, "encryption_key")
		secretValue := "my-super-secret-encryption-key-32chars!"
		err := os.WriteFile(secretFile, []byte(secretValue), 0o600)
		require.NoError(t, err)

		// Clear direct env var and set _FILE variant
		unsetEnv(t, "ARCANE_ENCRYPTION_KEY")
		unsetEnv(t, "ARCANE_ENCRYPTION_KEY__FILE")
		setEnv(t, "ARCANE_ENCRYPTION_KEY_FILE", secretFile)

		cfg := mustLoad(t)
		assert.Equal(t, secretValue, cfg.EncryptionKey)
	})

	t.Run("Falls back to default when _FILE points to non-existent file", func(t *testing.T) {
		unsetEnv(t, "ARCANE_ENCRYPTION_KEY")
		setEnv(t, "ARCANE_ENCRYPTION_KEY_FILE", "/nonexistent/path/to/secret")
		unsetEnv(t, "ARCANE_ENCRYPTION_KEY__FILE")

		cfg := mustLoad(t)
		assert.Empty(t, cfg.EncryptionKey)
	})

	t.Run("Load sensitive field from __FILE env var (double underscore)", func(t *testing.T) {
		// Create a temp file with the secret
		tmpDir := t.TempDir()
		secretFile := filepath.Join(tmpDir, "jwt_secret")
		testJWTValue := "test-jwt-stored-in-file"
		err := os.WriteFile(secretFile, []byte(testJWTValue+"\n"), 0o600) // Include trailing newline
		require.NoError(t, err)

		// Clear direct env var and set __FILE variant
		unsetEnv(t, "ARCANE_AGENT_TOKEN")
		unsetEnv(t, "ARCANE_AGENT_TOKEN_FILE")
		setEnv(t, "ARCANE_AGENT_TOKEN__FILE", secretFile)

		cfg := mustLoad(t)
		assert.Equal(t, testJWTValue, cfg.AgentToken) // Should be trimmed
	})

	t.Run("Direct env var is used when no _FILE variant exists", func(t *testing.T) {
		directValue := "direct-encryption-key-value-32chars!!"
		setEnv(t, "ARCANE_ENCRYPTION_KEY", directValue)
		unsetEnv(t, "ARCANE_ENCRYPTION_KEY_FILE")
		unsetEnv(t, "ARCANE_ENCRYPTION_KEY__FILE")

		cfg := mustLoad(t)
		assert.Equal(t, directValue, cfg.EncryptionKey)
	})

	t.Run("Default admin API key supports direct env var", func(t *testing.T) {
		directValue := "arc_directbootstrapkey1234567890"
		setEnv(t, "ARCANE_ADMIN_STATIC_API_KEY", directValue)
		unsetEnv(t, "ARCANE_ADMIN_STATIC_API_KEY_FILE")
		unsetEnv(t, "ARCANE_ADMIN_STATIC_API_KEY__FILE")

		cfg := mustLoad(t)
		assert.Equal(t, directValue, cfg.AdminStaticAPIKey)
	})

	t.Run("_FILE takes precedence over direct env var", func(t *testing.T) {
		// Create a temp file with the secret
		tmpDir := t.TempDir()
		secretFile := filepath.Join(tmpDir, "encryption_key")
		fileValue := "value-from-file-takes-precedence!!"
		err := os.WriteFile(secretFile, []byte(fileValue), 0o600)
		require.NoError(t, err)

		// Set both direct and _FILE variants
		setEnv(t, "ARCANE_ENCRYPTION_KEY", "direct-value-should-be-ignored!!!")
		unsetEnv(t, "ARCANE_ENCRYPTION_KEY__FILE")
		setEnv(t, "ARCANE_ENCRYPTION_KEY_FILE", secretFile)

		cfg := mustLoad(t)
		assert.Equal(t, fileValue, cfg.EncryptionKey)
	})

	t.Run("Default admin API key loads from _FILE env var", func(t *testing.T) {
		tmpDir := t.TempDir()
		secretFile := filepath.Join(tmpDir, "default_api_key")
		secretValue := "arc_filebootstrapkey1234567890"
		err := os.WriteFile(secretFile, []byte(secretValue), 0o600)
		require.NoError(t, err)

		unsetEnv(t, "ARCANE_ADMIN_STATIC_API_KEY")
		unsetEnv(t, "ARCANE_ADMIN_STATIC_API_KEY__FILE")
		setEnv(t, "ARCANE_ADMIN_STATIC_API_KEY_FILE", secretFile)

		cfg := mustLoad(t)
		assert.Equal(t, secretValue, cfg.AdminStaticAPIKey)
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

		unsetEnv(t, "ARCANE_AGENT_TOKEN")
		setEnv(t, "ARCANE_AGENT_TOKEN_FILE", singleFile)
		setEnv(t, "ARCANE_AGENT_TOKEN__FILE", doubleFile)

		cfg := mustLoad(t)
		assert.Equal(t, "double-underscore-value-32chars!!", cfg.AgentToken)
	})

	t.Run("Default admin API key __FILE takes precedence over _FILE", func(t *testing.T) {
		tmpDir := t.TempDir()

		singleFile := filepath.Join(tmpDir, "default_api_key_single")
		err := os.WriteFile(singleFile, []byte("arc_singlebootstrapkey1234567890"), 0o600)
		require.NoError(t, err)

		doubleFile := filepath.Join(tmpDir, "default_api_key_double")
		err = os.WriteFile(doubleFile, []byte("arc_doublebootstrapkey1234567890\n"), 0o600)
		require.NoError(t, err)

		unsetEnv(t, "ARCANE_ADMIN_STATIC_API_KEY")
		setEnv(t, "ARCANE_ADMIN_STATIC_API_KEY_FILE", singleFile)
		setEnv(t, "ARCANE_ADMIN_STATIC_API_KEY__FILE", doubleFile)

		cfg := mustLoad(t)
		assert.Equal(t, "arc_doublebootstrapkey1234567890", cfg.AdminStaticAPIKey)
	})

	t.Run("Non-sensitive fields do not support _FILE suffix", func(t *testing.T) {
		// Create a temp file
		tmpDir := t.TempDir()
		portFile := filepath.Join(tmpDir, "port")
		err := os.WriteFile(portFile, []byte("9999"), 0o600)
		require.NoError(t, err)

		// ARCANE_PORT is not marked with options:"file", so _FILE should not work
		unsetEnv(t, "ARCANE_PORT")
		setEnv(t, "ARCANE_PORT_FILE", portFile)

		cfg := mustLoad(t)
		assert.Equal(t, "3552", cfg.Port) // Should use default, not file content
	})
}

func TestConfig_OptionsToLower(t *testing.T) {
	origLogLevel := os.Getenv("ARCANE_LOG_LEVEL")
	origAgentTransport := os.Getenv("ARCANE_AGENT_TRANSPORT")
	origAgentMTLSMode := os.Getenv("ARCANE_AGENT_MTLS_MODE")
	defer restoreEnv("ARCANE_LOG_LEVEL", origLogLevel)
	defer restoreEnv("ARCANE_AGENT_TRANSPORT", origAgentTransport)
	defer restoreEnv("ARCANE_AGENT_MTLS_MODE", origAgentMTLSMode)

	t.Run("LogLevel is converted to lowercase", func(t *testing.T) {
		setEnv(t, "ARCANE_LOG_LEVEL", "DEBUG")

		cfg := mustLoad(t)
		assert.Equal(t, "debug", cfg.LogLevel)
	})

	t.Run("LogLevel mixed case is converted to lowercase", func(t *testing.T) {
		setEnv(t, "ARCANE_LOG_LEVEL", "WaRn")

		cfg := mustLoad(t)
		assert.Equal(t, "warn", cfg.LogLevel)
	})

	t.Run("AgentTransport is converted to lowercase", func(t *testing.T) {
		setEnv(t, "ARCANE_AGENT_TRANSPORT", "GRPC")

		cfg := mustLoad(t)
		assert.Equal(t, "grpc", cfg.AgentTransport)
	})

	t.Run("AgentTransport defaults to auto", func(t *testing.T) {
		unsetEnv(t, "ARCANE_AGENT_TRANSPORT")

		cfg := mustLoad(t)
		assert.Equal(t, "auto", cfg.AgentTransport)
	})

	t.Run("AgentMTLSMode is converted to lowercase", func(t *testing.T) {
		setEnv(t, "ARCANE_AGENT_MTLS_MODE", "REQUIRED")

		cfg := mustLoad(t)
		assert.Equal(t, "required", cfg.AgentMTLSMode)
	})

	t.Run("AgentMTLSMode defaults to disabled", func(t *testing.T) {
		unsetEnv(t, "ARCANE_AGENT_MTLS_MODE")

		cfg := mustLoad(t)
		assert.Equal(t, "disabled", cfg.AgentMTLSMode)
	})
}

func TestConfig_AllowDowngrade(t *testing.T) {
	origAllowDowngrade := os.Getenv("ARCANE_ALLOW_DOWNGRADE")
	defer restoreEnv("ARCANE_ALLOW_DOWNGRADE", origAllowDowngrade)

	t.Run("defaults to false", func(t *testing.T) {
		unsetEnv(t, "ARCANE_ALLOW_DOWNGRADE")

		cfg := mustLoad(t)
		assert.False(t, cfg.AllowDowngrade)
	})

	t.Run("loads explicit true from env", func(t *testing.T) {
		setEnv(t, "ARCANE_ALLOW_DOWNGRADE", "true")

		cfg := mustLoad(t)
		assert.True(t, cfg.AllowDowngrade)
	})
}

func TestConfig_ValidateRejectsLegacyDirectAgent(t *testing.T) {
	t.Setenv("ARCANE_AGENT", "")

	for _, name := range []string{"AGENT_MODE", "ARCANE_AGENT_MODE"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, "true")

			require.EqualError(t, mustLoad(t).Validate(), "direct agents were removed in v3; set ARCANE_AGENT=true and ARCANE_MANAGER_API_URL, then re-enroll from the manager")
		})
	}
}

func TestConfig_ValidateRequiresAgentManagerAndToken(t *testing.T) {
	t.Setenv("AGENT_MODE", "")
	t.Setenv("ARCANE_AGENT_MODE", "")
	t.Setenv("ARCANE_AGENT", "true")
	t.Setenv("ARCANE_MANAGER_API_URL", "https://manager.example.com")
	t.Setenv("ARCANE_AGENT_TOKEN", "")

	require.EqualError(t, mustLoad(t).Validate(), "ARCANE_AGENT=true requires ARCANE_MANAGER_API_URL and ARCANE_AGENT_TOKEN")

	t.Setenv("ARCANE_AGENT_TOKEN", "agent-token")
	require.NoError(t, mustLoad(t).Validate())
}

func TestConfig_AllowCLIPasswordReset(t *testing.T) {
	origAllowCLIPasswordReset := os.Getenv("ARCANE_ALLOW_CLI_PASSWORD_RESET")
	defer restoreEnv("ARCANE_ALLOW_CLI_PASSWORD_RESET", origAllowCLIPasswordReset)

	t.Run("defaults to false", func(t *testing.T) {
		unsetEnv(t, "ARCANE_ALLOW_CLI_PASSWORD_RESET")

		cfg := mustLoad(t)
		assert.False(t, cfg.AllowCLIPasswordReset)
	})

	t.Run("loads explicit true from env", func(t *testing.T) {
		setEnv(t, "ARCANE_ALLOW_CLI_PASSWORD_RESET", "TRUE")

		cfg := mustLoad(t)
		assert.True(t, cfg.AllowCLIPasswordReset)
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

func TestConfig_TLSSettings(t *testing.T) {
	origTLSEnabled := os.Getenv("ARCANE_TLS_ENABLED")
	origTLSCertFile := os.Getenv("ARCANE_TLS_CERT_FILE")
	origTLSKeyFile := os.Getenv("ARCANE_TLS_KEY_FILE")
	defer restoreEnv("ARCANE_TLS_ENABLED", origTLSEnabled)
	defer restoreEnv("ARCANE_TLS_CERT_FILE", origTLSCertFile)
	defer restoreEnv("ARCANE_TLS_KEY_FILE", origTLSKeyFile)

	t.Run("defaults to tls disabled", func(t *testing.T) {
		unsetEnv(t, "ARCANE_TLS_ENABLED")
		unsetEnv(t, "ARCANE_TLS_CERT_FILE")
		unsetEnv(t, "ARCANE_TLS_KEY_FILE")

		cfg := mustLoad(t)
		assert.False(t, cfg.TLSEnabled)
		assert.Empty(t, cfg.TLSCertFile)
		assert.Empty(t, cfg.TLSKeyFile)
	})

	t.Run("loads tls settings from env", func(t *testing.T) {
		setEnv(t, "ARCANE_TLS_ENABLED", "true")
		setEnv(t, "ARCANE_TLS_CERT_FILE", "/etc/ssl/certs/fullchain.pem")
		setEnv(t, "ARCANE_TLS_KEY_FILE", "/etc/ssl/private/privkey.pem")

		cfg := mustLoad(t)
		assert.True(t, cfg.TLSEnabled)
		assert.Equal(t, "/etc/ssl/certs/fullchain.pem", cfg.TLSCertFile)
		assert.Equal(t, "/etc/ssl/private/privkey.pem", cfg.TLSKeyFile)
	})
}

func TestConfig_GetManagerGRPCAddr(t *testing.T) {
	t.Run("uses manager api url explicit port when present", func(t *testing.T) {
		cfg := &Config{
			ManagerApiUrl: "https://manager.example.com:8443/api",
		}
		assert.Equal(t, "manager.example.com:8443", httpx.ManagerGRPCAddr(cfg.ManagerApiUrl))
	})

	t.Run("defaults to manager api https port when port is not set", func(t *testing.T) {
		cfg := &Config{
			ManagerApiUrl: "https://manager.example.com/api",
		}
		assert.Equal(t, "manager.example.com:443", httpx.ManagerGRPCAddr(cfg.ManagerApiUrl))
	})

	t.Run("defaults to manager api http port when port is not set", func(t *testing.T) {
		cfg := &Config{
			ManagerApiUrl: "http://manager.example.com/api",
		}
		assert.Equal(t, "manager.example.com:80", httpx.ManagerGRPCAddr(cfg.ManagerApiUrl))
	})

	t.Run("supports reverse-proxy path prefixes", func(t *testing.T) {
		cfg := &Config{
			ManagerApiUrl: "https://manager.example.com/arcane/api/",
		}
		assert.Equal(t, "manager.example.com:443", httpx.ManagerGRPCAddr(cfg.ManagerApiUrl))
	})

	t.Run("supports ipv6 hosts behind reverse proxies", func(t *testing.T) {
		cfg := &Config{
			ManagerApiUrl: "https://[2001:db8::1]/arcane/api",
		}
		assert.Equal(t, "[2001:db8::1]:443", httpx.ManagerGRPCAddr(cfg.ManagerApiUrl))
	})

	t.Run("returns empty for invalid manager url", func(t *testing.T) {
		cfg := &Config{
			ManagerApiUrl: "://bad-url",
		}
		assert.Empty(t, httpx.ManagerGRPCAddr(cfg.ManagerApiUrl))
	})
}

func TestConfig_GetManagerBaseURL(t *testing.T) {
	t.Run("strips trailing slash and api suffix", func(t *testing.T) {
		cfg := &Config{
			ManagerApiUrl: "https://manager.example.com/api/",
		}
		assert.Equal(t, "https://manager.example.com", httpx.ManagerBaseURL(cfg.ManagerApiUrl))
	})

	t.Run("keeps reverse-proxy path prefix", func(t *testing.T) {
		cfg := &Config{
			ManagerApiUrl: "https://manager.example.com/arcane/api/",
		}
		assert.Equal(t, "https://manager.example.com/arcane", httpx.ManagerBaseURL(cfg.ManagerApiUrl))
	})
}

func TestConfig_JWTRefreshExpiry(t *testing.T) {
	origJWTRefreshExpiry := os.Getenv("ARCANE_JWT_REFRESH_EXPIRY")
	defer restoreEnv("ARCANE_JWT_REFRESH_EXPIRY", origJWTRefreshExpiry)

	t.Run("defaults to 7 days when not set", func(t *testing.T) {
		unsetEnv(t, "ARCANE_JWT_REFRESH_EXPIRY")

		cfg := mustLoad(t)
		assert.Equal(t, 7*24*time.Hour, cfg.JWTRefreshExpiry)
	})

	t.Run("parses custom duration from env var", func(t *testing.T) {
		setEnv(t, "ARCANE_JWT_REFRESH_EXPIRY", "48h")

		cfg := mustLoad(t)
		assert.Equal(t, 48*time.Hour, cfg.JWTRefreshExpiry)
	})

	t.Run("supports compound duration format", func(t *testing.T) {
		setEnv(t, "ARCANE_JWT_REFRESH_EXPIRY", "24h30m")

		cfg := mustLoad(t)
		assert.Equal(t, 24*time.Hour+30*time.Minute, cfg.JWTRefreshExpiry)
	})

	for _, value := range []string{"notaduration", "0s", "-1h"} {
		t.Run("rejects "+value, func(t *testing.T) {
			setEnv(t, "ARCANE_JWT_REFRESH_EXPIRY", value)

			_, err := Load()
			require.ErrorContains(t, err, "ARCANE_JWT_REFRESH_EXPIRY")
		})
	}
}

func TestConfig_LoadJoinsParseErrors(t *testing.T) {
	t.Setenv("ARCANE_TLS_ENABLED", "maybe")
	t.Setenv("ARCANE_AGENT_RECONNECT_INTERVAL", "soon")
	t.Setenv("ARCANE_FILE_PERM", "rw-r--r--")
	t.Setenv("ARCANE_JWT_REFRESH_EXPIRY", "forever")

	cfg, err := Load()
	require.Error(t, err)
	assert.Nil(t, cfg)
	for _, envName := range []string{"ARCANE_TLS_ENABLED", "ARCANE_AGENT_RECONNECT_INTERVAL", "ARCANE_FILE_PERM", "ARCANE_JWT_REFRESH_EXPIRY"} {
		assert.ErrorContains(t, err, envName)
	}
}

func TestConfig_LoadRejectsWeakProductionEncryptionKey(t *testing.T) {
	t.Setenv("ARCANE_ENVIRONMENT", "production")
	t.Setenv("ARCANE_ENCRYPTION_KEY", "short")

	_, err := Load()
	require.ErrorContains(t, err, "ARCANE_ENCRYPTION_KEY")

	t.Setenv("ARCANE_ENCRYPTION_KEY", "hex:abc")
	_, err = Load()
	require.NoError(t, err)

	t.Setenv("ARCANE_ENCRYPTION_KEY", "short")
	t.Setenv("ARCANE_ENVIRONMENT", "development")
	_, err = Load()
	require.NoError(t, err)
}

func TestConfig_BasePath(t *testing.T) {
	tests := []struct {
		name    string
		appURL  string
		agent   bool
		want    string
		wantErr bool
	}{
		{name: "default", appURL: "", want: ""},
		{name: "host only", appURL: "https://example.com", want: ""},
		{name: "root slash", appURL: "https://example.com/", want: ""},
		{name: "path", appURL: "https://example.com/arcane", want: "/arcane"},
		{name: "trailing slash", appURL: "https://example.com/arcane/", want: "/arcane"},
		{name: "nested path", appURL: "https://example.com/tools/arcane", want: "/tools/arcane"},
		{name: "no scheme", appURL: "example.com/arcane", want: "/arcane"},
		{name: "agent ignores path", appURL: "https://manager.example.com/arcane", agent: true, want: ""},
		{name: "unsafe characters", appURL: "https://example.com/a%22b", wantErr: true},
		{name: "collides with the API", appURL: "https://example.com/api", wantErr: true},
		{name: "collides with the assets", appURL: "https://example.com/_app/x", wantErr: true},
		{name: "api only as a later segment", appURL: "https://example.com/tools/api", want: "/tools/api"},
		{name: "dot-dot segment", appURL: "https://example.com/tools/../arcane", wantErr: true},
		{name: "dot segment", appURL: "https://example.com/./arcane", wantErr: true},
		{name: "dots inside a segment", appURL: "https://example.com/arcane..v2", want: "/arcane..v2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := (&Config{AppUrl: tt.appURL, AgentEnabled: tt.agent}).BasePath()
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestConfig_ApplyProxyDefaults(t *testing.T) {
	t.Run("loopback ipv4 auto-trusts", func(t *testing.T) {
		cfg := &Config{Listen: "127.0.0.1"}
		applyProxyDefaults(cfg)
		assert.Equal(t, "127.0.0.0/8,::1/128", cfg.TrustedProxies)
	})

	t.Run("loopback ipv6 auto-trusts", func(t *testing.T) {
		cfg := &Config{Listen: "::1"}
		applyProxyDefaults(cfg)
		assert.Equal(t, "127.0.0.0/8,::1/128", cfg.TrustedProxies)
	})

	t.Run("explicit value preserved", func(t *testing.T) {
		cfg := &Config{Listen: "127.0.0.1", TrustedProxies: "10.0.0.0/8"}
		applyProxyDefaults(cfg)
		assert.Equal(t, "10.0.0.0/8", cfg.TrustedProxies)
	})

	t.Run("empty listen is no-op", func(t *testing.T) {
		cfg := &Config{Listen: ""}
		applyProxyDefaults(cfg)
		assert.Empty(t, cfg.TrustedProxies)
	})

	t.Run("public bind is no-op", func(t *testing.T) {
		cfg := &Config{Listen: "0.0.0.0"}
		applyProxyDefaults(cfg)
		assert.Empty(t, cfg.TrustedProxies)
	})
}

func TestConfig_TrustedProxies(t *testing.T) {
	orig := os.Getenv("ARCANE_TRUSTED_PROXIES")
	defer restoreEnv("ARCANE_TRUSTED_PROXIES", orig)

	t.Run("Default empty", func(t *testing.T) {
		unsetEnv(t, "ARCANE_TRUSTED_PROXIES")
		cfg := mustLoad(t)
		assert.Empty(t, cfg.TrustedProxies)
	})

	t.Run("Single CIDR", func(t *testing.T) {
		setEnv(t, "ARCANE_TRUSTED_PROXIES", "10.0.0.0/8")
		cfg := mustLoad(t)
		assert.Equal(t, "10.0.0.0/8", cfg.TrustedProxies)
	})

	t.Run("Multiple CIDRs", func(t *testing.T) {
		setEnv(t, "ARCANE_TRUSTED_PROXIES", "10.0.0.0/8,192.168.0.0/16,172.16.0.0/12")
		cfg := mustLoad(t)
		assert.Equal(t, "10.0.0.0/8,192.168.0.0/16,172.16.0.0/12", cfg.TrustedProxies)
	})
}

func mustLoad(t *testing.T) *Config {
	t.Helper()
	cfg, err := Load()
	require.NoError(t, err)
	return cfg
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
