// Package config handles CLI configuration loading and persistence.
//
// Configuration is stored in a YAML file at ~/.config/arcanecli.yml.
// This package provides functions to load, save, and access configuration
// values including the server URL, API key, and default environment.
//
// # Configuration File
//
// The configuration file uses the following format:
//
//	server_url: https://your-server.com
//	api_key: your-api-key
//	default_environment: "0"
//	log_level: info
//
// # Version Information
//
// Version and Revision variables are set at build time via ldflags:
//
//	go build -ldflags "-X github.com/getarcaneapp/arcane/cli/v2/internal/config.Version=1.0.0"
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/cli/v2/internal/types"
	"github.com/go-viper/mapstructure/v2"
	"github.com/samber/hot"
	"github.com/spf13/viper"
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

var customConfigPath string

var configCache = hot.NewHotCache[string, *types.Config](hot.LRU, 4).
	WithCopyOnRead(cloneConfig).
	WithCopyOnWrite(cloneConfig).
	Build()

func cloneConfig(cfg *types.Config) *types.Config {
	return cfg.Clone()
}

func normalizeConfig(cfg *types.Config) *types.Config {
	if cfg == nil {
		return DefaultConfig()
	}
	normalized := cfg.Clone()
	if normalized == nil {
		return DefaultConfig()
	}

	if normalized.Pagination.Resources == nil {
		normalized.Pagination.Resources = make(map[string]types.PaginationResourceConfig)
	}

	return normalized
}

func invalidateCache() {
	configCache.Purge()
}

// DefaultConfig returns a Config with sensible default values.
// The defaults are:
//   - ServerURL: http://localhost:3552
//   - DefaultEnvironment: "0"
//   - LogLevel: "info"
func DefaultConfig() *types.Config {
	return &types.Config{
		ServerURL:          "http://localhost:3552",
		DefaultEnvironment: "0",
		LogLevel:           "info",
	}
}

// ConfigPath returns the absolute path to the configuration file.
// The config file is located at ~/.config/arcanecli.yml.
// Returns an error if the user's home directory cannot be determined.
func ConfigPath() (string, error) {
	if customConfigPath != "" {
		return customConfigPath, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", errors.WrapIf(err, "failed to get home directory")
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
			return errors.WrapIf(err, "failed to expand config path")
		}
		rel := strings.TrimPrefix(path, "~")
		path = filepath.Join(home, strings.TrimPrefix(rel, string(os.PathSeparator)))
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return errors.WrapIf(err, "failed to resolve config path")
	}

	customConfigPath = absPath
	invalidateCache()
	return nil
}

// Load reads the configuration from disk and returns it.
// If the config file does not exist, default values are returned.
// Returns an error if the file exists but cannot be read or parsed.
func Load() (*types.Config, error) {
	path, err := ConfigPath()
	if err != nil {
		return nil, err
	}

	if cfg, ok, _ := configCache.Get(path); ok {
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			def := DefaultConfig()
			configCache.Set(path, def)
			return def, nil
		}
		return nil, errors.WrapIf(err, "failed to read config file")
	}
	_ = data

	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")
	v.SetDefault("server_url", "http://localhost:3552")
	v.SetDefault("default_environment", "0")
	v.SetDefault("federated_audience", "")
	v.SetDefault("log_level", "info")
	if err := v.ReadInConfig(); err != nil {
		return nil, errors.WrapIf(err, "failed to parse config file")
	}

	var cfg types.Config
	if err := v.Unmarshal(&cfg, func(dc *mapstructure.DecoderConfig) {
		dc.TagName = "mapstructure"
		dc.WeaklyTypedInput = true
	}); err != nil {
		return nil, errors.WrapIf(err, "failed to unmarshal config")
	}
	normalized := normalizeConfig(&cfg)

	configCache.Set(path, normalized)

	return normalized, nil
}

// Save writes the configuration to disk.
// The config directory is created if it does not exist.
// The file is created with 0600 permissions for security.
func Save(c *types.Config) error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}

	// Ensure the config directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return errors.WrapIf(err, "failed to create config directory")
	}

	cfg := normalizeConfig(c)
	v := viper.New()
	v.SetConfigType("yaml")
	v.Set("server_url", cfg.ServerURL)
	if cfg.APIKey != "" {
		v.Set("api_key", cfg.APIKey)
	}
	if cfg.JWTToken != "" {
		v.Set("jwt_token", cfg.JWTToken)
	}
	if cfg.RefreshToken != "" {
		v.Set("refresh_token", cfg.RefreshToken)
	}
	if cfg.DefaultEnvironment != "" {
		v.Set("default_environment", cfg.DefaultEnvironment)
	}
	if cfg.FederatedAudience != "" {
		v.Set("federated_audience", cfg.FederatedAudience)
	}
	if cfg.LogLevel != "" {
		v.Set("log_level", cfg.LogLevel)
	}
	if cfg.CLIUpdateChannel != "" {
		v.Set("cli_update_channel", cfg.CLIUpdateChannel)
	}

	// Canonical pagination structure.
	if cfg.Pagination.Default.Limit > 0 {
		v.Set("pagination.default.limit", cfg.Pagination.Default.Limit)
	}
	for resource, rc := range cfg.Pagination.Resources {
		resource = types.NormalizePaginatedResource(resource)
		if resource == "" || rc.Limit <= 0 {
			continue
		}
		v.Set(fmt.Sprintf("pagination.resources.%s.limit", resource), rc.Limit)
	}

	if err := v.WriteConfigAs(path); err != nil {
		return errors.WrapIf(err, "failed to write config file")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return errors.WrapIf(err, "failed to set config permissions")
	}

	configCache.Set(path, cfg)

	return nil
}

// InitDefaultFile creates a default config file with all known keys if one does
// not already exist. It returns true when a file is created, or false when an
// existing file is left unchanged.
func InitDefaultFile() (bool, error) {
	path, err := ConfigPath()
	if err != nil {
		return false, err
	}

	info, err := os.Stat(path)
	switch {
	case err == nil:
		if info.IsDir() {
			return false, errors.Errorf("config path is a directory: %s", path)
		}
		return false, nil
	case os.IsNotExist(err):
		// Continue and create the file below.
	default:
		return false, errors.WrapIf(err, "failed to stat config path")
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false, errors.WrapIf(err, "failed to create config directory")
	}

	v := viper.New()
	v.SetConfigType("yaml")
	v.Set("server_url", "http://localhost:3552")
	v.Set("api_key", "")
	v.Set("jwt_token", "")
	v.Set("refresh_token", "")
	v.Set("default_environment", "0")
	v.Set("federated_audience", "")
	v.Set("log_level", "info")

	v.Set("pagination.default.limit", defaultPaginationInitLimit)
	for _, resource := range types.KnownPaginatedResources {
		v.Set(fmt.Sprintf("pagination.resources.%s.limit", resource), defaultPaginationInitLimit)
	}

	if err := v.WriteConfigAs(path); err != nil {
		return false, errors.WrapIf(err, "failed to write config file")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return false, errors.WrapIf(err, "failed to set config permissions")
	}

	invalidateCache()
	return true, nil
}

// BackupFile moves the active config file to a .bak path and removes the
// original file from its previous location. If no config file exists, it
// returns moved=false and no error.
func BackupFile() (backupPath string, moved bool, err error) {
	path, err := ConfigPath()
	if err != nil {
		return "", false, err
	}
	backupPath = path + ".bak"

	info, err := os.Stat(path)
	switch {
	case err == nil:
		if info.IsDir() {
			return "", false, errors.Errorf("config path is a directory: %s", path)
		}
	case os.IsNotExist(err):
		return backupPath, false, nil
	default:
		return "", false, errors.WrapIf(err, "failed to stat config path")
	}

	if existingBackup, backupErr := os.Stat(backupPath); backupErr == nil {
		if existingBackup.IsDir() {
			return "", false, errors.Errorf("backup path is a directory: %s", backupPath)
		}
		rotatedPath := fmt.Sprintf("%s.%s", backupPath, time.Now().UTC().Format("20060102150405"))
		if err := os.Rename(backupPath, rotatedPath); err != nil {
			return "", false, errors.WrapIff(err, "failed to rotate existing backup %s", backupPath)
		}
	} else if !os.IsNotExist(backupErr) {
		return "", false, errors.WrapIf(backupErr, "failed to stat backup path")
	}

	if err := os.Rename(path, backupPath); err != nil {
		return "", false, errors.WrapIf(err, "failed to move config to backup")
	}

	invalidateCache()
	return backupPath, true, nil
}
