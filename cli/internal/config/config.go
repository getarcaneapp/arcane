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
//	go build -ldflags "-X github.com/getarcaneapp/arcane/cli/internal/config.Version=1.0.0"
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/getarcaneapp/arcane/cli/internal/types"
	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

// Version and build information - set via ldflags at build time
var (
	Version  = "dev"
	Revision = "unknown"
)

const (
	configFileName             = "arcanecli.yml"
	defaultPaginationInitLimit = 20
)

var customConfigPath string

var (
	cacheMu     sync.RWMutex
	cachedPath  string
	cachedCfg   *types.Config
	cacheLoaded bool
)

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

	if normalized.Pagination.Default.Limit <= 0 && normalized.DefaultLimit > 0 {
		normalized.Pagination.Default.Limit = normalized.DefaultLimit
	}
	if normalized.DefaultLimit <= 0 && normalized.Pagination.Default.Limit > 0 {
		normalized.DefaultLimit = normalized.Pagination.Default.Limit
	}

	if normalized.Pagination.Resources == nil {
		normalized.Pagination.Resources = make(map[string]types.PaginationResourceConfig)
	}
	if normalized.ResourceLimits == nil {
		normalized.ResourceLimits = make(map[string]int)
	}

	for resource, limit := range normalized.ResourceLimits {
		resource = types.NormalizePaginatedResource(resource)
		if resource == "" || limit <= 0 {
			continue
		}
		if _, exists := normalized.Pagination.Resources[resource]; !exists {
			normalized.Pagination.Resources[resource] = types.PaginationResourceConfig{Limit: limit}
		}
	}
	for resource, cfg := range normalized.Pagination.Resources {
		resource = types.NormalizePaginatedResource(resource)
		if resource == "" || cfg.Limit <= 0 {
			continue
		}
		normalized.ResourceLimits[resource] = cfg.Limit
	}

	return normalized
}

func invalidateCache() {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	cacheLoaded = false
	cachedPath = ""
	cachedCfg = nil
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

// Load reads the configuration from disk and returns it.
// If the config file does not exist, default values are returned.
// Returns an error if the file exists but cannot be read or parsed.
func Load() (*types.Config, error) {
	path, err := ConfigPath()
	if err != nil {
		return nil, err
	}

	cacheMu.RLock()
	if cacheLoaded && cachedCfg != nil && cachedPath == path {
		cfg := cloneConfig(cachedCfg)
		cacheMu.RUnlock()
		return cfg, nil
	}
	cacheMu.RUnlock()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			def := DefaultConfig()
			cacheMu.Lock()
			cacheLoaded = true
			cachedPath = path
			cachedCfg = cloneConfig(def)
			cacheMu.Unlock()
			return def, nil
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}
	_ = data

	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")
	v.SetDefault("server_url", "http://localhost:3552")
	v.SetDefault("default_environment", "0")
	v.SetDefault("log_level", "info")
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	var cfg types.Config
	if err := v.Unmarshal(&cfg, func(dc *mapstructure.DecoderConfig) {
		dc.TagName = "mapstructure"
		dc.WeaklyTypedInput = true
	}); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}
	normalized := normalizeConfig(&cfg)

	cacheMu.Lock()
	cacheLoaded = true
	cachedPath = path
	cachedCfg = cloneConfig(normalized)
	cacheMu.Unlock()

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
		return fmt.Errorf("failed to create config directory: %w", err)
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
	if cfg.LogLevel != "" {
		v.Set("log_level", cfg.LogLevel)
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

	// Legacy keys retained for backward compatibility.
	if cfg.DefaultLimit > 0 {
		v.Set("default_limit", cfg.DefaultLimit)
	}
	if len(cfg.ResourceLimits) > 0 {
		legacy := make(map[string]int)
		for resource, limit := range cfg.ResourceLimits {
			resource = types.NormalizePaginatedResource(resource)
			if resource == "" || limit <= 0 {
				continue
			}
			legacy[resource] = limit
		}
		if len(legacy) > 0 {
			v.Set("resource_limits", legacy)
		}
	}

	if err := v.WriteConfigAs(path); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("failed to set config permissions: %w", err)
	}

	cacheMu.Lock()
	cacheLoaded = true
	cachedPath = path
	cachedCfg = cloneConfig(cfg)
	cacheMu.Unlock()

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
			return false, fmt.Errorf("config path is a directory: %s", path)
		}
		return false, nil
	case os.IsNotExist(err):
		// Continue and create the file below.
	default:
		return false, fmt.Errorf("failed to stat config path: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false, fmt.Errorf("failed to create config directory: %w", err)
	}

	v := viper.New()
	v.SetConfigType("yaml")
	v.Set("server_url", "http://localhost:3552")
	v.Set("api_key", "")
	v.Set("jwt_token", "")
	v.Set("refresh_token", "")
	v.Set("default_environment", "0")
	v.Set("log_level", "info")

	v.Set("pagination.default.limit", defaultPaginationInitLimit)
	for _, resource := range types.KnownPaginatedResources {
		v.Set(fmt.Sprintf("pagination.resources.%s.limit", resource), defaultPaginationInitLimit)
	}

	v.Set("default_limit", defaultPaginationInitLimit)
	legacy := make(map[string]int, len(types.KnownPaginatedResources))
	for _, resource := range types.KnownPaginatedResources {
		legacy[resource] = defaultPaginationInitLimit
	}
	v.Set("resource_limits", legacy)

	if err := v.WriteConfigAs(path); err != nil {
		return false, fmt.Errorf("failed to write config file: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return false, fmt.Errorf("failed to set config permissions: %w", err)
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
			return "", false, fmt.Errorf("config path is a directory: %s", path)
		}
	case os.IsNotExist(err):
		return backupPath, false, nil
	default:
		return "", false, fmt.Errorf("failed to stat config path: %w", err)
	}

	if existingBackup, backupErr := os.Stat(backupPath); backupErr == nil {
		if existingBackup.IsDir() {
			return "", false, fmt.Errorf("backup path is a directory: %s", backupPath)
		}
		rotatedPath := fmt.Sprintf("%s.%s", backupPath, time.Now().UTC().Format("20060102150405"))
		if err := os.Rename(backupPath, rotatedPath); err != nil {
			return "", false, fmt.Errorf("failed to rotate existing backup %s: %w", backupPath, err)
		}
	} else if !os.IsNotExist(backupErr) {
		return "", false, fmt.Errorf("failed to stat backup path: %w", backupErr)
	}

	if err := os.Rename(path, backupPath); err != nil {
		return "", false, fmt.Errorf("failed to move config to backup: %w", err)
	}

	invalidateCache()
	return backupPath, true, nil
}
