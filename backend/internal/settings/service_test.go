package settings

import (
	"context"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx/fxtest"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/actors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	settingstypes "github.com/getarcaneapp/arcane/types/v2/settings"
)

func setupSettingsTestDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&SettingVariable{}))
	return &database.DB{DB: db}
}

func newSettingsServiceForTestInternal(t testing.TB, ctx context.Context, db *database.DB) (*SettingsService, error) {
	t.Helper()
	lifecycle := fxtest.NewLifecycle(t)
	runtime, err := actors.NewRuntime(t.Context(), lifecycle)
	require.NoError(t, err)
	executor, err := actors.NewExecutor(t.Context(), runtime, "settings-test", t.Name(), 3)
	require.NoError(t, err)
	effects, err := actors.NewExecutor(t.Context(), runtime, "settings-effects-test", t.Name(), 3)
	require.NoError(t, err)
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, executor.Stop(stopCtx))
		require.NoError(t, effects.Stop(stopCtx))
		require.NoError(t, lifecycle.Stop(stopCtx))
	})
	return NewSettingsService(ctx, db, executor, effects)
}

func newAdmissionGateForTestInternal(t testing.TB) *actors.Gate[actors.AdmissionKey] {
	t.Helper()
	lifecycle := fxtest.NewLifecycle(t)
	runtime, err := actors.NewRuntime(t.Context(), lifecycle)
	require.NoError(t, err)
	gate, err := actors.NewGate[actors.AdmissionKey](t.Context(), runtime, "services-test-admission", t.Name())
	require.NoError(t, err)
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, gate.Stop(stopCtx))
		require.NoError(t, lifecycle.Stop(stopCtx))
	})
	return gate
}

func waitForSettingsNotificationsInternal(t *testing.T, svc *SettingsService) {
	t.Helper()
	_, err := svc.writes.Execute(t.Context(), "wait for settings notification submission", func(context.Context) (actors.NoPayload, error) {
		return actors.NoPayload{}, nil
	}, nil)
	require.NoError(t, err)
	_, err = svc.effects.Execute(t.Context(), "wait for settings notifications", func(context.Context) (actors.NoPayload, error) {
		return actors.NoPayload{}, nil
	}, nil)
	require.NoError(t, err)
}

func TestSettingsService_EnsureDefaultSettings_Idempotent(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	var count1 int64
	require.NoError(t, svc.db.WithContext(ctx).Model(&SettingVariable{}).Count(&count1).Error)
	require.Positive(t, count1)

	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	var count2 int64
	require.NoError(t, svc.db.WithContext(ctx).Model(&SettingVariable{}).Count(&count2).Error)
	require.Equal(t, count1, count2)

	// Spot-check core and automation defaults exist with correct values
	for _, key := range []string{"authLocalEnabled", "projectsDirectory", "followProjectSymlinks", "autoUpdateExcludedContainers", "imageEventWatcherEnabled", "vulnerabilityScanEnabled", "vulnerabilityScanInterval", "trivyImage", "trivyNetwork", "trivySecurityOpts", "trivyPrivileged", "trivyPreserveCacheOnVolumePrune", "trivyResourceLimitsEnabled", "trivyCpuLimit", "trivyMemoryLimitMb", "trivyConcurrentScanContainers", "gitSyncMaxFiles", "gitSyncMaxTotalSizeMb", "gitSyncMaxBinarySizeMb", "lifecycleDefaultRunnerImage"} {
		var sv SettingVariable
		err := svc.db.WithContext(ctx).Where("key = ?", key).First(&sv).Error
		require.NoErrorf(t, err, "missing default key %s", key)

		switch key {
		case "followProjectSymlinks":
			require.Equal(t, "false", sv.Value)
		case "autoUpdateExcludedContainers":
			require.Empty(t, sv.Value)
		case "imageEventWatcherEnabled":
			require.Equal(t, "false", sv.Value)
		case "vulnerabilityScanEnabled":
			require.Equal(t, "false", sv.Value)
		case "vulnerabilityScanInterval":
			require.Equal(t, "0 0 0 * * *", sv.Value)
		case "trivyImage":
			require.Equal(t, DefaultTrivyImage, sv.Value)
		case "lifecycleDefaultRunnerImage":
			require.Equal(t, "alpine:latest", sv.Value)
		case "trivyNetwork":
			require.Empty(t, sv.Value)
		case "trivySecurityOpts":
			require.Empty(t, sv.Value)
		case "trivyPrivileged":
			require.Equal(t, "false", sv.Value)
		case "trivyPreserveCacheOnVolumePrune":
			require.Equal(t, "true", sv.Value)
		case "trivyResourceLimitsEnabled":
			require.Equal(t, "true", sv.Value)
		case "trivyCpuLimit":
			require.Equal(t, "1", sv.Value)
		case "trivyMemoryLimitMb":
			require.Equal(t, "0", sv.Value)
		case "trivyConcurrentScanContainers":
			require.Equal(t, "1", sv.Value)
		case "gitSyncMaxFiles":
			require.Equal(t, "500", sv.Value)
		case "gitSyncMaxTotalSizeMb":
			require.Equal(t, "50", sv.Value)
		case "gitSyncMaxBinarySizeMb":
			require.Equal(t, "10", sv.Value)
		}
	}
}

func TestSettingsService_ImageEventWatcherSettingPersists(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))
	require.False(t, svc.GetBoolSetting(ctx, "imageEventWatcherEnabled", true))

	_, err = svc.UpdateSettings(ctx, settingstypes.Update{ImageEventWatcherEnabled: new("true")})
	require.NoError(t, err)
	require.True(t, svc.GetBoolSetting(ctx, "imageEventWatcherEnabled", false))

	var stored SettingVariable
	require.NoError(t, db.WithContext(ctx).Where("key = ?", "imageEventWatcherEnabled").First(&stored).Error)
	require.Equal(t, "true", stored.Value)
}

func TestSettingsService_EnsureDefaultSettings_OverridesExistingTrivyImage(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	require.NoError(t, svc.UpdateSetting(ctx, "trivyImage", "ghcr.io/aquasecurity/trivy:latest"))
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	var sv SettingVariable
	require.NoError(t, svc.db.WithContext(ctx).Where("key = ?", "trivyImage").First(&sv).Error)
	require.Equal(t, DefaultTrivyImage, sv.Value)
}

func TestSettingsService_GetSettings_UnknownKeysIgnored(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	require.NoError(t, svc.db.WithContext(ctx).
		Create(&SettingVariable{Key: "someUnknownKey", Value: "x"}).Error)

	_, err = svc.GetSettings(ctx)
	require.NoError(t, err)
}

func TestSettingsService_GetSettings_UsesCachedSnapshotWithoutDatabase(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	require.NoError(t, svc.SetStringSetting(ctx, "baseServerUrl", "http://cached"))

	// GetSettings should clone the in-memory snapshot and not touch the database.
	svc.db = nil

	settingsCfg, err := svc.GetSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, "http://cached", settingsCfg.BaseServerURL.Value)
}

func TestSettingsService_AvatarMaxUploadSizeDefaultAndUpdate(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	current, err := svc.GetSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, "2", current.AvatarMaxUploadSizeMb.Value)

	updatedValue := "8"
	_, err = svc.UpdateSettings(ctx, settingstypes.Update{
		AvatarMaxUploadSizeMb: &updatedValue,
	})
	require.NoError(t, err)

	current, err = svc.GetSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, "8", current.AvatarMaxUploadSizeMb.Value)
}

func TestSettingsServiceUpdateSettingsRejectsOIDCIssuerChangeWithStoredSecretInternal(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.UpdateSetting(ctx, "oidcIssuerUrl", "https://issuer.example.com"))
	require.NoError(t, svc.UpdateSetting(ctx, "oidcClientSecret", "old-client-secret"))

	_, err = svc.UpdateSettings(ctx, settingstypes.Update{
		OidcIssuerUrl:    new("https://attacker.example.com"),
		OidcClientSecret: new(""),
	})
	require.ErrorIs(t, err, common.ErrValidation)

	current, loadErr := svc.GetSettings(ctx)
	require.NoError(t, loadErr)
	require.Equal(t, "https://issuer.example.com", current.OidcIssuerUrl.Value)
	require.Equal(t, "old-client-secret", current.OidcClientSecret.Value)
}

func TestSettingsServiceUpdateSettingsAllowsOIDCIssuerChangeWithReplacementSecretInternal(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.UpdateSetting(ctx, "oidcIssuerUrl", "https://issuer.example.com"))
	require.NoError(t, svc.UpdateSetting(ctx, "oidcClientSecret", "old-client-secret"))

	_, err = svc.UpdateSettings(ctx, settingstypes.Update{
		OidcIssuerUrl:    new("https://replacement.example.com"),
		OidcClientSecret: new("new-client-secret"),
	})
	require.NoError(t, err)

	current, loadErr := svc.GetSettings(ctx)
	require.NoError(t, loadErr)
	require.Equal(t, "https://replacement.example.com", current.OidcIssuerUrl.Value)
	require.Equal(t, "new-client-secret", current.OidcClientSecret.Value)
}

func TestSettingsServiceUpdateSettingsRejectsTrivyServerChangeWithStoredTokenInternal(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.UpdateSetting(ctx, "trivyServerUrl", "https://trivy.example.com"))
	require.NoError(t, svc.UpdateSetting(ctx, "trivyServerToken", "old-trivy-token"))

	_, err = svc.UpdateSettings(ctx, settingstypes.Update{
		TrivyServerUrl: new("https://attacker.example.com"),
	})
	require.ErrorIs(t, err, common.ErrValidation)

	current, loadErr := svc.GetSettings(ctx)
	require.NoError(t, loadErr)
	require.Equal(t, "https://trivy.example.com", current.TrivyServerUrl.Value)
	require.Equal(t, "old-trivy-token", current.TrivyServerToken.Value)
}

func TestSettingsServiceUpdateSettingsAllowsTrivyServerChangeWithReplacementTokenInternal(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.UpdateSetting(ctx, "trivyServerUrl", "https://trivy.example.com"))
	require.NoError(t, svc.UpdateSetting(ctx, "trivyServerToken", "old-trivy-token"))

	_, err = svc.UpdateSettings(ctx, settingstypes.Update{
		TrivyServerUrl:   new("https://replacement.example.com"),
		TrivyServerToken: new("new-trivy-token"),
	})
	require.NoError(t, err)

	current, loadErr := svc.GetSettings(ctx)
	require.NoError(t, loadErr)
	require.Equal(t, "https://replacement.example.com", current.TrivyServerUrl.Value)
	require.Equal(t, "new-trivy-token", current.TrivyServerToken.Value)
}

func TestSettingsServiceUpdateSettingsAllowsClearingTrivyServerWithStoredTokenInternal(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.UpdateSetting(ctx, "trivyServerUrl", "https://trivy.example.com"))
	require.NoError(t, svc.UpdateSetting(ctx, "trivyServerToken", "old-trivy-token"))

	_, err = svc.UpdateSettings(ctx, settingstypes.Update{
		TrivyServerUrl: new(""),
	})
	require.NoError(t, err)

	current, loadErr := svc.GetSettings(ctx)
	require.NoError(t, loadErr)
	require.Empty(t, current.TrivyServerUrl.Value)
	require.Equal(t, "old-trivy-token", current.TrivyServerToken.Value)
}

func TestSettingsServiceUpdateSettingsAllowsTrivyServerChangeWithoutStoredTokenInternal(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.UpdateSetting(ctx, "trivyServerUrl", "https://trivy.example.com"))

	_, err = svc.UpdateSettings(ctx, settingstypes.Update{
		TrivyServerUrl: new("https://replacement.example.com"),
	})
	require.NoError(t, err)

	current, loadErr := svc.GetSettings(ctx)
	require.NoError(t, loadErr)
	require.Equal(t, "https://replacement.example.com", current.TrivyServerUrl.Value)
}

func TestSettingsService_PruneUnknownSettings_RemovesStaleKeys(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	require.NoError(t, svc.UpdateSetting(ctx, "projectsDirectory", "/tmp/projects"))
	require.NoError(t, svc.UpdateSetting(ctx, "encryptionKey", "test-encryption-key"))
	require.NoError(t, svc.UpdateSetting(ctx, "unknownKey", "value"))

	require.NoError(t, svc.PruneUnknownSettings(ctx))

	var sv SettingVariable
	err = svc.db.WithContext(ctx).Where("key = ?", "unknownKey").First(&sv).Error
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)

	var sv2 SettingVariable
	err = svc.db.WithContext(ctx).Where("key = ?", "projectsDirectory").First(&sv2).Error
	require.NoError(t, err)

	var sv3 SettingVariable
	err = svc.db.WithContext(ctx).Where("key = ?", "encryptionKey").First(&sv3).Error
	require.NoError(t, err)
}

func TestSettingsService_GetSettings_EnvOverride_OidcMergeAccounts(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	t.Setenv("OIDC_MERGE_ACCOUNTS", "true")

	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	settingsCfg, err := svc.GetSettings(ctx)
	require.NoError(t, err)
	require.True(t, settingsCfg.OidcMergeAccounts.IsTrue())
}

func TestSettingsService_GetSettings_EnvOverride_TrivyScanTimeout(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	t.Setenv("TRIVY_SCAN_TIMEOUT", "1800")

	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	settingsCfg, err := svc.GetSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, 1800, settingsCfg.TrivyScanTimeout.AsInt())
}

func TestSettingsService_GetSettings_EnvOverride_TrivyResourceLimits(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	t.Setenv("TRIVY_RESOURCE_LIMITS_ENABLED", "false")
	t.Setenv("TRIVY_CPU_LIMIT", "2.5")
	t.Setenv("TRIVY_MEMORY_LIMIT_MB", "2048")

	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	settingsCfg, err := svc.GetSettings(ctx)
	require.NoError(t, err)
	require.False(t, settingsCfg.TrivyResourceLimitsEnabled.IsTrue())
	require.Equal(t, "2.5", settingsCfg.TrivyCpuLimit.Value)
	require.Equal(t, 2048, settingsCfg.TrivyMemoryLimitMb.AsInt())
}

func TestSettingsService_GetSettings_EnvOverride_TrivyNetwork(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	t.Setenv("TRIVY_NETWORK", "arcane-external")

	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	settingsCfg, err := svc.GetSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, "arcane-external", settingsCfg.TrivyNetwork.Value)
}

func TestSettingsService_GetSettings_EnvOverride_FollowProjectSymlinks(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	t.Setenv("FOLLOW_PROJECT_SYMLINKS", "true")

	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	settingsCfg, err := svc.GetSettings(ctx)
	require.NoError(t, err)
	require.True(t, settingsCfg.FollowProjectSymlinks.IsTrue())
}

func TestSettingsService_GetSettings_EnvOverride_TrivyRuntimeSecurity(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	t.Setenv("TRIVY_SECURITY_OPTS", "label=disable,\nlabel=type:container_runtime_t")
	t.Setenv("TRIVY_PRIVILEGED", "true")

	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	settingsCfg, err := svc.GetSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, "label=disable,\nlabel=type:container_runtime_t", settingsCfg.TrivySecurityOpts.Value)
	require.True(t, settingsCfg.TrivyPrivileged.IsTrue())
}

func TestSettingsService_GetStringSetting_EnvOverride_SwarmStackSourcesDirectory(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	t.Setenv("SWARM_STACK_SOURCES_DIRECTORY", "/mnt/swarm-from-env")

	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.UpdateSetting(ctx, "swarmStackSourcesDirectory", "/tmp/swarm-from-db"))

	require.Equal(t, "/mnt/swarm-from-env", svc.GetStringSetting(ctx, "swarmStackSourcesDirectory", "/fallback"))
	require.Equal(t, "/mnt/swarm-from-env", svc.GetSettingsConfig().SwarmStackSourcesDirectory.Value)
}

func TestSettingsService_isEnvOverrideActiveInternal(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)

	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.False(t, svc.IsEnvOverrideActive("oidcEnabled"))

	t.Setenv("OIDC_ENABLED", "false")
	svcWithOverride, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.True(t, svcWithOverride.IsEnvOverrideActive("oidcEnabled"))

	t.Setenv("AUTH_SESSION_TIMEOUT", "120")
	svcWithNonOverrideEnv, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.False(t, svcWithNonOverrideEnv.IsEnvOverrideActive("authSessionTimeout"))
}

func TestSettingsService_GetSetHelpers(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	// Defaults for missing keys
	require.True(t, svc.GetBoolSetting(ctx, "nonexistentBool", true))
	require.Equal(t, 42, svc.GetIntSetting(ctx, "nonexistentInt", 42))
	require.Equal(t, "def", svc.GetStringSetting(ctx, "nonexistentStr", "def"))

	// Set and read back
	require.NoError(t, svc.SetBoolSetting(ctx, "enableGravatar", true))
	require.True(t, svc.GetBoolSetting(ctx, "enableGravatar", false))

	require.NoError(t, svc.SetIntSetting(ctx, "authSessionTimeout", 123))
	require.Equal(t, 123, svc.GetIntSetting(ctx, "authSessionTimeout", 0))

	require.NoError(t, svc.SetStringSetting(ctx, "baseServerUrl", "http://localhost"))
	require.Equal(t, "http://localhost", svc.GetStringSetting(ctx, "baseServerUrl", ""))
}

func TestSettingsService_UpdateSetting(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	require.NoError(t, svc.UpdateSetting(ctx, "pruneImageMode", "all"))

	var sv SettingVariable
	require.NoError(t, svc.db.WithContext(ctx).Where("key = ?", "pruneImageMode").First(&sv).Error)
	require.Equal(t, "all", sv.Value)
}

func TestSettingsService_UpdateSettingRejectsInvalidCronBeforePersistence(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	require.NoError(t, svc.UpdateSetting(ctx, "pollingInterval", "*/5 * * * * *"))
	require.Error(t, svc.UpdateSetting(ctx, "pollingInterval", "not a cron schedule"))

	var setting SettingVariable
	require.NoError(t, svc.db.WithContext(ctx).Where("key = ?", "pollingInterval").First(&setting).Error)
	require.Equal(t, "*/5 * * * * *", setting.Value)
}

func TestSettingsService_UpdateSetting_RefreshesCachedSnapshot(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	require.Equal(t, "http://localhost", svc.GetSettingsConfig().BaseServerURL.Value)
	require.NoError(t, svc.UpdateSetting(ctx, "baseServerUrl", "https://arcane.test"))

	require.Equal(t, "https://arcane.test", svc.GetSettingsConfig().BaseServerURL.Value)

	settingsCfg, err := svc.GetSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, "https://arcane.test", settingsCfg.BaseServerURL.Value)
}

func TestSettingsService_UpdateSettings_PruneModesDoNotTriggerScheduledPruneCallback(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	callbackCalls := 0
	svc.SubscribeSettingsChanges([]string{"scheduledPruneEnabled", "scheduledPruneInterval"}, func([]libarcane.SettingUpdate) {
		callbackCalls++
	})

	_, err = svc.UpdateSettings(ctx, settingstypes.Update{
		PruneImageMode:      new("all"),
		PruneContainerUntil: new("24h"),
	})
	require.NoError(t, err)
	waitForSettingsNotificationsInternal(t, svc)
	require.Equal(t, 0, callbackCalls)
}

func TestSettingsService_UpdateSettings_ScheduledPruneScheduleTriggersCallback(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	callbackCalls := 0
	svc.SubscribeSettingsChanges([]string{"scheduledPruneEnabled", "scheduledPruneInterval"}, func([]libarcane.SettingUpdate) {
		callbackCalls++
	})

	_, err = svc.UpdateSettings(ctx, settingstypes.Update{
		ScheduledPruneEnabled: new("true"),
	})
	require.NoError(t, err)
	waitForSettingsNotificationsInternal(t, svc)
	require.Equal(t, 1, callbackCalls)
}

func TestSettingsServiceActorSerializesConcurrentUpdatesInternal(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	start := make(chan struct{})
	results := make(chan error, 2)
	go func() {
		<-start
		_, updateErr := svc.UpdateSettings(ctx, settingstypes.Update{ProjectsDirectory: new("/data/projects-a")})
		results <- updateErr
	}()
	go func() {
		<-start
		_, updateErr := svc.UpdateSettings(ctx, settingstypes.Update{TemplatesDirectory: new("/data/templates-b")})
		results <- updateErr
	}()
	close(start)

	require.NoError(t, <-results)
	require.NoError(t, <-results)
	current := svc.GetSettingsConfig()
	require.Equal(t, "/data/projects-a", current.ProjectsDirectory.Value)
	require.Equal(t, "/data/templates-b", current.TemplatesDirectory.Value)
}

func TestSettingsServiceActorPublishesSnapshotBeforeAsynchronousNotificationInternal(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	callbackStarted := make(chan string, 1)
	releaseCallback := make(chan struct{})
	svc.SubscribeSettingsChanges([]string{"baseServerUrl"}, func(_ []libarcane.SettingUpdate) {
		callbackStarted <- svc.GetSettingsConfig().BaseServerURL.Value
		<-releaseCallback
	})

	updateResult := make(chan error, 1)
	go func() {
		_, updateErr := svc.UpdateSettings(ctx, settingstypes.Update{BaseServerURL: new("https://actor.example")})
		updateResult <- updateErr
	}()

	require.Equal(t, "https://actor.example", <-callbackStarted)
	select {
	case err := <-updateResult:
		require.NoError(t, err)
	case <-time.After(time.Second):
		require.FailNow(t, "settings update waited for notification callback")
	}

	secondUpdate := make(chan error, 1)
	go func() {
		_, updateErr := svc.UpdateSettings(ctx, settingstypes.Update{ProjectsDirectory: new("/data/unblocked")})
		secondUpdate <- updateErr
	}()
	select {
	case err := <-secondUpdate:
		require.NoError(t, err)
	case <-time.After(time.Second):
		require.FailNow(t, "settings subscriber blocked the next settings writer")
	}
	close(releaseCallback)
}

func TestSettingsServiceActorNotifiesSubscriberOnceForMultipleMatchingKeysInternal(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	var calls atomic.Int32
	notified := make(chan []libarcane.SettingUpdate, 1)
	svc.SubscribeSettingsChanges([]string{"pollingEnabled", "pollingInterval"}, func(updates []libarcane.SettingUpdate) {
		calls.Add(1)
		notified <- updates
	})

	_, err = svc.UpdateSettings(ctx, settingstypes.Update{
		PollingEnabled:  new("false"),
		PollingInterval: new("0 */5 * * * *"),
	})
	require.NoError(t, err)
	require.Len(t, <-notified, 2)
	require.Equal(t, int32(1), calls.Load())
}

func TestSettingsServiceActorDoesNotPublishSensitiveValuesInternal(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	var calls atomic.Int32
	svc.SubscribeSettingsChanges([]string{"oidcClientSecret"}, func([]libarcane.SettingUpdate) {
		calls.Add(1)
	})
	_, err = svc.UpdateSettings(ctx, settingstypes.Update{OidcClientSecret: new("should-not-leave-settings-service")})
	require.NoError(t, err)
	waitForSettingsNotificationsInternal(t, svc)
	require.Zero(t, calls.Load())
}

func TestSettingsServiceActorSerializesContainerExclusionUpdatesInternal(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	start := make(chan struct{})
	results := make(chan error, 2)
	for _, name := range []string{"api", "worker"} {
		go func() {
			<-start
			results <- svc.SetContainerAutoUpdateExclusionInternal(ctx, name, true)
		}()
	}
	close(start)

	require.NoError(t, <-results)
	require.NoError(t, <-results)
	require.ElementsMatch(t, []string{"api", "worker"}, strings.Split(svc.GetStringSetting(ctx, "autoUpdateExcludedContainers", ""), ","))
}

func BenchmarkSettingsService_GetSettings(b *testing.B) {
	ctx := context.Background()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		require.FailNowf(b, "benchmark database setup failed", "%v", err)
	}
	if err := db.AutoMigrate(&SettingVariable{}); err != nil {
		require.FailNowf(b, "benchmark database migration failed", "%v", err)
	}
	settingsDB := &database.DB{DB: db}
	svc, err := newSettingsServiceForTestInternal(b, ctx, settingsDB)
	if err != nil {
		require.FailNowf(b, "benchmark service setup failed", "%v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		settingsCfg, err := svc.GetSettings(ctx)
		if err != nil {
			require.FailNowf(b, "benchmark GetSettings failed", "%v", err)
		}
		if settingsCfg == nil {
			require.FailNow(b, "GetSettings returned nil settings")
		}
	}
}

func TestSettingsService_EnsureEncryptionKey(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	k1, err := svc.EnsureEncryptionKey(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, k1)

	k2, err := svc.EnsureEncryptionKey(ctx)
	require.NoError(t, err)
	require.Equal(t, k1, k2, "encryption key should be stable between calls")

	var sv SettingVariable
	require.NoError(t, svc.db.WithContext(ctx).Where("key = ?", "encryptionKey").First(&sv).Error)
	require.Equal(t, k1, sv.Value)
}

func TestSettingsService_LoadDatabaseSettings_ReloadsChanges(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	// Initially empty DB -> defaults (not persisted yet)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	// Update a value directly in DB
	require.NoError(t, svc.UpdateSetting(ctx, "projectsDirectory", "custom/projects"))

	// Force reload
	require.NoError(t, svc.LoadDatabaseSettings(ctx))

	cfg := svc.GetSettingsConfig()
	require.Equal(t, "custom/projects", cfg.ProjectsDirectory.Value)
}

func TestSettingsService_LoadDatabaseSettings_UIConfigurationDisabled_Env(t *testing.T) {
	// Set env + disable flag BEFORE service init
	t.Setenv("UI_CONFIGURATION_DISABLED", "true")
	t.Setenv("PROJECTS_DIRECTORY", "env/projects")
	t.Setenv("BASE_SERVER_URL", "https://env.example")

	c := config.Load()
	c.UIConfigurationDisabled = true

	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	// Reload explicitly (NewSettingsService already did, but explicit for clarity)
	require.NoError(t, svc.LoadDatabaseSettings(ctx))

	cfg := svc.GetSettingsConfig()
	require.Equal(t, "env/projects", cfg.ProjectsDirectory.Value)
	require.Equal(t, "https://env.example", cfg.BaseServerURL.Value)
}

func TestSettingsService_PersistEnvSettingsIfMissing_DoesNotOverrideForcedTrivyImage(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	t.Setenv("TRIVY_IMAGE", "ghcr.io/aquasecurity/trivy:latest")

	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	require.NoError(t, svc.PersistEnvSettingsIfMissing(ctx))

	var sv SettingVariable
	require.NoError(t, svc.db.WithContext(ctx).Where("key = ?", "trivyImage").First(&sv).Error)
	require.Equal(t, DefaultTrivyImage, sv.Value)
	require.Equal(t, DefaultTrivyImage, svc.GetSettingsConfig().TrivyImage.Value)
}

func TestSettingsService_UpdateSettings_RefreshesCache(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	newDir := "custom/projects2"
	req := settingstypes.Update{
		ProjectsDirectory: &newDir,
	}

	_, err = svc.UpdateSettings(ctx, req)
	require.NoError(t, err)

	// ListSettings uses the cached snapshot; should reflect updated value
	list := svc.ListSettings(SettingVisibilityAll)
	found := false
	for _, sv := range list {
		if sv.Key == "projectsDirectory" {
			found = true
			require.Equal(t, newDir, sv.Value)
		}
	}
	require.True(t, found, "projectsDirectory setting not found in cached list")
}

func TestSettingsService_UpdateSettings_ReturnsEnvOverriddenValues(t *testing.T) {
	t.Setenv("DOCKER_HOST", "tcp://env-docker:2375")

	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	settingsList, err := svc.UpdateSettings(ctx, settingstypes.Update{
		ProjectsDirectory: new("custom/projects2"),
	})
	require.NoError(t, err)

	found := false
	for _, sv := range settingsList {
		if sv.Key == "dockerHost" {
			found = true
			require.Equal(t, "tcp://env-docker:2375", sv.Value)
		}
	}
	require.True(t, found, "dockerHost setting not found in update response")
}

func TestSettingsService_UpdateSettings_TimeoutCallbackIncludesTrivyScanTimeout(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	var callbackPayload []libarcane.SettingUpdate
	svc.SubscribeSettingsChanges(libarcane.TimeoutSettingKeys(), func(timeoutSettings []libarcane.SettingUpdate) {
		callbackPayload = timeoutSettings
	})

	_, err = svc.UpdateSettings(ctx, settingstypes.Update{TrivyScanTimeout: new("1200")})
	require.NoError(t, err)
	waitForSettingsNotificationsInternal(t, svc)

	require.NotNil(t, callbackPayload)
	require.Contains(t, callbackPayload, libarcane.SettingUpdate{Key: "trivyScanTimeout", Value: "1200"})
}

func TestSettingsService_UpdateSettings_TimeoutCallbackIncludesTrivyResourceLimits(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	var callbackPayload []libarcane.SettingUpdate
	svc.SubscribeSettingsChanges(libarcane.TimeoutSettingKeys(), func(timeoutSettings []libarcane.SettingUpdate) {
		callbackPayload = timeoutSettings
	})

	_, err = svc.UpdateSettings(ctx, settingstypes.Update{
		TrivyResourceLimitsEnabled: new("false"),
		TrivyCpuLimit:              new("2.5"),
		TrivyMemoryLimitMb:         new("3072"),
	})
	require.NoError(t, err)
	waitForSettingsNotificationsInternal(t, svc)

	require.NotNil(t, callbackPayload)
	require.Contains(t, callbackPayload, libarcane.SettingUpdate{Key: "trivyResourceLimitsEnabled", Value: "false"})
	require.Contains(t, callbackPayload, libarcane.SettingUpdate{Key: "trivyCpuLimit", Value: "2.5"})
	require.Contains(t, callbackPayload, libarcane.SettingUpdate{Key: "trivyMemoryLimitMb", Value: "3072"})
}

func TestSettingsService_UpdateSettings_TimeoutCallbackIncludesTrivyConcurrentScanContainers(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	var callbackPayload []libarcane.SettingUpdate
	svc.SubscribeSettingsChanges(libarcane.TimeoutSettingKeys(), func(timeoutSettings []libarcane.SettingUpdate) {
		callbackPayload = timeoutSettings
	})

	_, err = svc.UpdateSettings(ctx, settingstypes.Update{TrivyConcurrentScanContainers: new("4")})
	require.NoError(t, err)
	waitForSettingsNotificationsInternal(t, svc)

	require.NotNil(t, callbackPayload)
	require.Contains(t, callbackPayload, libarcane.SettingUpdate{Key: "trivyConcurrentScanContainers", Value: "4"})
}

func TestSettingsService_UpdateSettings_TrivyNetworkTriggersVulnerabilityCallback(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	callbackCalled := false
	svc.SubscribeSettingsChanges([]string{"trivyNetwork"}, func(_ []libarcane.SettingUpdate) {
		callbackCalled = true
	})

	_, err = svc.UpdateSettings(ctx, settingstypes.Update{TrivyNetwork: new("arcane-external")})
	require.NoError(t, err)
	waitForSettingsNotificationsInternal(t, svc)
	require.True(t, callbackCalled)
}

func TestSettingsService_UpdateSettings_TrivyNetworkDoesNotTriggerTimeoutCallback(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	var callbackPayload []libarcane.SettingUpdate
	svc.SubscribeSettingsChanges(libarcane.TimeoutSettingKeys(), func(timeoutSettings []libarcane.SettingUpdate) {
		callbackPayload = timeoutSettings
	})

	_, err = svc.UpdateSettings(ctx, settingstypes.Update{TrivyNetwork: new("arcane-external")})
	require.NoError(t, err)
	waitForSettingsNotificationsInternal(t, svc)
	require.Nil(t, callbackPayload)
}

func TestSettingsService_UpdateSettings_TrivyRuntimeSecurityTriggersVulnerabilityCallback(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	callbackCalled := false
	svc.SubscribeSettingsChanges([]string{"trivySecurityOpts", "trivyPrivileged"}, func(_ []libarcane.SettingUpdate) {
		callbackCalled = true
	})

	_, err = svc.UpdateSettings(ctx, settingstypes.Update{
		TrivySecurityOpts: new("label=disable"),
		TrivyPrivileged:   new("true"),
	})
	require.NoError(t, err)
	waitForSettingsNotificationsInternal(t, svc)
	require.True(t, callbackCalled)
}

func TestSettingsService_UpdateSettings_TrivyRuntimeSecurityDoesNotTriggerTimeoutCallback(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	var callbackPayload []libarcane.SettingUpdate
	svc.SubscribeSettingsChanges(libarcane.TimeoutSettingKeys(), func(timeoutSettings []libarcane.SettingUpdate) {
		callbackPayload = timeoutSettings
	})

	_, err = svc.UpdateSettings(ctx, settingstypes.Update{
		TrivySecurityOpts: new("label=disable"),
		TrivyPrivileged:   new("true"),
	})
	require.NoError(t, err)
	waitForSettingsNotificationsInternal(t, svc)
	require.Nil(t, callbackPayload)
}

func TestSettingsService_UpdateSettings_TrivyPreserveCacheOnVolumePrunePersists(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	_, err = svc.UpdateSettings(ctx, settingstypes.Update{TrivyPreserveCacheOnVolumePrune: new("false")})
	require.NoError(t, err)

	current, err := svc.GetSettings(ctx)
	require.NoError(t, err)
	require.False(t, current.TrivyPreserveCacheOnVolumePrune.IsTrue())

	var stored SettingVariable
	err = svc.db.WithContext(ctx).Where("key = ?", "trivyPreserveCacheOnVolumePrune").First(&stored).Error
	require.NoError(t, err)
	require.Equal(t, "false", stored.Value)
}

func TestSettingsService_UpdateSettings_TrivyPreserveCacheOnVolumePruneDoesNotTriggerTimeoutCallback(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	var callbackPayload []libarcane.SettingUpdate
	svc.SubscribeSettingsChanges(libarcane.TimeoutSettingKeys(), func(timeoutSettings []libarcane.SettingUpdate) {
		callbackPayload = timeoutSettings
	})

	_, err = svc.UpdateSettings(ctx, settingstypes.Update{TrivyPreserveCacheOnVolumePrune: new("false")})
	require.NoError(t, err)
	waitForSettingsNotificationsInternal(t, svc)
	require.Nil(t, callbackPayload)
}

func TestSettingsService_LoadDatabaseSettings_InternalKeys_EnvMode(t *testing.T) {
	// Set env + disable flag
	t.Setenv("UI_CONFIGURATION_DISABLED", "true")

	ctx := context.Background()
	db := setupSettingsTestDB(t)

	// Pre-populate an internal setting in the DB
	internalKey := "instanceId"
	internalVal := "test-instance-id"
	require.NoError(t, db.DB.Create(&SettingVariable{Key: internalKey, Value: internalVal}).Error)

	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	// Reload explicitly to trigger the env loading path
	require.NoError(t, svc.LoadDatabaseSettings(ctx))

	cfg := svc.GetSettingsConfig()
	// Should have loaded the internal setting from DB even in env mode
	require.Equal(t, internalVal, cfg.InstanceID.Value)
}

func TestSettingsService_NormalizeProjectsDirectory_ConvertsRelativeToAbsolute(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	// Seed with relative path
	require.NoError(t, svc.UpdateSetting(ctx, "projectsDirectory", "data/projects"))

	// Run normalization without env var set (empty string)
	err = svc.NormalizeProjectsDirectory(ctx, "")
	require.NoError(t, err)

	// Verify it was updated to absolute path
	var setting SettingVariable
	require.NoError(t, svc.db.WithContext(ctx).Where("key = ?", "projectsDirectory").First(&setting).Error)

	// Should be converted to absolute path
	expectedPath, _ := filepath.Abs("data/projects")
	require.Equal(t, expectedPath, setting.Value)
	require.True(t, filepath.IsAbs(setting.Value), "path should be absolute")
}

func TestSettingsService_NormalizeProjectsDirectory_SkipsWhenEnvSet(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	// Seed with relative path
	require.NoError(t, svc.UpdateSetting(ctx, "projectsDirectory", "data/projects"))

	// Run normalization WITH env var set
	err = svc.NormalizeProjectsDirectory(ctx, "/custom/env/path")
	require.NoError(t, err)

	// Verify it was NOT changed
	var setting SettingVariable
	require.NoError(t, svc.db.WithContext(ctx).Where("key = ?", "projectsDirectory").First(&setting).Error)
	require.Equal(t, "data/projects", setting.Value, "should not change when env var is set")
}

func TestSettingsService_NormalizeProjectsDirectory_LeavesOtherPathsUnchanged(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	customPath := "/custom/projects/path"
	require.NoError(t, svc.UpdateSetting(ctx, "projectsDirectory", customPath))

	// Run normalization
	err = svc.NormalizeProjectsDirectory(ctx, "")
	require.NoError(t, err)

	// Verify it was NOT changed
	var setting SettingVariable
	require.NoError(t, svc.db.WithContext(ctx).Where("key = ?", "projectsDirectory").First(&setting).Error)
	require.Equal(t, customPath, setting.Value, "should not change custom paths")
}

func TestSettingsService_NormalizeProjectsDirectory_HandlesNotFound(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	// Don't create the setting at all

	// Run normalization - should not error
	err = svc.NormalizeProjectsDirectory(ctx, "")
	require.NoError(t, err)
}

func TestSettingsService_NormalizeProjectsDirectory_UpdatesCacheAfterNormalization(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	// Set to relative path
	require.NoError(t, svc.UpdateSetting(ctx, "projectsDirectory", "data/projects"))
	require.NoError(t, svc.LoadDatabaseSettings(ctx))

	// Verify cache has relative path
	cfg1 := svc.GetSettingsConfig()
	require.Equal(t, "data/projects", cfg1.ProjectsDirectory.Value)

	// Run normalization
	err = svc.NormalizeProjectsDirectory(ctx, "")
	require.NoError(t, err)

	// Verify cache was updated to absolute path
	cfg2 := svc.GetSettingsConfig()
	expectedPath, _ := filepath.Abs("data/projects")
	require.Equal(t, expectedPath, cfg2.ProjectsDirectory.Value)
	require.True(t, filepath.IsAbs(cfg2.ProjectsDirectory.Value), "path should be absolute")
}

func TestSettingsService_NormalizeProjectsDirectoryPublishesChangeInternal(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))
	require.NoError(t, svc.UpdateSetting(ctx, "projectsDirectory", "data/projects"))

	notified := make(chan []libarcane.SettingUpdate, 1)
	unsubscribe := svc.SubscribeSettingsChanges([]string{"projectsDirectory"}, func(updates []libarcane.SettingUpdate) {
		notified <- updates
	})
	defer unsubscribe()

	require.NoError(t, svc.NormalizeProjectsDirectory(ctx, ""))
	waitForSettingsNotificationsInternal(t, svc)
	updates := <-notified
	require.Len(t, updates, 1)
	require.Equal(t, "projectsDirectory", updates[0].Key)
	require.True(t, filepath.IsAbs(updates[0].Value))
}

// TestSettingsServiceEffectiveSnapshotMaterializedInternal verifies the
// env-override-applied snapshot is built once per refresh: reads share one
// pointer (no per-call clone), env overrides win over database values, and
// an update rebuilds the snapshot while keeping the override applied.
func TestSettingsServiceEffectiveSnapshotMaterializedInternal(t *testing.T) {
	ctx := context.Background()
	t.Setenv("PROJECTS_DIRECTORY", "/env/projects")

	db := setupSettingsTestDB(t)
	require.NoError(t, db.Create(&SettingVariable{Key: "projectsDirectory", Value: "/db/projects"}).Error)
	require.NoError(t, db.Create(&SettingVariable{Key: "baseServerUrl", Value: "http://before.example"}).Error)

	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	// Env override beats the database value on the effective snapshot.
	require.Equal(t, "/env/projects", svc.GetStringSetting(ctx, "projectsDirectory", ""))

	// Reads share the materialized snapshot instead of cloning per call.
	first, err := svc.GetSettings(ctx)
	require.NoError(t, err)
	again, err := svc.GetSettings(ctx)
	require.NoError(t, err)
	require.Same(t, first, again)

	// A settings update rebuilds the snapshot: the new value is visible and
	// the env override is still applied.
	require.NoError(t, svc.UpdateSetting(ctx, "baseServerUrl", "http://after.example"))
	require.Equal(t, "http://after.example", svc.GetStringSetting(ctx, "baseServerUrl", ""))
	require.Equal(t, "/env/projects", svc.GetStringSetting(ctx, "projectsDirectory", ""))

	refreshed, err := svc.GetSettings(ctx)
	require.NoError(t, err)
	require.NotSame(t, first, refreshed)
}
