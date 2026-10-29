package startup

import (
	"context"
	"log/slog"
	"os"
	"runtime/debug"

	"github.com/distribution/reference"
	"go.getarcane.app/sys/cgroup"

	"github.com/getarcaneapp/arcane/backend/v3/buildables"
)

const unlimitedGCPercent = 50

// ApplyMemoryLimit sets the Go soft memory limit to 90% of the cgroup memory
// limit so the GC tightens before the container is OOM-killed. Without a cgroup
// limit it lowers GOGC to 50 instead. GOMEMLIMIT and GOGC win.
func ApplyMemoryLimit(ctx context.Context) {
	if os.Getenv("GOMEMLIMIT") != "" {
		return
	}
	limits, err := cgroup.DetectLimits()
	// cgroup v1 reports "no limit" as a page-aligned value near MaxInt64.
	if err != nil || limits.MemoryLimit <= 0 || limits.MemoryLimit >= 1<<62 {
		if os.Getenv("GOGC") == "" {
			debug.SetGCPercent(unlimitedGCPercent)
			slog.InfoContext(ctx, "Lowered Go GC target without a cgroup memory limit", "gcPercent", unlimitedGCPercent)
		}
		return
	}
	limit := limits.MemoryLimit / 10 * 9
	debug.SetMemoryLimit(limit)
	slog.InfoContext(ctx, "Set Go memory limit from cgroup", "cgroupLimitBytes", limits.MemoryLimit, "goMemoryLimitBytes", limit)
}

type SettingsPruner interface {
	PruneUnknownSettings(ctx context.Context) error
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

var deprecatedImageReplacements = map[string]string{
	"ghcr.io/getarcaneapp/arcane":          "ghcr.io/getarcaneapp/manager",
	"ghcr.io/getarcaneapp/arcane-headless": "ghcr.io/getarcaneapp/agent",
}

func WarnDeprecatedImage(ctx context.Context, imageFunc func(context.Context) (string, error)) {
	imageRef, err := imageFunc(ctx)
	if err != nil {
		slog.DebugContext(ctx, "Could not detect Arcane container image for deprecation check", "error", err)
		return
	}

	named, err := reference.ParseNormalizedNamed(imageRef)
	if err != nil {
		return
	}

	if replacement, ok := deprecatedImageReplacements[named.Name()]; ok {
		slog.WarnContext(ctx, "This Arcane image is deprecated and will be removed in v3; switch to the replacement image", "image", imageRef, "replacement", replacement)
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
	agentMode bool,
	ensureBuiltInRolesFunc func(context.Context) error,
	createAdminFunc func(context.Context) error,
	reconcileDefaultAdminAPIKeyFunc func(context.Context) error,
	autoLoginInitFunc func(context.Context) error,
) {
	if agentMode {
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

func InitializeAutoLogin(ctx context.Context, username string) {
	if !buildables.HasBuildFeature("autologin") {
		return
	}
	slog.WarnContext(ctx, "⚠️  AUTO-LOGIN IS ENABLED - This is a security risk! Do NOT use in production.", "username", username)
	slog.WarnContext(ctx, "⚠️  Auto-login will automatically authenticate users without requiring credentials.")
}
