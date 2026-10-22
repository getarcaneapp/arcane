// Package config loads and persists CLI configuration in ~/.config/arcanecli.yml and session tokens in ~/.arcanecli/auth.json.
package config

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofrs/flock"
	"github.com/samber/hot"
	"go.getarcane.app/acfs/atomic"
	"go.yaml.in/yaml/v4"

	"github.com/getarcaneapp/arcane/cli/v3/internal/output"
	"github.com/getarcaneapp/arcane/cli/v3/internal/types"
)

// Version and build information - set via ldflags at build time
var (
	Version          = "dev"
	Revision         = "unknown"
	CLIStableBaseURL = "https://github.com/getarcaneapp/arcane/releases/download"
	CLINextBaseURL   = "https://bucket.getarcane.app/bin/cli-next"
)

const (
	configFileName             = "arcanecli.yml"
	defaultPaginationInitLimit = 20
)

// authTokens are the session tokens saved for one server.
type authTokens struct {
	AccessToken  string `json:"accessToken,omitempty"`
	RefreshToken string `json:"refreshToken,omitempty"`
}

var customConfigPath string

var configCache = hot.NewHotCache[string, *types.Config](hot.LRU, 4).
	WithCopyOnRead((*types.Config).Clone).
	WithCopyOnWrite((*types.Config).Clone).
	Build()

func invalidateCache() {
	configCache.Purge()
}

func DefaultConfig() *types.Config {
	return &types.Config{
		ServerURL: "http://localhost:3552",
		LogLevel:  "info",
	}
}

// ConfigPath resolves the active config path, defaulting to ~/.config/arcanecli.yml.
func ConfigPath() (string, error) {
	if customConfigPath != "" {
		return customConfigPath, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	return filepath.Join(home, ".config", configFileName), nil
}

// SetConfigPath overrides the default configuration file location.
// Accepts absolute or relative paths and expands a leading ~ to the home directory.
func SetConfigPath(path string) error {
	if strings.TrimSpace(path) == "" {
		customConfigPath = ""
		invalidateCache()
		return nil
	}

	path = strings.TrimSpace(path)
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to expand config path: %w", err)
		}
		rel := strings.TrimPrefix(path, "~")
		path = filepath.Join(home, strings.TrimPrefix(rel, string(os.PathSeparator)))
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to resolve config path: %w", err)
	}

	customConfigPath = absPath
	invalidateCache()
	return nil
}

// Load reads configuration, using defaults when the file does not exist.
func Load() (*types.Config, error) {
	path, err := ConfigPath()
	if err != nil {
		return nil, err
	}

	if cfg, ok, _ := configCache.Get(path); ok {
		return cfg, nil
	}

	cfg := DefaultConfig()
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if parseErr := yaml.Unmarshal(data, cfg); parseErr != nil {
			return nil, fmt.Errorf("failed to parse config file: %w", parseErr)
		}
	case !os.IsNotExist(err):
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}
	// API key configs ignore saved sessions so they keep authenticating as the key.
	if cfg.APIKey == "" {
		auth, authErr := loadAuth()
		if authErr != nil {
			output.Warning("Ignoring saved sessions: %v", authErr)
		}
		tokens := auth[authKey(cfg.ServerURL)]
		cfg.JWTToken, cfg.RefreshToken = tokens.AccessToken, tokens.RefreshToken
	}
	normalized := cfg.Clone()
	if normalized.Pagination.Resources == nil {
		normalized.Pagination.Resources = make(map[string]types.PaginationResourceConfig)
	}

	configCache.Set(path, normalized)

	return normalized, nil
}

// Save writes settings with mode 0600 without changing saved sessions.
func Save(ctx context.Context, c *types.Config) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := ConfigPath()
	if err != nil {
		return err
	}

	cfg := DefaultConfig()
	if c != nil {
		cfg = c.Clone()
	}
	cfg.Pagination.Default.Limit = max(0, cfg.Pagination.Default.Limit)
	resources := cfg.Pagination.Resources
	cfg.Pagination.Resources = make(map[string]types.PaginationResourceConfig, len(resources))
	for resource, rc := range resources {
		if rc.Limit > 0 {
			cfg.SetResourceLimit(resource, rc.Limit)
		}
	}

	if writeErr := writeConfig(path, cfg, false); writeErr != nil {
		return writeErr
	}

	invalidateCache()

	return nil
}

// ClearTokens removes the current server's saved session and clears tokens from the active config.
func ClearTokens(ctx context.Context, cfg *types.Config) error {
	cfg.JWTToken, cfg.RefreshToken = "", ""
	return SaveTokens(ctx, cfg)
}

// InitDefaultFile creates a template with credential placeholders and returns false if the file already exists.
func InitDefaultFile() (bool, error) {
	path, err := ConfigPath()
	if err != nil {
		return false, err
	}

	info, err := os.Stat(path)
	switch {
	case err == nil:
		if info.IsDir() {
			return false, fmt.Errorf("config path is a directory: %s", path)
		}
		return false, nil
	case os.IsNotExist(err):
		// Continue and create the file below.
	default:
		return false, fmt.Errorf("failed to stat config path: %w", err)
	}

	cfg := DefaultConfig()
	cfg.SetDefaultLimit(defaultPaginationInitLimit)
	for _, resource := range types.KnownPaginatedResources {
		cfg.SetResourceLimit(resource, defaultPaginationInitLimit)
	}
	var template yaml.Node
	if encodeErr := template.Encode(cfg); encodeErr != nil {
		return false, fmt.Errorf("failed to encode config template: %w", encodeErr)
	}
	// Show empty authentication fields even though normal saves omit them.
	for _, key := range []string{"api_key", "federated_audience"} {
		template.Content = append(template.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: ""},
		)
	}

	if writeErr := writeConfig(path, &template, true); writeErr != nil {
		if errors.Is(writeErr, os.ErrExist) {
			return false, nil
		}
		return false, writeErr
	}

	invalidateCache()
	return true, nil
}

func writeConfig(path string, cfg any, exclusive bool) (err error) {
	data, marshalErr := yaml.Marshal(cfg)
	if marshalErr != nil {
		return fmt.Errorf("failed to marshal config: %w", marshalErr)
	}
	if mkdirErr := os.MkdirAll(filepath.Dir(path), 0o700); mkdirErr != nil {
		return fmt.Errorf("failed to create config directory: %w", mkdirErr)
	}
	writePath := path
	if exclusive {
		stagingDir, tempErr := os.MkdirTemp(filepath.Dir(path), ".arcanecli-*")
		if tempErr != nil {
			return fmt.Errorf("failed to create config staging directory: %w", tempErr)
		}
		defer func() {
			if cleanupErr := os.RemoveAll(stagingDir); cleanupErr != nil {
				err = errors.Join(err, fmt.Errorf("failed to remove config staging directory: %w", cleanupErr))
			}
		}()
		writePath = filepath.Join(stagingDir, configFileName)
	}
	if writeErr := atomic.WriteFile(writePath, data, 0o600); writeErr != nil {
		return fmt.Errorf("failed to write config file: %w", writeErr)
	}
	if exclusive {
		// Linking publishes the complete file without replacing an existing path.
		if linkErr := os.Link(writePath, path); linkErr != nil {
			return fmt.Errorf("failed to create config file: %w", linkErr)
		}
	}
	return nil
}

// AuthPath returns the session token file, ~/.arcanecli/auth.json.
func AuthPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(home, ".arcanecli", "auth.json"), nil
}

// SwitchServer points cfg at serverURL and loads that server's saved session without changing auth.json.
func SwitchServer(ctx context.Context, cfg *types.Config, serverURL string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if authKey(serverURL) == authKey(cfg.ServerURL) {
		cfg.ServerURL = serverURL
		return nil
	}
	cfg.ServerURL, cfg.JWTToken, cfg.RefreshToken = serverURL, "", ""
	if cfg.APIKey != "" {
		return nil
	}
	auth, err := loadAuth()
	if err != nil {
		return err
	}
	tokens := auth[authKey(serverURL)]
	cfg.JWTToken, cfg.RefreshToken = tokens.AccessToken, tokens.RefreshToken
	return nil
}

func authKey(serverURL string) string {
	return strings.TrimRight(strings.TrimSpace(serverURL), "/")
}

func loadAuth() (map[string]authTokens, error) {
	path, err := AuthPath()
	if err != nil {
		return nil, err
	}
	auth := make(map[string]authTokens)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return auth, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read auth file: %w", err)
	}
	if unmarshalErr := json.Unmarshal(data, &auth); unmarshalErr != nil {
		return nil, fmt.Errorf("failed to parse auth file %s: %w (run `arcane-cli config backup --auth` to set it aside)", path, unmarshalErr)
	}
	if auth == nil {
		auth = make(map[string]authTokens)
	}
	return auth, nil
}

// lockAuth serializes auth.json updates across CLI processes.
func lockAuth(ctx context.Context) (func() error, error) {
	path, err := AuthPath()
	if err != nil {
		return nil, err
	}
	if mkdirErr := os.MkdirAll(filepath.Dir(path), 0o700); mkdirErr != nil {
		return nil, fmt.Errorf("failed to create auth directory: %w", mkdirErr)
	}
	lock := flock.New(path + ".lock")
	if _, lockErr := lock.TryLockContext(ctx, 100*time.Millisecond); lockErr != nil {
		return nil, fmt.Errorf("failed to lock auth file: %w", lockErr)
	}
	return func() error {
		if unlockErr := lock.Unlock(); unlockErr != nil {
			return fmt.Errorf("failed to unlock auth file: %w", unlockErr)
		}
		return nil
	}, nil
}

// SaveTokens explicitly stores or removes the current server's session before saving its settings.
func SaveTokens(ctx context.Context, cfg *types.Config) (err error) {
	unlock, err := lockAuth(ctx)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, unlock()) }()
	auth, err := loadAuth()
	if err != nil {
		return err
	}
	key := authKey(cfg.ServerURL)
	tokens := authTokens{AccessToken: cfg.JWTToken, RefreshToken: cfg.RefreshToken}
	if auth[key] != tokens {
		if tokens == (authTokens{}) {
			delete(auth, key)
		} else {
			auth[key] = tokens
		}
		path, pathErr := AuthPath()
		if pathErr != nil {
			return pathErr
		}
		if len(auth) == 0 {
			if removeErr := os.Remove(path); removeErr != nil && !os.IsNotExist(removeErr) {
				return fmt.Errorf("failed to remove auth file: %w", removeErr)
			}
		} else {
			data, marshalErr := json.Marshal(auth, jsontext.WithIndent("  "))
			if marshalErr != nil {
				return fmt.Errorf("failed to marshal auth file: %w", marshalErr)
			}
			if writeErr := atomic.WriteFile(path, append(data, '\n'), 0o600); writeErr != nil {
				return fmt.Errorf("failed to write auth file: %w", writeErr)
			}
		}
	}
	return Save(ctx, cfg)
}

// BackupFile moves the active config to .bak, returning moved=false when no config exists.
func BackupFile() (backupPath string, moved bool, err error) {
	path, err := ConfigPath()
	if err != nil {
		return "", false, err
	}
	return moveToBackup(path, "config")
}

// BackupAuthFile moves auth.json to .bak so a damaged file stops blocking saves.
func BackupAuthFile(ctx context.Context) (backupPath string, moved bool, err error) {
	path, err := AuthPath()
	if err != nil {
		return "", false, err
	}
	unlock, err := lockAuth(ctx)
	if err != nil {
		return "", false, err
	}
	defer func() { err = errors.Join(err, unlock()) }()
	return moveToBackup(path, "auth")
}

func moveToBackup(path, kind string) (backupPath string, moved bool, err error) {
	backupPath = path + ".bak"

	info, err := os.Stat(path)
	switch {
	case err == nil:
		if info.IsDir() {
			return "", false, fmt.Errorf("%s path is a directory: %s", kind, path)
		}
	case os.IsNotExist(err):
		return backupPath, false, nil
	default:
		return "", false, fmt.Errorf("failed to stat %s path: %w", kind, err)
	}

	if existingBackup, backupErr := os.Stat(backupPath); backupErr == nil {
		if existingBackup.IsDir() {
			return "", false, fmt.Errorf("backup path is a directory: %s", backupPath)
		}
		rotatedPath := fmt.Sprintf("%s.%s", backupPath, time.Now().UTC().Format("20060102150405"))
		if renameErr := os.Rename(backupPath, rotatedPath); renameErr != nil {
			return "", false, fmt.Errorf("failed to rotate existing backup %s: %w", backupPath, renameErr)
		}
	} else if !os.IsNotExist(backupErr) {
		return "", false, fmt.Errorf("failed to stat backup path: %w", backupErr)
	}

	if backupConfigErr := os.Rename(path, backupPath); backupConfigErr != nil {
		return "", false, fmt.Errorf("failed to move %s file to backup: %w", kind, backupConfigErr)
	}

	invalidateCache()
	return backupPath, true, nil
}
