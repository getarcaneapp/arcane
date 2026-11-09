package config

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofrs/flock"
	"go.yaml.in/yaml/v4"

	"kit.getarcane.app/acfs"
	acfstypes "kit.getarcane.app/acfs/types"
	"kit.getarcane.app/cache"
	"kit.getarcane.app/fs/atomic"

	"getarcane.dev/cli/v3/internal/output"
	"getarcane.dev/cli/v3/internal/types"
)

type (
	authTokens struct {
		AccessToken  string `json:"accessToken,omitempty"`
		RefreshToken string `json:"refreshToken,omitempty"`
	}
)

const (
	configFileName             = "arcanecli.yml"
	defaultPaginationInitLimit = 20
)

var (
	Version          = "dev"
	Revision         = "unknown"
	CLIStableBaseURL = "https://github.com/getarcaneapp/arcane/releases/download"
	CLINextBaseURL   = "https://bucket.getarcane.app/bin/cli-next"

	customConfigPath string

	configCache = &cache.Cache[string, *types.Config]{
		Clone:    (*types.Config).Clone,
		Capacity: 4,
	}
)

func DefaultConfig() *types.Config {
	return &types.Config{
		ServerURL: "http://localhost:3552",
		LogLevel:  "info",
	}
}

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

func SetConfigPath(path string) error {
	if strings.TrimSpace(path) == "" {
		customConfigPath = ""
		configCache.Purge()
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
	configCache.Purge()
	return nil
}

func Load(ctx context.Context) (*types.Config, error) {
	path, err := ConfigPath()
	if err != nil {
		return nil, err
	}

	if cfg, ok, _ := configCache.Get(path); ok {
		return cfg, nil
	}

	cfg := DefaultConfig()
	data, err := acfs.Host{}.ReadFile(ctx, path)
	switch {
	case err == nil:
		if parseErr := yaml.Unmarshal(data, cfg); parseErr != nil {
			return nil, fmt.Errorf("failed to parse config file: %w", parseErr)
		}
	case !errors.Is(err, fs.ErrNotExist):
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}
	// API key configs ignore saved sessions so they keep authenticating as the key.
	if cfg.APIKey == "" {
		auth, authErr := loadAuth(ctx)
		if authErr != nil {
			output.Warning("Ignoring saved sessions: %v", authErr)
		}
		tokens := auth[strings.TrimRight(strings.TrimSpace(cfg.ServerURL), "/")]
		cfg.JWTToken, cfg.RefreshToken = tokens.AccessToken, tokens.RefreshToken
	}
	normalized := cfg.Clone()
	if normalized.Pagination.Resources == nil {
		normalized.Pagination.Resources = make(map[string]types.PaginationResourceConfig)
	}

	configCache.Set(cache.Entry[string, *types.Config]{
		Key:   path,
		Value: normalized,
	})

	return normalized, nil
}

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

	if writeErr := writeConfig(ctx, path, cfg, false); writeErr != nil {
		return writeErr
	}

	configCache.Purge()

	return nil
}

func ClearTokens(ctx context.Context, cfg *types.Config) error {
	cfg.JWTToken, cfg.RefreshToken = "", ""
	return SaveTokens(ctx, cfg)
}

func InitDefaultFile(ctx context.Context) (bool, error) {
	path, err := ConfigPath()
	if err != nil {
		return false, err
	}

	info, err := acfs.Host{}.Stat(ctx, path, acfstypes.StatOptions{FollowFinal: true})
	switch {
	case err == nil:
		if info.IsDirectory {
			return false, fmt.Errorf("config path is a directory: %s", path)
		}
		return false, nil
	case errors.Is(err, fs.ErrNotExist):
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

	if writeErr := writeConfig(ctx, path, &template, true); writeErr != nil {
		if errors.Is(writeErr, os.ErrExist) {
			return false, nil
		}
		return false, writeErr
	}

	configCache.Purge()
	return true, nil
}

func writeConfig(ctx context.Context, path string, cfg any, exclusive bool) error {
	data, marshalErr := yaml.Marshal(cfg)
	if marshalErr != nil {
		return fmt.Errorf("failed to marshal config: %w", marshalErr)
	}
	if mkdirErr := (acfs.Host{}).MkdirAll(ctx, filepath.Dir(path), 0o700); mkdirErr != nil {
		return fmt.Errorf("failed to create config directory: %w", mkdirErr)
	}
	if exclusive {
		if writeErr := (acfs.Host{}).Write(ctx, path, data, acfs.WriteOptions{Mode: 0o600, Exclusive: true}); writeErr != nil {
			return fmt.Errorf("failed to create config file: %w", writeErr)
		}
		return nil
	}
	if writeErr := atomic.WriteFile(path, data, 0o600); writeErr != nil {
		return fmt.Errorf("failed to write config file: %w", writeErr)
	}
	return nil
}

func AuthPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(home, ".arcanecli", "auth.json"), nil
}

func SwitchServer(ctx context.Context, cfg *types.Config, serverURL string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimRight(strings.TrimSpace(serverURL), "/") == strings.TrimRight(strings.TrimSpace(cfg.ServerURL), "/") {
		cfg.ServerURL = serverURL
		return nil
	}
	cfg.ServerURL, cfg.JWTToken, cfg.RefreshToken = serverURL, "", ""
	if cfg.APIKey != "" {
		return nil
	}
	auth, err := loadAuth(ctx)
	if err != nil {
		return err
	}
	tokens := auth[strings.TrimRight(strings.TrimSpace(serverURL), "/")]
	cfg.JWTToken, cfg.RefreshToken = tokens.AccessToken, tokens.RefreshToken
	return nil
}

func loadAuth(ctx context.Context) (map[string]authTokens, error) {
	path, err := AuthPath()
	if err != nil {
		return nil, err
	}
	auth := make(map[string]authTokens)
	data, err := acfs.Host{}.ReadFile(ctx, path)
	if errors.Is(err, fs.ErrNotExist) {
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
	if mkdirErr := (acfs.Host{}).MkdirAll(ctx, filepath.Dir(path), 0o700); mkdirErr != nil {
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

func SaveTokens(ctx context.Context, cfg *types.Config) (err error) {
	unlock, err := lockAuth(ctx)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, unlock()) }()
	auth, err := loadAuth(ctx)
	if err != nil {
		return err
	}
	key := strings.TrimRight(strings.TrimSpace(cfg.ServerURL), "/")
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
			if removeErr := (acfs.Host{}).Remove(ctx, path); removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
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

func BackupFile(ctx context.Context) (backupPath string, moved bool, err error) {
	path, err := ConfigPath()
	if err != nil {
		return "", false, err
	}
	return moveToBackup(ctx, path, "config")
}

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
	return moveToBackup(ctx, path, "auth")
}

func moveToBackup(ctx context.Context, path, kind string) (backupPath string, moved bool, err error) {
	backupPath = path + ".bak"

	info, err := acfs.Host{}.Stat(ctx, path, acfstypes.StatOptions{FollowFinal: true})
	switch {
	case err == nil:
		if info.IsDirectory {
			return "", false, fmt.Errorf("%s path is a directory: %s", kind, path)
		}
	case errors.Is(err, fs.ErrNotExist):
		return backupPath, false, nil
	default:
		return "", false, fmt.Errorf("failed to stat %s path: %w", kind, err)
	}

	if existingBackup, backupErr := (acfs.Host{}).Stat(ctx, backupPath, acfstypes.StatOptions{FollowFinal: true}); backupErr == nil {
		if existingBackup.IsDirectory {
			return "", false, fmt.Errorf("backup path is a directory: %s", backupPath)
		}
		rotatedPath := fmt.Sprintf("%s.%s", backupPath, time.Now().UTC().Format("20060102150405"))
		if renameErr := (acfs.Host{}).Rename(ctx, backupPath, rotatedPath); renameErr != nil {
			return "", false, fmt.Errorf("failed to rotate existing backup %s: %w", backupPath, renameErr)
		}
	} else if !errors.Is(backupErr, fs.ErrNotExist) {
		return "", false, fmt.Errorf("failed to stat backup path: %w", backupErr)
	}

	if backupConfigErr := (acfs.Host{}).Rename(ctx, path, backupPath); backupConfigErr != nil {
		return "", false, fmt.Errorf("failed to move %s file to backup: %w", kind, backupConfigErr)
	}

	configCache.Purge()
	return backupPath, true, nil
}
