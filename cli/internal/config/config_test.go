package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setTempConfigPath(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "arcanecli.yml")
	if err := SetConfigPath(path); err != nil {
		t.Fatalf("SetConfigPath() failed: %v", err)
	}
	t.Cleanup(func() {
		if err := SetConfigPath(""); err != nil {
			t.Errorf("SetConfigPath(reset) failed: %v", err)
		}
	})
	return path
}

func TestLoadReturnsDefaultsWhenFileMissing(t *testing.T) {
	path := setTempConfigPath(t)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected config file to be missing, got err=%v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if cfg.ServerURL != "http://localhost:3552" {
		t.Fatalf("ServerURL=%q, want %q", cfg.ServerURL, "http://localhost:3552")
	}
	if cfg.DefaultEnvironment != "0" {
		t.Fatalf("DefaultEnvironment=%q, want %q", cfg.DefaultEnvironment, "0")
	}
	if cfg.LogLevel != "info" {
		t.Fatalf("LogLevel=%q, want %q", cfg.LogLevel, "info")
	}

	// Ensure callers cannot mutate cached config state.
	cfg.ServerURL = "https://mutated.invalid"
	cfg2, err := Load()
	if err != nil {
		t.Fatalf("Load() second call failed: %v", err)
	}
	if cfg2.ServerURL != "http://localhost:3552" {
		t.Fatalf("cached config was mutated, ServerURL=%q", cfg2.ServerURL)
	}
}

func TestSaveAndLoadRoundTripPaginationCompatibility(t *testing.T) {
	path := setTempConfigPath(t)

	cfg := DefaultConfig()
	cfg.APIKey = "k_test"
	cfg.SetDefaultLimit(42)
	cfg.SetResourceLimit("images", 17)
	cfg.SetResourceLimit("Containers", 9)

	if err := Save(cfg); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read saved config: %v", err)
	}
	text := string(raw)
	if !strings.Contains(text, "pagination:") {
		t.Fatalf("expected saved YAML to include pagination block:\n%s", text)
	}
	if !strings.Contains(text, "default_limit: 42") {
		t.Fatalf("expected saved YAML to include legacy default_limit key:\n%s", text)
	}
	if !strings.Contains(text, "resource_limits:") {
		t.Fatalf("expected saved YAML to include legacy resource_limits key:\n%s", text)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("failed to stat config file: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("config file permissions=%#o, want %#o", got, 0o600)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if loaded.Pagination.Default.Limit != 42 || loaded.DefaultLimit != 42 {
		t.Fatalf("default limit mismatch: pagination=%d legacy=%d", loaded.Pagination.Default.Limit, loaded.DefaultLimit)
	}
	if got := loaded.LimitFor("images"); got != 17 {
		t.Fatalf("images limit=%d, want 17", got)
	}
	if got := loaded.LimitFor("containers"); got != 9 {
		t.Fatalf("containers limit=%d, want 9", got)
	}
}

func TestLoadLegacyPaginationKeys(t *testing.T) {
	path := setTempConfigPath(t)
	content := `
server_url: https://arcane.example
default_environment: "7"
log_level: warn
default_limit: 25
resource_limits:
  image: 31
  Volumes: 5
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write config fixture: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if cfg.ServerURL != "https://arcane.example" {
		t.Fatalf("ServerURL=%q, want %q", cfg.ServerURL, "https://arcane.example")
	}
	if cfg.Pagination.Default.Limit != 25 || cfg.DefaultLimit != 25 {
		t.Fatalf("default limit mismatch: pagination=%d legacy=%d", cfg.Pagination.Default.Limit, cfg.DefaultLimit)
	}
	if got := cfg.LimitFor("images"); got != 31 {
		t.Fatalf("images limit=%d, want 31", got)
	}
	if got := cfg.LimitFor("volumes"); got != 5 {
		t.Fatalf("volumes limit=%d, want 5", got)
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
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write config fixture: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if cfg.ServerURL != "https://api.arcane.test" {
		t.Fatalf("ServerURL=%q, want %q", cfg.ServerURL, "https://api.arcane.test")
	}
	if cfg.APIKey != "k_123" {
		t.Fatalf("APIKey=%q, want %q", cfg.APIKey, "k_123")
	}
	if cfg.Pagination.Default.Limit != 13 {
		t.Fatalf("Pagination.Default.Limit=%d, want 13", cfg.Pagination.Default.Limit)
	}
	if got := cfg.LimitFor("networks"); got != 4 {
		t.Fatalf("networks limit=%d, want 4", got)
	}
	if got := cfg.LimitFor("registries"); got != 9 {
		t.Fatalf("registries limit=%d, want 9", got)
	}
}

func TestInitDefaultFileCreatesTemplate(t *testing.T) {
	path := setTempConfigPath(t)

	created, err := InitDefaultFile()
	if err != nil {
		t.Fatalf("InitDefaultFile() failed: %v", err)
	}
	if !created {
		t.Fatal("InitDefaultFile() created = false, want true")
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read config file: %v", err)
	}
	text := string(raw)

	requiredKeys := []string{
		"server_url:",
		"api_key:",
		"jwt_token:",
		"refresh_token:",
		"default_environment:",
		"log_level:",
		"pagination:",
		"default_limit:",
		"resource_limits:",
	}
	for _, key := range requiredKeys {
		if !strings.Contains(text, key) {
			t.Fatalf("expected generated config to contain %q:\n%s", key, text)
		}
	}
	for _, resource := range []string{"containers", "images", "volumes", "networks", "projects", "environments", "registries", "templates", "users", "events", "apikeys"} {
		if !strings.Contains(text, resource+":") {
			t.Fatalf("expected generated config to contain resource key %q:\n%s", resource, text)
		}
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() after init failed: %v", err)
	}
	if cfg.Pagination.Default.Limit != defaultPaginationInitLimit {
		t.Fatalf("Pagination.Default.Limit=%d, want %d", cfg.Pagination.Default.Limit, defaultPaginationInitLimit)
	}
	if cfg.DefaultLimit != defaultPaginationInitLimit {
		t.Fatalf("DefaultLimit=%d, want %d", cfg.DefaultLimit, defaultPaginationInitLimit)
	}
	for _, resource := range []string{"containers", "images", "volumes", "networks", "projects", "environments", "registries", "templates", "users", "events", "apikeys"} {
		if got := cfg.LimitFor(resource); got != defaultPaginationInitLimit {
			t.Fatalf("LimitFor(%s)=%d, want %d", resource, got, defaultPaginationInitLimit)
		}
	}
}

func TestInitDefaultFileDoesNotOverwriteExistingFile(t *testing.T) {
	path := setTempConfigPath(t)
	original := "server_url: https://custom.arcane.example\napi_key: custom\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatalf("failed to write fixture file: %v", err)
	}

	created, err := InitDefaultFile()
	if err != nil {
		t.Fatalf("InitDefaultFile() failed: %v", err)
	}
	if created {
		t.Fatal("InitDefaultFile() created = true, want false")
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read config file: %v", err)
	}
	if string(raw) != original {
		t.Fatalf("existing file was modified:\nwant:\n%s\ngot:\n%s", original, string(raw))
	}
}

func TestBackupFileMovesConfig(t *testing.T) {
	path := setTempConfigPath(t)
	original := "server_url: https://backup.arcane.example\napi_key: abc123\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatalf("failed to write config fixture: %v", err)
	}

	backupPath, moved, err := BackupFile()
	if err != nil {
		t.Fatalf("BackupFile() failed: %v", err)
	}
	if !moved {
		t.Fatal("BackupFile() moved = false, want true")
	}
	if backupPath != path+".bak" {
		t.Fatalf("backup path = %q, want %q", backupPath, path+".bak")
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected original config to be removed, stat err=%v", err)
	}

	raw, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("failed to read backup file: %v", err)
	}
	if string(raw) != original {
		t.Fatalf("backup content mismatch:\nwant:\n%s\ngot:\n%s", original, string(raw))
	}
}

func TestBackupFileNoConfig(t *testing.T) {
	path := setTempConfigPath(t)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected missing config before backup, stat err=%v", err)
	}

	backupPath, moved, err := BackupFile()
	if err != nil {
		t.Fatalf("BackupFile() failed: %v", err)
	}
	if moved {
		t.Fatal("BackupFile() moved = true, want false")
	}
	if backupPath != path+".bak" {
		t.Fatalf("backup path = %q, want %q", backupPath, path+".bak")
	}
}

func TestBackupFileRotatesExistingBak(t *testing.T) {
	path := setTempConfigPath(t)
	backupPath := path + ".bak"

	if err := os.WriteFile(path, []byte("server_url: https://new.example\n"), 0o600); err != nil {
		t.Fatalf("failed to write primary config: %v", err)
	}
	if err := os.WriteFile(backupPath, []byte("server_url: https://old.example\n"), 0o600); err != nil {
		t.Fatalf("failed to write existing backup: %v", err)
	}

	newBackupPath, moved, err := BackupFile()
	if err != nil {
		t.Fatalf("BackupFile() failed: %v", err)
	}
	if !moved {
		t.Fatal("BackupFile() moved = false, want true")
	}
	if newBackupPath != backupPath {
		t.Fatalf("backup path = %q, want %q", newBackupPath, backupPath)
	}

	raw, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("failed to read newest backup: %v", err)
	}
	if !strings.Contains(string(raw), "new.example") {
		t.Fatalf("expected newest backup to contain new config, got:\n%s", string(raw))
	}

	rotated, err := filepath.Glob(backupPath + ".*")
	if err != nil {
		t.Fatalf("failed to glob rotated backups: %v", err)
	}
	if len(rotated) == 0 {
		t.Fatalf("expected rotated backup matching %q", backupPath+".*")
	}
}
