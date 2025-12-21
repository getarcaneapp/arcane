package utils

import (
	"context"
	"log/slog"

	"github.com/getarcaneapp/arcane/backend/internal/config"
)

func LoadAgentToken(ctx context.Context, cfg *config.Config, getSettingFunc func(context.Context, string, string) string) {
	if cfg.AgentMode && cfg.AgentToken == "" {
		if tok := getSettingFunc(ctx, "agentToken", ""); tok != "" {
			cfg.AgentToken = tok
			slog.InfoContext(ctx, "Loaded agent token from database")
		}
	}
}

func EnsureEncryptionKey(ctx context.Context, cfg *config.Config, ensureKeyFunc func(context.Context) (string, error)) {
	if cfg.AgentMode || cfg.Environment != "production" {
		key, err := ensureKeyFunc(ctx)
		if err != nil {
			slog.WarnContext(ctx, "Failed to ensure encryption key; falling back to derived behavior", "error", err.Error())
			return
		}
		cfg.EncryptionKey = key
	}
}

type SettingsManager interface {
	PersistEnvSettingsIfMissing(ctx context.Context) error
	SetBoolSetting(ctx context.Context, key string, value bool) error
	EnsureDefaultSettings(ctx context.Context) error
}

func InitializeDefaultSettings(ctx context.Context, cfg *config.Config, settingsMgr SettingsManager) {
	slog.InfoContext(ctx, "Ensuring default settings are initialized")

	if err := settingsMgr.EnsureDefaultSettings(ctx); err != nil {
		slog.WarnContext(ctx, "Failed to initialize default settings", "error", err.Error())
	} else {
		slog.InfoContext(ctx, "Default settings initialized successfully")
	}

	if cfg.AgentMode || cfg.UIConfigurationDisabled {
		if err := settingsMgr.PersistEnvSettingsIfMissing(ctx); err != nil {
			slog.WarnContext(ctx, "Failed to persist env-driven settings", "error", err.Error())
		} else {
			slog.DebugContext(ctx, "Persisted env-driven settings if missing")
		}
	}
}

func TestDockerConnection(ctx context.Context, testFunc func(context.Context) error) {
	if err := testFunc(ctx); err != nil {
		slog.WarnContext(ctx, "Docker connection failed during init, local Docker features may be unavailable", "error", err.Error())
	}
}

func InitializeNonAgentFeatures(ctx context.Context, cfg *config.Config, createAdminFunc func(context.Context) error, migrateOidcFunc func(context.Context) error) {
	if cfg.AgentMode {
		return
	}

	if err := createAdminFunc(ctx); err != nil {
		slog.WarnContext(ctx, "Failed to create default admin user", "error", err.Error())
	}

	// Migrate legacy OIDC JSON config to individual fields (runs before env sync)
	if migrateOidcFunc != nil {
		if err := migrateOidcFunc(ctx); err != nil {
			slog.WarnContext(ctx, "Failed to migrate OIDC config to individual fields", "error", err.Error())
		}
	}
}
