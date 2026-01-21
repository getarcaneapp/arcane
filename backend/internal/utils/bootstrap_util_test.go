package utils

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/stretchr/testify/assert"
)

type mockSettingsManager struct {
	persistCalled        bool
	setBoolCalled        bool
	ensureDefaultsCalled bool
	persistErr           error
	setBoolErr           error
	ensureDefaultsErr    error
}

func (m *mockSettingsManager) PersistEnvSettingsIfMissing(ctx context.Context) error {
	m.persistCalled = true
	return m.persistErr
}

func (m *mockSettingsManager) SetBoolSetting(ctx context.Context, key string, value bool) error {
	m.setBoolCalled = true
	return m.setBoolErr
}

func (m *mockSettingsManager) EnsureDefaultSettings(ctx context.Context) error {
	m.ensureDefaultsCalled = true
	return m.ensureDefaultsErr
}

type mockSettingsPruner struct {
	pruneCalled bool
	pruneErr    error
}

func (m *mockSettingsPruner) PruneUnknownSettings(ctx context.Context) error {
	m.pruneCalled = true
	return m.pruneErr
}

func TestLoadAgentToken(t *testing.T) {
	ctx := context.Background()

	t.Run("loads token when agent mode and token empty", func(t *testing.T) {
		cfg := &config.Config{
			AgentMode:  true,
			AgentToken: "",
		}
		getSettingFunc := func(ctx context.Context, key string, def string) string {
			if key == "agentToken" {
				return "test-token-123"
			}
			return def
		}

		LoadAgentToken(ctx, cfg, getSettingFunc)

		assert.Equal(t, "test-token-123", cfg.AgentToken)
	})

	t.Run("does not load when not in agent mode", func(t *testing.T) {
		cfg := &config.Config{
			AgentMode:  false,
			AgentToken: "",
		}
		getSettingFunc := func(ctx context.Context, key string, def string) string {
			return "test-token-123"
		}

		LoadAgentToken(ctx, cfg, getSettingFunc)

		assert.Empty(t, cfg.AgentToken)
	})

	t.Run("does not override existing token", func(t *testing.T) {
		cfg := &config.Config{
			AgentMode:  true,
			AgentToken: "existing-token",
		}
		getSettingFunc := func(ctx context.Context, key string, def string) string {
			return "new-token"
		}

		LoadAgentToken(ctx, cfg, getSettingFunc)

		assert.Equal(t, "existing-token", cfg.AgentToken)
	})

	t.Run("handles empty token from database", func(t *testing.T) {
		cfg := &config.Config{
			AgentMode:  true,
			AgentToken: "",
		}
		getSettingFunc := func(ctx context.Context, key string, def string) string {
			return ""
		}

		LoadAgentToken(ctx, cfg, getSettingFunc)

		assert.Empty(t, cfg.AgentToken)
	})
}

func TestEnsureEncryptionKey(t *testing.T) {
	ctx := context.Background()

	t.Run("sets key in agent mode", func(t *testing.T) {
		cfg := &config.Config{
			AgentMode:     true,
			EncryptionKey: "",
		}
		ensureKeyFunc := func(ctx context.Context) (string, error) {
			return "generated-key", nil
		}

		EnsureEncryptionKey(ctx, cfg, ensureKeyFunc)

		assert.Equal(t, "generated-key", cfg.EncryptionKey)
	})

	t.Run("sets key in non-production", func(t *testing.T) {
		cfg := &config.Config{
			AgentMode:     false,
			Environment:   "development",
			EncryptionKey: "",
		}
		ensureKeyFunc := func(ctx context.Context) (string, error) {
			return "dev-key", nil
		}

		EnsureEncryptionKey(ctx, cfg, ensureKeyFunc)

		assert.Equal(t, "dev-key", cfg.EncryptionKey)
	})

	t.Run("does not set key in production when not agent", func(t *testing.T) {
		cfg := &config.Config{
			AgentMode:     false,
			Environment:   "production",
			EncryptionKey: "",
		}
		ensureKeyFunc := func(ctx context.Context) (string, error) {
			return "should-not-set", nil
		}

		EnsureEncryptionKey(ctx, cfg, ensureKeyFunc)

		assert.Empty(t, cfg.EncryptionKey)
	})

	t.Run("handles error gracefully", func(t *testing.T) {
		cfg := &config.Config{
			AgentMode:     true,
			EncryptionKey: "",
		}
		ensureKeyFunc := func(ctx context.Context) (string, error) {
			return "", errors.New("key generation failed")
		}

		EnsureEncryptionKey(ctx, cfg, ensureKeyFunc)

		assert.Empty(t, cfg.EncryptionKey)
	})
}

func TestInitializeDefaultSettings(t *testing.T) {
	ctx := context.Background()
	cfg := &config.Config{}

	t.Run("calls all initialization methods", func(t *testing.T) {
		mgr := &mockSettingsManager{}

		InitializeDefaultSettings(ctx, cfg, mgr)

		assert.True(t, mgr.ensureDefaultsCalled)
		assert.True(t, mgr.persistCalled)
	})

	t.Run("handles ensure defaults error", func(t *testing.T) {
		mgr := &mockSettingsManager{
			ensureDefaultsErr: errors.New("defaults failed"),
		}

		InitializeDefaultSettings(ctx, cfg, mgr)

		assert.True(t, mgr.ensureDefaultsCalled)
		assert.True(t, mgr.persistCalled)
	})

	t.Run("handles persist error", func(t *testing.T) {
		mgr := &mockSettingsManager{
			persistErr: errors.New("persist failed"),
		}

		InitializeDefaultSettings(ctx, cfg, mgr)

		assert.True(t, mgr.ensureDefaultsCalled)
		assert.True(t, mgr.persistCalled)
	})
}

func TestCleanupUnknownSettings(t *testing.T) {
	ctx := context.Background()

	t.Run("calls prune method", func(t *testing.T) {
		pruner := &mockSettingsPruner{}

		CleanupUnknownSettings(ctx, pruner)

		assert.True(t, pruner.pruneCalled)
	})

	t.Run("handles error gracefully", func(t *testing.T) {
		pruner := &mockSettingsPruner{
			pruneErr: errors.New("prune failed"),
		}

		CleanupUnknownSettings(ctx, pruner)

		assert.True(t, pruner.pruneCalled)
	})
}

func TestTestDockerConnection(t *testing.T) {
	ctx := context.Background()

	t.Run("executes test function", func(t *testing.T) {
		called := false
		testFunc := func(ctx context.Context) error {
			called = true
			return nil
		}

		TestDockerConnection(ctx, testFunc)

		assert.True(t, called)
	})

	t.Run("handles error gracefully", func(t *testing.T) {
		testFunc := func(ctx context.Context) error {
			return errors.New("docker connection failed")
		}

		TestDockerConnection(ctx, testFunc)
	})
}

func TestInitializeNonAgentFeatures(t *testing.T) {
	ctx := context.Background()

	t.Run("skips in agent mode", func(t *testing.T) {
		cfg := &config.Config{AgentMode: true}
		createAdminCalled := false
		createAdminFunc := func(ctx context.Context) error {
			createAdminCalled = true
			return nil
		}

		InitializeNonAgentFeatures(ctx, cfg, createAdminFunc, nil, nil)

		assert.False(t, createAdminCalled)
	})

	t.Run("calls all functions in non-agent mode", func(t *testing.T) {
		cfg := &config.Config{AgentMode: false}
		createAdminCalled := false
		migrateOidcCalled := false
		migrateDiscordCalled := false

		createAdminFunc := func(ctx context.Context) error {
			createAdminCalled = true
			return nil
		}
		migrateOidcFunc := func(ctx context.Context) error {
			migrateOidcCalled = true
			return nil
		}
		migrateDiscordFunc := func(ctx context.Context) error {
			migrateDiscordCalled = true
			return nil
		}

		InitializeNonAgentFeatures(ctx, cfg, createAdminFunc, migrateOidcFunc, migrateDiscordFunc)

		assert.True(t, createAdminCalled)
		assert.True(t, migrateOidcCalled)
		assert.True(t, migrateDiscordCalled)
	})

	t.Run("handles errors gracefully", func(t *testing.T) {
		cfg := &config.Config{AgentMode: false}
		createAdminFunc := func(ctx context.Context) error {
			return errors.New("admin creation failed")
		}
		migrateOidcFunc := func(ctx context.Context) error {
			return errors.New("oidc migration failed")
		}
		migrateDiscordFunc := func(ctx context.Context) error {
			return errors.New("discord migration failed")
		}

		InitializeNonAgentFeatures(ctx, cfg, createAdminFunc, migrateOidcFunc, migrateDiscordFunc)
	})
}

func TestMigrateSchedulerCronValues(t *testing.T) {
	ctx := context.Background()

	t.Run("migrates minute-based intervals to cron", func(t *testing.T) {
		settings := map[string]string{
			"pollingInterval": "15",
		}
		updateCalled := false

		getSettingFunc := func(ctx context.Context, key string, def string) string {
			if val, ok := settings[key]; ok {
				return val
			}
			return def
		}
		updateSettingFunc := func(ctx context.Context, key string, value string) error {
			updateCalled = true
			assert.Equal(t, "pollingInterval", key)
			assert.Equal(t, "0 */15 * * * *", value)
			return nil
		}
		reloadFunc := func(ctx context.Context) error {
			return nil
		}

		MigrateSchedulerCronValues(ctx, getSettingFunc, updateSettingFunc, reloadFunc)

		assert.True(t, updateCalled)
	})

	t.Run("migrates duration strings", func(t *testing.T) {
		settings := map[string]string{
			"autoUpdateInterval": "30m",
		}
		updateCalled := false

		getSettingFunc := func(ctx context.Context, key string, def string) string {
			if val, ok := settings[key]; ok {
				return val
			}
			return def
		}
		updateSettingFunc := func(ctx context.Context, key string, value string) error {
			updateCalled = true
			assert.Equal(t, "autoUpdateInterval", key)
			assert.Equal(t, "0 */30 * * * *", value)
			return nil
		}
		reloadFunc := func(ctx context.Context) error {
			return nil
		}

		MigrateSchedulerCronValues(ctx, getSettingFunc, updateSettingFunc, reloadFunc)

		assert.True(t, updateCalled)
	})

	t.Run("skips already valid cron expressions", func(t *testing.T) {
		settings := map[string]string{
			"pollingInterval": "0 */15 * * * *",
		}
		updateCalled := false

		getSettingFunc := func(ctx context.Context, key string, def string) string {
			if val, ok := settings[key]; ok {
				return val
			}
			return def
		}
		updateSettingFunc := func(ctx context.Context, key string, value string) error {
			updateCalled = true
			return nil
		}
		reloadFunc := func(ctx context.Context) error {
			return nil
		}

		MigrateSchedulerCronValues(ctx, getSettingFunc, updateSettingFunc, reloadFunc)

		assert.False(t, updateCalled)
	})

	t.Run("handles update errors", func(t *testing.T) {
		settings := map[string]string{
			"pollingInterval": "15",
		}

		getSettingFunc := func(ctx context.Context, key string, def string) string {
			if val, ok := settings[key]; ok {
				return val
			}
			return def
		}
		updateSettingFunc := func(ctx context.Context, key string, value string) error {
			return errors.New("update failed")
		}
		reloadFunc := func(ctx context.Context) error {
			return nil
		}

		MigrateSchedulerCronValues(ctx, getSettingFunc, updateSettingFunc, reloadFunc)
	})
}

func TestMigrateGitOpsSyncIntervals(t *testing.T) {
	ctx := context.Background()

	t.Run("migrates intervals to minutes", func(t *testing.T) {
		items := []IntervalMigrationItem{
			{ID: "sync-1", RawValue: "30"},
			{ID: "sync-2", RawValue: "1h"},
		}
		updates := make(map[string]int)

		listFunc := func(ctx context.Context) ([]IntervalMigrationItem, error) {
			return items, nil
		}
		updateFunc := func(ctx context.Context, id string, minutes int) error {
			updates[id] = minutes
			return nil
		}

		MigrateGitOpsSyncIntervals(ctx, listFunc, updateFunc)

		assert.Len(t, updates, 1)
		assert.Equal(t, 60, updates["sync-2"])
	})

	t.Run("handles list error", func(t *testing.T) {
		listFunc := func(ctx context.Context) ([]IntervalMigrationItem, error) {
			return nil, errors.New("list failed")
		}
		updateFunc := func(ctx context.Context, id string, minutes int) error {
			return nil
		}

		MigrateGitOpsSyncIntervals(ctx, listFunc, updateFunc)
	})

	t.Run("handles nil functions", func(t *testing.T) {
		MigrateGitOpsSyncIntervals(ctx, nil, nil)
	})
}

func TestParseSchedulerDuration(t *testing.T) {
	tests := []struct {
		name         string
		raw          string
		defaultUnit  time.Duration
		wantDuration time.Duration
		wantOk       bool
	}{
		{"empty string", "", time.Minute, 0, false},
		{"valid duration string", "30m", time.Minute, 30 * time.Minute, true},
		{"integer with default minute", "15", time.Minute, 15 * time.Minute, true},
		{"integer with default hour", "2", time.Hour, 2 * time.Hour, true},
		{"seconds duration", "45s", time.Minute, 45 * time.Second, true},
		{"hours duration", "2h", time.Minute, 2 * time.Hour, true},
		{"invalid format", "abc", time.Minute, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotDuration, gotOk := parseSchedulerDuration(tt.raw, tt.defaultUnit)
			assert.Equal(t, tt.wantOk, gotOk)
			if gotOk {
				assert.Equal(t, tt.wantDuration, gotDuration)
			}
		})
	}
}

func TestDurationToCron(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		wantCron string
		wantWarn string
	}{
		{"zero duration", 0, "", ""},
		{"negative duration", -1 * time.Minute, "", ""},
		{"30 seconds", 30 * time.Second, "*/30 * * * * *", ""},
		{"1 minute", time.Minute, "0 */1 * * * *", ""},
		{"15 minutes", 15 * time.Minute, "0 */15 * * * *", ""},
		{"1 hour", time.Hour, "0 0 */1 * * *", ""},
		{"24 hours", 24 * time.Hour, "0 0 0 */1 * *", ""},
		{"45 seconds rounds up", 45 * time.Second, "*/45 * * * * *", ""},
		{"90 seconds rounds to 2 min", 90 * time.Second, "0 */2 * * * *", "scheduler interval rounded up to minutes"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotCron, gotWarn := durationToCron(tt.duration)
			assert.Equal(t, tt.wantCron, gotCron)
			if tt.wantWarn != "" {
				assert.Contains(t, gotWarn, tt.wantWarn)
			}
		})
	}
}

func TestMinutesToCron(t *testing.T) {
	tests := []struct {
		name     string
		minutes  int
		wantCron string
	}{
		{"zero minutes", 0, ""},
		{"negative minutes", -1, ""},
		{"15 minutes", 15, "0 */15 * * * *"},
		{"30 minutes", 30, "0 */30 * * * *"},
		{"60 minutes", 60, "0 0 */1 * * *"},
		{"120 minutes", 120, "0 0 */2 * * *"},
		{"1440 minutes (1 day)", 1440, "0 0 0 */1 * *"},
		{"2880 minutes (2 days)", 2880, "0 0 0 */2 * *"},
		{"90 minutes", 90, "0 */90 * * * *"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotCron := minutesToCron(tt.minutes)
			assert.Equal(t, tt.wantCron, gotCron)
		})
	}
}

func TestNormalizeSchedulerValueToCron(t *testing.T) {
	tests := []struct {
		name             string
		raw              string
		defaultUnit      time.Duration
		wantCron         string
		wantShouldUpdate bool
	}{
		{"empty string", "", time.Minute, "", false},
		{"@hourly special", "@hourly", time.Minute, "@hourly", false},
		{"valid 6-field cron", "0 */15 * * * *", time.Minute, "0 */15 * * * *", false},
		{"5-field cron adds second", "*/15 * * * *", time.Minute, "0 */15 * * * *", true},
		{"integer minutes", "30", time.Minute, "0 */30 * * * *", true},
		{"duration string", "1h", time.Minute, "0 0 */1 * * *", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotCron, gotShouldUpdate, _ := normalizeSchedulerValueToCron(tt.raw, tt.defaultUnit)
			assert.Equal(t, tt.wantCron, gotCron)
			assert.Equal(t, tt.wantShouldUpdate, gotShouldUpdate)
		})
	}
}

func TestNormalizeSchedulerValueToMinutes(t *testing.T) {
	tests := []struct {
		name             string
		raw              string
		defaultUnit      time.Duration
		wantMinutes      int
		wantShouldUpdate bool
	}{
		{"empty string", "", time.Minute, 0, false},
		{"@hourly special", "@hourly", time.Minute, 0, false},
		{"integer already minutes", "30", time.Minute, 30, false},
		{"duration string", "1h", time.Minute, 60, true},
		{"cron every hour", "0 0 * * * *", time.Minute, 60, true},
		{"cron every 15 min", "0 */15 * * * *", time.Minute, 15, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMinutes, gotShouldUpdate, _ := normalizeSchedulerValueToMinutes(tt.raw, tt.defaultUnit)
			assert.Equal(t, tt.wantMinutes, gotMinutes)
			assert.Equal(t, tt.wantShouldUpdate, gotShouldUpdate)
		})
	}
}

func TestCronToMinutes(t *testing.T) {
	tests := []struct {
		name        string
		fields      []string
		wantMinutes int
		wantOk      bool
	}{
		{"every 30 seconds", []string{"*/30", "*", "*", "*", "*", "*"}, 1, true},
		{"every 15 minutes", []string{"0", "*/15", "*", "*", "*", "*"}, 15, true},
		{"every hour", []string{"0", "0", "*", "*", "*", "*"}, 60, true},
		{"every 2 hours", []string{"0", "0", "*/2", "*", "*", "*"}, 120, true},
		{"every day", []string{"0", "0", "0", "*/1", "*", "*"}, 1440, true},
		{"too few fields", []string{"0", "*/15", "*", "*", "*"}, 0, false},
		{"complex pattern", []string{"0", "15", "*/2", "*", "*", "*"}, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMinutes, gotOk, _ := cronToMinutes(tt.fields)
			assert.Equal(t, tt.wantOk, gotOk)
			if gotOk {
				assert.Equal(t, tt.wantMinutes, gotMinutes)
			}
		})
	}
}

func TestParseCronStep(t *testing.T) {
	tests := []struct {
		name     string
		field    string
		wantStep int
		wantOk   bool
	}{
		{"valid step", "*/15", 15, true},
		{"no prefix", "15", 0, false},
		{"zero step", "*/0", 0, false},
		{"negative step", "*/-5", 0, false},
		{"invalid number", "*/abc", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotStep, gotOk := parseCronStep(tt.field)
			assert.Equal(t, tt.wantOk, gotOk)
			if gotOk {
				assert.Equal(t, tt.wantStep, gotStep)
			}
		})
	}
}

func TestTryConvertSecondStep(t *testing.T) {
	tests := []struct {
		name        string
		sec         string
		min         string
		hour        string
		day         string
		month       string
		weekday     string
		wantMinutes int
		wantOk      bool
	}{
		{"every 30 seconds", "*/30", "*", "*", "*", "*", "*", 1, true},
		{"every 60 seconds", "*/60", "*", "*", "*", "*", "*", 1, true},
		{"every 90 seconds", "*/90", "*", "*", "*", "*", "*", 2, true},
		{"not wildcard minutes", "*/30", "0", "*", "*", "*", "*", 0, false},
		{"not step pattern", "0", "*", "*", "*", "*", "*", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMinutes, gotOk, _ := tryConvertSecondStep(tt.sec, tt.min, tt.hour, tt.day, tt.month, tt.weekday)
			assert.Equal(t, tt.wantOk, gotOk)
			if gotOk {
				assert.Equal(t, tt.wantMinutes, gotMinutes)
			}
		})
	}
}

func TestTryConvertMinuteOrHour(t *testing.T) {
	tests := []struct {
		name        string
		sec         string
		min         string
		hour        string
		day         string
		month       string
		weekday     string
		wantMinutes int
		wantOk      bool
	}{
		{"every 15 minutes", "0", "*/15", "*", "*", "*", "*", 15, true},
		{"every hour", "0", "0", "*", "*", "*", "*", 60, true},
		{"every 2 hours", "0", "0", "*/2", "*", "*", "*", 120, true},
		{"complex pattern", "0", "30", "*/2", "*", "*", "*", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMinutes, gotOk, _ := tryConvertMinuteOrHour(tt.sec, tt.min, tt.hour, tt.day, tt.month, tt.weekday)
			assert.Equal(t, tt.wantOk, gotOk)
			if gotOk {
				assert.Equal(t, tt.wantMinutes, gotMinutes)
			}
		})
	}
}

func TestTryConvertDayStep(t *testing.T) {
	tests := []struct {
		name        string
		sec         string
		min         string
		hour        string
		day         string
		month       string
		weekday     string
		wantMinutes int
		wantOk      bool
	}{
		{"every day", "0", "0", "0", "*/1", "*", "*", 1440, true},
		{"every 2 days", "0", "0", "0", "*/2", "*", "*", 2880, true},
		{"not midnight", "0", "0", "1", "*/1", "*", "*", 0, false},
		{"not step pattern", "0", "0", "0", "1", "*", "*", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMinutes, gotOk, _ := tryConvertDayStep(tt.sec, tt.min, tt.hour, tt.day, tt.month, tt.weekday)
			assert.Equal(t, tt.wantOk, gotOk)
			if gotOk {
				assert.Equal(t, tt.wantMinutes, gotMinutes)
			}
		})
	}
}
