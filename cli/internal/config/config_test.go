package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v4"

	"github.com/getarcaneapp/arcane/cli/v2/internal/types"
)

func setTempConfigPath(t *testing.T) string {
	t.Helper()

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
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

	require.False(t, cfg.Previews.AllEnvironments)
	require.False(t, cfg.Previews.NewSelfUpdater)
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
	for _, filename := range []string{"arcanecli.yml", "custom.json", "config"} {
		t.Run(filename, func(t *testing.T) {
			path := filepath.Join(filepath.Dir(setTempConfigPath(t)), "nested", filename)
			require.NoError(t, SetConfigPath(path))

			cfg := DefaultConfig()
			cfg.Previews.AllEnvironments = true
			cfg.Previews.NewSelfUpdater = true
			cfg.APIKey = "k_test"
			cfg.FederatedAudience = "audience_test"
			cfg.CLIUpdateChannel = "next"
			cfg.SetDefaultLimit(42)
			cfg.SetResourceLimit("images", 17)
			cfg.SetResourceLimit("Containers", 9)
			cfg.Pagination.Resources["GitOps"] = types.PaginationResourceConfig{Limit: 8}
			cfg.Pagination.Resources[" "] = types.PaginationResourceConfig{Limit: 5}
			cfg.Pagination.Resources["volumes"] = types.PaginationResourceConfig{Limit: -1}
			{

				err := Save(t.Context(), cfg)
				require.NoError(t, err,
					"Save() failed: %v", err)
			}

			raw, err := os.ReadFile(path)

			require.NoError(t, err,
				"failed to read saved config: %v", err)

			text := string(raw)
			require.NotContains(t, text, "volumes:")
			require.Contains(t, text, "gitops-syncs:")
			require.NotContains(t, text, "GitOps:")

			require.Contains(t, text, "previews:\n    allEnvironments: true")
			require.Contains(t, text, "newSelfUpdater: true")
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

			dirInfo, dirErr := os.Stat(filepath.Dir(path))
			require.NoError(t, dirErr)
			require.Equal(t, os.FileMode(0o700), dirInfo.Mode().Perm())
			cfg.APIKey = "changed after save"
			cfg.SetResourceLimit("images", 99)
			cached, cacheErr := Load()
			require.NoError(t, cacheErr)
			require.True(t, cached.Previews.AllEnvironments)
			require.True(t, cached.Previews.NewSelfUpdater)
			require.Equal(t, "k_test", cached.APIKey)
			require.Equal(t, 17, cached.LimitFor("images"))
			require.Equal(t, 8, cached.LimitFor("gitops-syncs"))
			require.NotContains(t, cached.Pagination.Resources, "GitOps")
			require.NotContains(t, cached.Pagination.Resources, "volumes")
			invalidateCache()
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
			require.True(t, loaded.Previews.AllEnvironments)
			require.True(t, loaded.Previews.NewSelfUpdater)
			require.Equal(t, "k_test", loaded.APIKey)
			require.Equal(t, "audience_test", loaded.FederatedAudience)
			require.Equal(t, 8, loaded.LimitFor("gitops-syncs"))
			loaded.SetResourceLimit("images", 101)
			reloaded, reloadErr := Load()
			require.NoError(t, reloadErr)
			require.Equal(t, 17, reloaded.LimitFor("images"))

			require.NoError(t, os.Chmod(path, 0o644))
			require.NoError(t, Save(t.Context(), &types.Config{
				ServerURL:  "https://short.test",
				Pagination: types.PaginationConfig{Default: types.PaginationResourceConfig{Limit: -1}},
			}))
			omittedRaw, omittedErr := os.ReadFile(path)
			require.NoError(t, omittedErr)
			require.Equal(t, "server_url: https://short.test\npreviews:\n    allEnvironments: false\n    newSelfUpdater: false\n", string(omittedRaw))
			securedInfo, securedErr := os.Stat(path)
			require.NoError(t, securedErr)
			require.Equal(t, os.FileMode(0o600), securedInfo.Mode().Perm())
		})
	}
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

	for _, tc := range []struct {
		name          string
		content       string
		serverURL     string
		environment   string
		logLevel      string
		limit         int
		resourceLimit int
		errorText     string
	}{
		{name: "partial defaults", content: "api_key: partial\n", serverURL: "http://localhost:3552", environment: "0", logLevel: "info"},
		{name: "empty document", content: "", serverURL: "http://localhost:3552", environment: "0", logLevel: "info"},
		{name: "null document", content: "null\n", serverURL: "http://localhost:3552", environment: "0", logLevel: "info"},
		{name: "dotted keys are unknown", content: "pagination.default.limit: 13\n", serverURL: "http://localhost:3552", environment: "0", logLevel: "info"},
		{name: "keys are case sensitive", content: `SERVER_URL: https://mixed.test
Default_Environment: 2
LOG_LEVEL: debug
PAGINATION:
  DEFAULT:
    LIMIT: 13
  RESOURCES:
    NETWORKS:
      LIMIT: 4
`, serverURL: "http://localhost:3552", environment: "0", logLevel: "info"},
		{name: "numeric environment and unknown keys", content: `default_environment: 2
pagination:
  default:
    limit: 13
    unknown_key: ignored
  resources:
    networks:
      limit: 4
unknown_key: ignored
`, serverURL: "http://localhost:3552", environment: "2", logLevel: "info", limit: 13, resourceLimit: 4},
		{name: "explicit empty and zero", content: `server_url: ""
default_environment: ""
log_level: ""
pagination:
  default:
    limit: 0
  resources:
    networks:
      limit: 0
`},
		{name: "null defaults", content: `server_url: null
default_environment: null
log_level: null
pagination:
  default:
    limit: null
  resources:
    networks:
      limit: null
`, serverURL: "http://localhost:3552", environment: "0", logLevel: "info"},
		{name: "malformed yaml", content: "pagination: [\n", errorText: "failed to parse config file"},
		{name: "invalid limit", content: "pagination:\n  default:\n    limit: invalid\n", errorText: "failed to parse config file"},
		{name: "quoted limit", content: "pagination:\n  default:\n    limit: \"13\"\n", errorText: "failed to parse config file"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixturePath := setTempConfigPath(t)
			require.NoError(t, os.WriteFile(fixturePath, []byte(tc.content), 0o600))
			loaded, loadErr := Load()
			if tc.errorText != "" {
				require.ErrorContains(t, loadErr, tc.errorText)
				require.Nil(t, loaded)
				require.NoError(t, os.WriteFile(fixturePath, []byte("server_url: https://recovered.test\n"), 0o600))
				recovered, recoveryErr := Load()
				require.NoError(t, recoveryErr)
				require.Equal(t, "https://recovered.test", recovered.ServerURL)
				return
			}
			require.NoError(t, loadErr)
			require.Equal(t, tc.serverURL, loaded.ServerURL)
			require.Equal(t, tc.environment, loaded.DefaultEnvironment)
			require.Equal(t, tc.logLevel, loaded.LogLevel)
			require.Equal(t, tc.limit, loaded.Pagination.Default.Limit)
			require.Equal(t, tc.resourceLimit, loaded.LimitFor("networks"))
			require.NotNil(t, loaded.Pagination.Resources)
		})
	}
}

func TestInitDefaultFileCreatesTemplate(t *testing.T) {
	path := setTempConfigPath(t)
	path = filepath.Join(filepath.Dir(path), "template.json")
	require.NoError(t, SetConfigPath(path))
	_, initialErr := Load()
	require.NoError(t, initialErr)

	created, err := InitDefaultFile()

	require.NoError(t, err,
		"InitDefaultFile() failed: %v", err)

	require.True(t, created,
		"InitDefaultFile() created = false, want true")

	raw, err := os.ReadFile(path)

	require.NoError(t, err,
		"failed to read config file: %v", err)

	text := string(raw)
	require.Contains(t, text, "previews:\n    allEnvironments: false")
	var values map[string]any
	require.NoError(t, yaml.Unmarshal(raw, &values))
	for _, key := range []string{"api_key", "federated_audience"} {
		require.Contains(t, values, key)
		require.Empty(t, values[key])
	}
	info, statErr := os.Stat(path)
	require.NoError(t, statErr)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	entries, readDirErr := os.ReadDir(filepath.Dir(path))
	require.NoError(t, readDirErr)
	require.Len(t, entries, 1)
	require.Equal(t, filepath.Base(path), entries[0].Name())

	requiredKeys := []string{
		"server_url:",
		"api_key:",
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
	// The writer must also refuse an existing file after the initial path check.
	require.ErrorIs(t, writeConfig(path, DefaultConfig(), true), os.ErrExist)
	entries, readDirErr := os.ReadDir(filepath.Dir(path))
	require.NoError(t, readDirErr)
	require.Len(t, entries, 1)

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

		_, statErr := os.Stat(path)
		require.True(t, os.IsNotExist(statErr),
			"expected original config to be removed, stat err=%v", statErr)
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

func TestAuthTokens(t *testing.T) {
	path := setTempConfigPath(t)
	require.NoError(t, os.WriteFile(path, []byte("server_url: https://a.test\n"), 0o600))

	session, err := Load()
	require.NoError(t, err)
	session.JWTToken, session.RefreshToken = "a_jwt", "a_refresh"
	require.NoError(t, SaveTokens(t.Context(), session))

	session.DefaultEnvironment = "1"
	require.NoError(t, Save(t.Context(), session))
	loadedSession, err := Load()
	require.NoError(t, err)
	require.Equal(t, "a_jwt", loadedSession.JWTToken)
	require.Equal(t, "a_refresh", loadedSession.RefreshToken)

	require.NoError(t, SwitchServer(t.Context(), session, "https://b.test/"))
	require.Empty(t, session.JWTToken)
	require.NoError(t, Save(t.Context(), session))
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "a_jwt")

	authPath, err := AuthPath()
	require.NoError(t, err)
	info, err := os.Stat(authPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	session.JWTToken, session.RefreshToken = "b_jwt", "b_refresh"
	require.NoError(t, SaveTokens(t.Context(), session))
	require.NoError(t, SwitchServer(t.Context(), session, "https://a.test"))
	require.Equal(t, "a_jwt", session.JWTToken)
	require.Equal(t, "a_refresh", session.RefreshToken)

	apiKeyConfig := &types.Config{ServerURL: "https://a.test", APIKey: "arc_key"}
	require.NoError(t, Save(t.Context(), apiKeyConfig))
	invalidateCache()
	loaded, err := Load()
	require.NoError(t, err)
	require.Equal(t, "arc_key", loaded.APIKey)
	require.Empty(t, loaded.JWTToken)
	auth, err := loadAuth()
	require.NoError(t, err)
	require.Len(t, auth, 2)

	stale := session.Clone()
	refreshed := session.Clone()
	refreshed.JWTToken, refreshed.RefreshToken = "refreshed_jwt", "refreshed_refresh"
	require.NoError(t, SaveTokens(t.Context(), refreshed))
	waitingPath := path + ".waiting"
	require.NoError(t, SetConfigPath(waitingPath))
	stale.DefaultEnvironment = "2"
	require.NoError(t, Save(t.Context(), stale))
	auth, err = loadAuth()
	require.NoError(t, err)
	require.Equal(t, "refreshed_jwt", auth["https://a.test"].AccessToken)
	require.Equal(t, "refreshed_refresh", auth["https://a.test"].RefreshToken)
	loaded, err = Load()
	require.NoError(t, err)
	require.Equal(t, "refreshed_jwt", loaded.JWTToken)
	require.Equal(t, "2", loaded.DefaultEnvironment)

	require.NoError(t, SetConfigPath(path))
	require.NoError(t, ClearTokens(t.Context(), apiKeyConfig))
	require.Equal(t, "arc_key", apiKeyConfig.APIKey)
	require.NoError(t, SetConfigPath(waitingPath))
	require.NoError(t, Save(t.Context(), stale))
	loaded, err = Load()
	require.NoError(t, err)
	require.Empty(t, loaded.JWTToken)
	require.Empty(t, loaded.RefreshToken)
	auth, err = loadAuth()
	require.NoError(t, err)
	require.NotContains(t, auth, "https://a.test")
	require.Equal(t, "b_jwt", auth["https://b.test"].AccessToken)
	require.NoError(t, os.WriteFile(path+".session", []byte("server_url: https://a.test\n"), 0o600))
	require.NoError(t, SetConfigPath(path+".session"))
	loaded, err = Load()
	require.NoError(t, err)
	require.Empty(t, loaded.JWTToken)
	require.Empty(t, loaded.RefreshToken)
	require.NoError(t, SetConfigPath(path))

	for _, serverURL := range []string{"https://a.test", "https://b.test"} {
		require.NoError(t, ClearTokens(t.Context(), &types.Config{ServerURL: serverURL}))
	}
	_, statErr := os.Stat(authPath)
	require.ErrorIs(t, statErr, os.ErrNotExist)

	require.NoError(t, os.MkdirAll(filepath.Dir(authPath), 0o700))
	require.NoError(t, os.WriteFile(authPath, []byte("null"), 0o600))
	require.NoError(t, SaveTokens(t.Context(), &types.Config{ServerURL: "https://a.test", JWTToken: "jwt"}))

	require.NoError(t, os.WriteFile(authPath, []byte("{broken"), 0o600))
	require.NoError(t, Save(t.Context(), stale))
	require.ErrorContains(t, SaveTokens(t.Context(), &types.Config{ServerURL: "https://a.test", JWTToken: "jwt"}), "config backup --auth")
	backupPath, moved, err := BackupAuthFile(t.Context())
	require.NoError(t, err)
	require.True(t, moved)
	require.FileExists(t, backupPath)
	require.NoError(t, SaveTokens(t.Context(), &types.Config{ServerURL: "https://a.test", JWTToken: "jwt"}))

	before, err := os.ReadFile(authPath)
	require.NoError(t, err)
	unlock, err := lockAuth(t.Context())
	require.NoError(t, err)
	defer func() { assert.NoError(t, unlock()) }()
	require.NoError(t, Save(t.Context(), stale))
	require.NoError(t, SwitchServer(t.Context(), stale, "https://b.test"))
	for _, operation := range []struct {
		name string
		run  func(context.Context) error
	}{
		{name: "save", run: func(ctx context.Context) error {
			return SaveTokens(ctx, &types.Config{ServerURL: "https://a.test", JWTToken: "changed"})
		}},
		{name: "backup", run: func(ctx context.Context) error {
			_, _, backupErr := BackupAuthFile(ctx)
			return backupErr
		}},
		{name: "clear tokens", run: func(ctx context.Context) error {
			return ClearTokens(ctx, &types.Config{ServerURL: "https://a.test", APIKey: "arc_key"})
		}},
	} {
		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		operationErr := operation.run(ctx)
		cancel()
		require.ErrorIs(t, operationErr, context.DeadlineExceeded, operation.name)
	}
	after, err := os.ReadFile(authPath)
	require.NoError(t, err)
	require.Equal(t, before, after)
}
