package utils

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/backend/buildables"
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

type SettingsPruner interface {
	PruneUnknownSettings(ctx context.Context) error
}

type IntervalMigrationItem struct {
	ID       string
	RawValue string
}

func InitializeDefaultSettings(ctx context.Context, cfg *config.Config, settingsMgr SettingsManager) {
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

func InitializeNonAgentFeatures(ctx context.Context, cfg *config.Config, createAdminFunc func(context.Context) error, autoLoginInitFunc func(context.Context) error, migrateOidcFunc func(context.Context) error, migrateDiscordFunc func(context.Context) error) {
	if cfg.AgentMode {
		return
	}

	if err := createAdminFunc(ctx); err != nil {
		slog.WarnContext(ctx, "Failed to create default admin user", "error", err.Error())
	}

	if err := autoLoginInitFunc(ctx); err != nil {
		slog.WarnContext(ctx, "Failed to initialize auto-login", "error", err.Error())
	}

	// Migrate legacy OIDC JSON config to individual fields (runs before env sync)
	if migrateOidcFunc != nil {
		if err := migrateOidcFunc(ctx); err != nil {
			slog.WarnContext(ctx, "Failed to migrate OIDC config to individual fields", "error", err.Error())
		}
	}

	// Migrate legacy Discord webhookUrl to separate webhookId and token fields
	if migrateDiscordFunc != nil {
		if err := migrateDiscordFunc(ctx); err != nil {
			slog.WarnContext(ctx, "Failed to migrate Discord webhook config", "error", err.Error())
		}
	}
}

func InitializeAutoLogin(ctx context.Context, cfg *config.Config) {
	if !buildables.HasBuildFeature("autologin") {
		return
	}
	slog.WarnContext(ctx, "⚠️  AUTO-LOGIN IS ENABLED - This is a security risk! Do NOT use in production.", "username", cfg.AutoLoginUsername)
	slog.WarnContext(ctx, "⚠️  Auto-login will automatically authenticate users without requiring credentials.")
}

func MigrateSchedulerCronValues(
	ctx context.Context,
	getSettingFunc func(context.Context, string, string) string,
	updateSettingFunc func(context.Context, string, string) error,
	reloadFunc func(context.Context) error,
) {
	migrations := []struct {
		key         string
		defaultUnit time.Duration
	}{
		{key: "pollingInterval", defaultUnit: time.Minute},
		{key: "autoUpdateInterval", defaultUnit: time.Minute},
		{key: "scheduledPruneInterval", defaultUnit: time.Minute},
		{key: "environmentHealthInterval", defaultUnit: time.Minute},
		{key: "eventCleanupInterval", defaultUnit: time.Minute},
		{key: "analyticsHeartbeatInterval", defaultUnit: time.Minute},
	}

	changed := false
	for _, item := range migrations {
		raw := strings.TrimSpace(getSettingFunc(ctx, item.key, ""))
		if raw == "" {
			continue
		}

		cronValue, shouldUpdate, warn := normalizeSchedulerValueToCron(raw, item.defaultUnit)
		if warn != "" {
			slog.WarnContext(ctx, warn, "key", item.key, "value", raw)
		}
		if !shouldUpdate {
			continue
		}
		if cronValue == "" || cronValue == raw {
			continue
		}
		if err := updateSettingFunc(ctx, item.key, cronValue); err != nil {
			slog.WarnContext(ctx, "Failed to migrate scheduler setting", "key", item.key, "error", err.Error())
			continue
		}
		slog.InfoContext(ctx, "Migrated scheduler setting to cron", "key", item.key, "value", cronValue)
		changed = true
	}

	if changed && reloadFunc != nil {
		if err := reloadFunc(ctx); err != nil {
			slog.WarnContext(ctx, "Failed to reload settings after scheduler migration", "error", err.Error())
		}
	}
}

func MigrateGitOpsSyncIntervals(
	ctx context.Context,
	listFunc func(context.Context) ([]IntervalMigrationItem, error),
	updateFunc func(context.Context, string, int) error,
) {
	if listFunc == nil || updateFunc == nil {
		return
	}

	items, err := listFunc(ctx)
	if err != nil {
		slog.WarnContext(ctx, "Failed to load git sync intervals for migration", "error", err.Error())
		return
	}

	for _, item := range items {
		raw := strings.TrimSpace(item.RawValue)
		if raw == "" {
			continue
		}
		minutes, shouldUpdate, warn := normalizeSchedulerValueToMinutes(raw, time.Minute)
		if warn != "" {
			slog.WarnContext(ctx, warn, "syncId", item.ID, "value", raw)
		}
		if !shouldUpdate || minutes <= 0 {
			continue
		}
		if err := updateFunc(ctx, item.ID, minutes); err != nil {
			slog.WarnContext(ctx, "Failed to migrate git sync interval", "syncId", item.ID, "error", err.Error())
			continue
		}
		slog.InfoContext(ctx, "Migrated git sync interval", "syncId", item.ID, "minutes", minutes)
	}
}

func normalizeSchedulerValueToCron(raw string, defaultUnit time.Duration) (string, bool, string) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", false, ""
	}

	if strings.HasPrefix(trimmed, "@") {
		return trimmed, false, ""
	}

	if strings.HasPrefix(trimmed, "CRON_TZ=") || strings.HasPrefix(trimmed, "TZ=") {
		parts := strings.Fields(trimmed)
		if len(parts) == 6 {
			return parts[0] + " 0 " + strings.Join(parts[1:], " "), true, ""
		}
		return trimmed, false, ""
	}

	fields := strings.Fields(trimmed)
	if len(fields) == 5 {
		return "0 " + trimmed, true, ""
	}
	if len(fields) >= 6 {
		return trimmed, false, ""
	}

	duration, ok := parseSchedulerDuration(trimmed, defaultUnit)
	if !ok {
		return "", false, ""
	}

	cronValue, warn := durationToCron(duration)
	if cronValue == "" {
		return "", false, warn
	}

	return cronValue, true, warn
}

func parseSchedulerDuration(raw string, defaultUnit time.Duration) (time.Duration, bool) {
	if raw == "" {
		return 0, false
	}
	if d, err := time.ParseDuration(raw); err == nil {
		return d, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	if defaultUnit <= 0 {
		defaultUnit = time.Minute
	}
	return time.Duration(value) * defaultUnit, true
}

func durationToCron(d time.Duration) (string, string) {
	if d <= 0 {
		return "", ""
	}

	if d < time.Minute {
		seconds := int(d.Seconds())
		if d%time.Second != 0 {
			seconds++
		}
		if seconds < 1 {
			return "", ""
		}
		if seconds >= 60 {
			minutes := (seconds + 59) / 60
			return minutesToCron(minutes), "scheduler interval rounded up to minutes"
		}
		return fmt.Sprintf("*/%d * * * * *", seconds), ""
	}

	if d%time.Minute != 0 {
		seconds := int(d.Seconds())
		if d%time.Second != 0 {
			seconds++
		}
		minutes := (seconds + 59) / 60
		return minutesToCron(minutes), "scheduler interval rounded up to minutes"
	}

	minutes := int(d.Minutes())
	return minutesToCron(minutes), ""
}

func minutesToCron(minutes int) string {
	if minutes <= 0 {
		return ""
	}
	if minutes < 60 {
		return fmt.Sprintf("0 */%d * * * *", minutes)
	}
	if minutes%60 == 0 {
		hours := minutes / 60
		if hours < 24 {
			return fmt.Sprintf("0 0 */%d * * *", hours)
		}
		if hours%24 == 0 {
			days := hours / 24
			return fmt.Sprintf("0 0 0 */%d * *", days)
		}
	}
	return fmt.Sprintf("0 */%d * * * *", minutes)
}

func normalizeSchedulerValueToMinutes(raw string, defaultUnit time.Duration) (int, bool, string) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, false, ""
	}

	if strings.HasPrefix(trimmed, "@") {
		return 0, false, ""
	}

	if strings.HasPrefix(trimmed, "CRON_TZ=") || strings.HasPrefix(trimmed, "TZ=") {
		parts := strings.Fields(trimmed)
		if len(parts) >= 6 {
			minutes, ok, warn := cronToMinutes(parts[1:])
			if ok {
				return minutes, true, warn
			}
		}
		return 0, false, ""
	}

	fields := strings.Fields(trimmed)
	if len(fields) >= 5 {
		if len(fields) == 5 {
			fields = append([]string{"0"}, fields...)
		}
		minutes, ok, warn := cronToMinutes(fields)
		if ok {
			return minutes, true, warn
		}
		return 0, false, ""
	}

	duration, ok := parseSchedulerDuration(trimmed, defaultUnit)
	if !ok {
		return 0, false, ""
	}

	minutes := int(duration / time.Minute)
	warn := ""
	if duration%time.Minute != 0 {
		minutes++
		warn = "scheduler interval rounded up to minutes"
	}
	if minutes < 1 {
		minutes = 1
		warn = "scheduler interval rounded up to minutes"
	}
	if strconv.Itoa(minutes) == trimmed {
		return minutes, false, warn
	}

	return minutes, true, warn
}

func cronToMinutes(fields []string) (int, bool, string) {
	if len(fields) < 6 {
		return 0, false, ""
	}

	sec, min, hour, day, month, weekday := fields[0], fields[1], fields[2], fields[3], fields[4], fields[5]

	if mins, ok, warn := tryConvertSecondStep(sec, min, hour, day, month, weekday); ok {
		return mins, ok, warn
	}
	if mins, ok, warn := tryConvertMinuteOrHour(sec, min, hour, day, month, weekday); ok {
		return mins, ok, warn
	}
	if mins, ok, warn := tryConvertDayStep(sec, min, hour, day, month, weekday); ok {
		return mins, ok, warn
	}

	return 0, false, ""
}

func tryConvertSecondStep(sec, min, hour, day, month, weekday string) (int, bool, string) {
	step, ok := parseCronStep(sec)
	if !ok {
		return 0, false, ""
	}

	if min == "*" && hour == "*" && day == "*" && month == "*" && weekday == "*" {
		minutes := max((step+59)/60, 1)
		warn := ""
		if step%60 != 0 {
			warn = "scheduler interval rounded up to minutes"
		}
		return minutes, true, warn
	}
	return 0, false, ""
}

func tryConvertMinuteOrHour(sec, min, hour, day, month, weekday string) (int, bool, string) {
	if (sec != "0" && sec != "*") || day != "*" || month != "*" || weekday != "*" {
		return 0, false, ""
	}

	if step, ok := parseCronStep(min); ok && hour == "*" {
		return step, true, ""
	}
	if min == "*" && hour == "*" {
		return 1, true, ""
	}
	if min == "0" {
		if step, ok := parseCronStep(hour); ok && day == "*" {
			return step * 60, true, ""
		}
		if hour == "*" {
			return 60, true, ""
		}
	}
	return 0, false, ""
}

func tryConvertDayStep(sec, min, hour, day, month, weekday string) (int, bool, string) {
	if (sec == "0" || sec == "*") && min == "0" && hour == "0" && month == "*" && weekday == "*" {
		if step, ok := parseCronStep(day); ok {
			return step * 1440, true, ""
		}
	}
	return 0, false, ""
}

func parseCronStep(field string) (int, bool) {
	if !strings.HasPrefix(field, "*/") {
		return 0, false
	}
	step, err := strconv.Atoi(strings.TrimPrefix(field, "*/"))
	if err != nil || step <= 0 {
		return 0, false
	}
	return step, true
}
