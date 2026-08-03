package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setTempConfigPath(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "arcanecli.yml")
	{
		err := SetConfigPath(path)
		require.NoError(t, err,
			"SetConfigPath() failed: %v", err)
	}

	t.Cleanup(func() {
		{
			err := SetConfigPath("")
			assert.NoError(t, err,
				"SetConfigPath(reset) failed: %v", err)
		}

	})
	return path
}

func TestLoadReturnsDefaultsWhenFileMissing(t *testing.T) {
	path := setTempConfigPath(t)
	{
		_, err := os.Stat(path)
		require.True(t, os.IsNotExist(err),
			"expected config file to be missing, got err=%v", err)
	}

	cfg, err := Load()

	require.NoError(t, err,
		"Load() failed: %v", err)

	require.Equal(t, "http://localhost:3552", cfg.ServerURL,
		"ServerURL=%q, want %q", cfg.ServerURL, "http://localhost:3552")

	require.Equal(t, "0", cfg.DefaultEnvironment,
		"DefaultEnvironment=%q, want %q", cfg.DefaultEnvironment, "0")

	require.Equal(t, "info", cfg.LogLevel,
		"LogLevel=%q, want %q", cfg.LogLevel, "info")

	// Ensure callers cannot mutate cached config state.
	cfg.ServerURL = "https://mutated.invalid"
	cfg2, err := Load()

	require.NoError(t, err,
		"Load() second call failed: %v", err)

	require.Equal(t, "http://localhost:3552", cfg2.ServerURL,
		"cached config was mutated, ServerURL=%q", cfg2.ServerURL)

}

func TestSaveAndLoadRoundTripPagination(t *testing.T) {
	path := setTempConfigPath(t)

	cfg := DefaultConfig()
	cfg.APIKey = "k_test"
	cfg.CLIUpdateChannel = "next"
	cfg.SetDefaultLimit(42)
	cfg.SetResourceLimit("images", 17)
	cfg.SetResourceLimit("Containers", 9)
	{

		err := Save(cfg)
		require.NoError(t, err,
			"Save() failed: %v", err)
	}

	raw, err := os.ReadFile(path)

	require.NoError(t, err,
		"failed to read saved config: %v", err)

	text := string(raw)

	require.Contains(t, text, "pagination:",
		"expected saved YAML to include pagination block:\n%s", text)

	require.Contains(t, text, "cli_update_channel: next",
		"expected saved YAML to include cli_update_channel key:\n%s", text)

	info, err := os.Stat(path)

	require.NoError(t, err,
		"failed to stat config file: %v", err)
	{

		got := info.Mode().Perm()
		require.Equal(t, os.FileMode(0o600), got,
			"config file permissions=%#o, want %#o", got, 0o600)
	}

	loaded, err := Load()

	require.NoError(t, err,
		"Load() failed: %v", err)

	require.Equal(t, 42, loaded.Pagination.Default.Limit,
		"default limit mismatch: pagination=%d, want 42", loaded.Pagination.Default.Limit)
	{

		got := loaded.LimitFor("images")
		require.Equal(t, 17, got,
			"images limit=%d, want 17", got)
	}
	{

		got := loaded.LimitFor("containers")
		require.Equal(t, 9, got,
			"containers limit=%d, want 9", got)
	}

	require.Equal(t, "next", loaded.CLIUpdateChannel,
		"CLIUpdateChannel=%q, want next", loaded.CLIUpdateChannel)

}

func TestLoadCanonicalPaginationBlock(t *testing.T) {
	path := setTempConfigPath(t)
	content := `
server_url: https://api.arcane.test
api_key: k_123
default_environment: "2"
log_level: debug
pagination:
  default:
    limit: 13
  resources:
    networks:
      limit: 4
    registries:
      limit: 9
`
	{
		err := os.WriteFile(path, []byte(content), 0o600)
		require.NoError(t, err,
			"failed to write config fixture: %v", err)
	}

	cfg, err := Load()

	require.NoError(t, err,
		"Load() failed: %v", err)

	require.Equal(t, "https://api.arcane.test", cfg.ServerURL,
		"ServerURL=%q, want %q", cfg.ServerURL, "https://api.arcane.test")

	require.Equal(t, "k_123", cfg.APIKey,
		"APIKey=%q, want %q", cfg.APIKey, "k_123")

	require.Equal(t, 13, cfg.Pagination.Default.Limit,
		"Pagination.Default.Limit=%d, want 13", cfg.Pagination.Default.Limit)
	{

		got := cfg.LimitFor("networks")
		require.Equal(t, 4, got,
			"networks limit=%d, want 4", got)
	}
	{

		got := cfg.LimitFor("registries")
		require.Equal(t, 9, got,
			"registries limit=%d, want 9", got)
	}

}

func TestInitDefaultFileCreatesTemplate(t *testing.T) {
	path := setTempConfigPath(t)

	created, err := InitDefaultFile()

	require.NoError(t, err,
		"InitDefaultFile() failed: %v", err)

	require.True(t, created,
		"InitDefaultFile() created = false, want true")

	raw, err := os.ReadFile(path)

	require.NoError(t, err,
		"failed to read config file: %v", err)

	text := string(raw)

	requiredKeys := []string{
		"server_url:",
		"api_key:",
		"jwt_token:",
		"refresh_token:",
		"default_environment:",
		"log_level:",
		"pagination:",
	}
	for _, key := range requiredKeys {

		require.Contains(t, text, key,
			"expected generated config to contain %q:\n%s", key, text)

	}
	for _, resource := range []string{"containers", "images", "volumes", "networks", "projects", "environments", "registries", "templates", "users", "events", "apikeys"} {

		require.Contains(t, text, resource+":",
			"expected generated config to contain resource key %q:\n%s", resource, text)

	}

	cfg, err := Load()

	require.NoError(t, err,
		"Load() after init failed: %v", err)

	require.Equal(t, defaultPaginationInitLimit, cfg.Pagination.Default.Limit,
		"Pagination.Default.Limit=%d, want %d", cfg.Pagination.Default.Limit, defaultPaginationInitLimit)

	for _, resource := range []string{"containers", "images", "volumes", "networks", "projects", "environments", "registries", "templates", "users", "events", "apikeys"} {
		{
			got := cfg.LimitFor(resource)
			require.Equal(t, defaultPaginationInitLimit, got,
				"LimitFor(%s)=%d, want %d", resource, got, defaultPaginationInitLimit)
		}

	}
}

func TestInitDefaultFileDoesNotOverwriteExistingFile(t *testing.T) {
	path := setTempConfigPath(t)
	original := "server_url: https://custom.arcane.example\napi_key: custom\n"
	{
		err := os.WriteFile(path, []byte(original), 0o600)
		require.NoError(t, err,
			"failed to write fixture file: %v", err)
	}

	created, err := InitDefaultFile()

	require.NoError(t, err,
		"InitDefaultFile() failed: %v", err)

	require.False(t, created,
		"InitDefaultFile() created = true, want false")

	raw, err := os.ReadFile(path)

	require.NoError(t, err,
		"failed to read config file: %v", err)

	require.Equal(t, original, string(raw),
		"existing file was modified:\nwant:\n%s\ngot:\n%s", original, string(raw))

}

func TestBackupFileMovesConfig(t *testing.T) {
	path := setTempConfigPath(t)
	original := "server_url: https://backup.arcane.example\napi_key: abc123\n"
	{
		err := os.WriteFile(path, []byte(original), 0o600)
		require.NoError(t, err,
			"failed to write config fixture: %v", err)
	}

	backupPath, moved, err := BackupFile()

	require.NoError(t, err,
		"BackupFile() failed: %v", err)

	require.True(t, moved,
		"BackupFile() moved = false, want true")

	require.Equal(t, path+".bak", backupPath,
		"backup path = %q, want %q", backupPath, path+".bak")
	{

		_, err := os.Stat(path)
		require.True(t, os.IsNotExist(err),
			"expected original config to be removed, stat err=%v", err)
	}

	raw, err := os.ReadFile(backupPath)

	require.NoError(t, err,
		"failed to read backup file: %v", err)

	require.Equal(t, original, string(raw),
		"backup content mismatch:\nwant:\n%s\ngot:\n%s", original, string(raw))

}

func TestBackupFileNoConfig(t *testing.T) {
	path := setTempConfigPath(t)
	{
		_, err := os.Stat(path)
		require.True(t, os.IsNotExist(err),
			"expected missing config before backup, stat err=%v", err)
	}

	backupPath, moved, err := BackupFile()

	require.NoError(t, err,
		"BackupFile() failed: %v", err)

	require.False(t, moved,
		"BackupFile() moved = true, want false")

	require.Equal(t, path+".bak", backupPath,
		"backup path = %q, want %q", backupPath, path+".bak")

}

func TestBackupFileRotatesExistingBak(t *testing.T) {
	path := setTempConfigPath(t)
	backupPath := path + ".bak"
	{

		err := os.WriteFile(path, []byte("server_url: https://new.example\n"), 0o600)
		require.NoError(t, err,
			"failed to write primary config: %v", err)
	}
	{

		err := os.WriteFile(backupPath, []byte("server_url: https://old.example\n"), 0o600)
		require.NoError(t, err,
			"failed to write existing backup: %v", err)
	}

	newBackupPath, moved, err := BackupFile()

	require.NoError(t, err,
		"BackupFile() failed: %v", err)

	require.True(t, moved,
		"BackupFile() moved = false, want true")

	require.Equal(t, backupPath, newBackupPath,
		"backup path = %q, want %q", newBackupPath, backupPath)

	raw, err := os.ReadFile(backupPath)

	require.NoError(t, err,
		"failed to read newest backup: %v", err)

	require.Contains(t, string(raw), "new.example",
		"expected newest backup to contain new config, got:\n%s", string(raw))

	rotated, err := filepath.Glob(backupPath + ".*")

	require.NoError(t, err,
		"failed to glob rotated backups: %v", err)

	require.NotEmpty(t, rotated,
		"expected rotated backup matching %q", backupPath+".*")

}
