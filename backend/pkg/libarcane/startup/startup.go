package startup

import (
	"context"
	"log/slog"
	"os"
	"runtime/debug"

	"go.getarcane.app/sys/cgroup"

	"github.com/getarcaneapp/arcane/backend/v2/buildables"
)

type RuntimeConfig struct {
	AgentMode         bool
	AgentToken        string
	Environment       string
	EncryptionKey     string
	AutoLoginUsername string
	AdminStaticAPIKey string
}

// ApplyMemoryLimit sets the Go soft memory limit to 90% of the cgroup memory
// limit so the GC tightens before the container is OOM-killed. GOMEMLIMIT wins.
func ApplyMemoryLimit(ctx context.Context) {
	if os.Getenv("GOMEMLIMIT") != "" {
		return
	}
	limits, err := cgroup.DetectLimits()
	// cgroup v1 reports "no limit" as a page-aligned value near MaxInt64.
	if err != nil || limits.MemoryLimit <= 0 || limits.MemoryLimit >= 1<<62 {
		return
	}
	limit := limits.MemoryLimit / 10 * 9
	debug.SetMemoryLimit(limit)
	slog.InfoContext(ctx, "Set Go memory limit from cgroup", "cgroupLimitBytes", limits.MemoryLimit, "goMemoryLimitBytes", limit)
}

func LoadAgentToken(ctx context.Context, cfg *RuntimeConfig, getSettingFunc func(context.Context, string, string) string) {
	if cfg.AgentMode && cfg.AgentToken == "" {
		if tok := getSettingFunc(ctx, "agentToken", ""); tok != "" {
			cfg.AgentToken = tok
			slog.InfoContext(ctx, "Loaded agent token from database")
		}
	}
}

func EnsureEncryptionKey(ctx context.Context, cfg *RuntimeConfig, ensureKeyFunc func(context.Context) (string, error)) {
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

type SettingsPruner interface {
	PruneUnknownSettings(ctx context.Context) error
}

func InitializeDefaultSettings(ctx context.Context, cfg *RuntimeConfig, settingsMgr SettingsManager) {
	slog.InfoContext(ctx, "Ensuring default settings are initialized")

	if err := settingsMgr.EnsureDefaultSettings(ctx); err != nil {
		slog.WarnContext(ctx, "Failed to initialize default settings", "error", err.Error())
	} else {
		slog.InfoContext(ctx, "Default settings initialized successfully")
	}

	if err := settingsMgr.PersistEnvSettingsIfMissing(ctx); err != nil {
		slog.WarnContext(ctx, "Failed to persist env-driven settings", "error", err.Error())
	} else {
		slog.DebugContext(ctx, "Persisted env-driven settings")
	}
}

func CleanupUnknownSettings(ctx context.Context, settingsMgr SettingsPruner) {
	if err := settingsMgr.PruneUnknownSettings(ctx); err != nil {
		slog.ErrorContext(ctx, "Failed to prune unknown settings", "error", err.Error())
	}
}

func TestDockerConnection(ctx context.Context, testFunc func(context.Context) error) {
	if err := testFunc(ctx); err != nil {
		slog.WarnContext(ctx, "Docker connection failed during init, local Docker features may be unavailable", "error", err.Error())
	}
}

func CleanupOrphanedVolumeHelpers(ctx context.Context, cleanupFunc func(context.Context) (int, error)) {
	if cleanupFunc == nil {
		return
	}

	slog.InfoContext(ctx, "Checking for orphaned volume helper containers from previous runs")

	removedCount, err := cleanupFunc(ctx)
	if err != nil {
		slog.WarnContext(ctx, "Failed to clean up orphaned volume helper containers during startup", "error", err.Error())
		return
	}

	slog.InfoContext(ctx, "Orphaned volume helper cleanup completed", "removedCount", removedCount)
}

func InitializeNonAgentFeatures(
	ctx context.Context,
	cfg *RuntimeConfig,
	ensureBuiltInRolesFunc func(context.Context) error,
	createAdminFunc func(context.Context) error,
	reconcileDefaultAdminAPIKeyFunc func(context.Context) error,
	autoLoginInitFunc func(context.Context) error,
) {
	if cfg.AgentMode {
		return
	}

	if ensureBuiltInRolesFunc != nil {
		if err := ensureBuiltInRolesFunc(ctx); err != nil {
			slog.ErrorContext(ctx, "Failed to seed built-in roles before admin bootstrap", "error", err.Error())
		}
	}

	if err := createAdminFunc(ctx); err != nil {
		slog.WarnContext(ctx, "Failed to create default admin user", "error", err.Error())
	}

	if reconcileDefaultAdminAPIKeyFunc != nil {
		if err := reconcileDefaultAdminAPIKeyFunc(ctx); err != nil {
			slog.WarnContext(ctx, "Failed to reconcile default admin API key", "error", err.Error())
		}
	}

	if err := autoLoginInitFunc(ctx); err != nil {
		slog.WarnContext(ctx, "Failed to initialize auto-login", "error", err.Error())
	}
}

func InitializeAutoLogin(ctx context.Context, cfg *RuntimeConfig) {
	if !buildables.HasBuildFeature("autologin") {
		return
	}
	slog.WarnContext(ctx, "⚠️  AUTO-LOGIN IS ENABLED - This is a security risk! Do NOT use in production.", "username", cfg.AutoLoginUsername)
	slog.WarnContext(ctx, "⚠️  Auto-login will automatically authenticate users without requiring credentials.")
}
