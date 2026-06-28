package services

import (
	"context"
	"path/filepath"
	"testing"

	glsqlite "github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/getarcaneapp/arcane/types/v2/settings"
)

func setupSettingsTestDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := gorm.Open(glsqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.SettingVariable{}))
	return &database.DB{DB: db}
}

func TestSettingsService_EnsureDefaultSettings_Idempotent(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	var count1 int64
	require.NoError(t, svc.db.WithContext(ctx).Model(&models.SettingVariable{}).Count(&count1).Error)
	require.Positive(t, count1)

	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	var count2 int64
	require.NoError(t, svc.db.WithContext(ctx).Model(&models.SettingVariable{}).Count(&count2).Error)
	require.Equal(t, count1, count2)

	// Spot-check core and automation defaults exist with correct values
	for _, key := range []string{"authLocalEnabled", "projectsDirectory", "followProjectSymlinks", "autoUpdateExcludedContainers", "vulnerabilityScanEnabled", "vulnerabilityScanInterval", "trivyImage", "trivyNetwork", "trivySecurityOpts", "trivyPrivileged", "trivyPreserveCacheOnVolumePrune", "trivyResourceLimitsEnabled", "trivyCpuLimit", "trivyMemoryLimitMb", "trivyConcurrentScanContainers", "gitSyncMaxFiles", "gitSyncMaxTotalSizeMb", "gitSyncMaxBinarySizeMb"} {
		var sv models.SettingVariable
		err := svc.db.WithContext(ctx).Where("key = ?", key).First(&sv).Error
		require.NoErrorf(t, err, "missing default key %s", key)

		switch key {
		case "followProjectSymlinks":
			require.Equal(t, "false", sv.Value)
		case "autoUpdateExcludedContainers":
			require.Equal(t, "", sv.Value)
		case "vulnerabilityScanEnabled":
			require.Equal(t, "false", sv.Value)
		case "vulnerabilityScanInterval":
			require.Equal(t, "0 0 0 * * *", sv.Value)
		case "trivyImage":
			require.Equal(t, DefaultTrivyImage, sv.Value)
		case "trivyNetwork":
			require.Equal(t, "", sv.Value)
		case "trivySecurityOpts":
			require.Equal(t, "", sv.Value)
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

func TestSettingsService_EnsureDefaultSettings_OverridesExistingTrivyImage(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	require.NoError(t, svc.UpdateSetting(ctx, "trivyImage", "ghcr.io/aquasecurity/trivy:latest"))
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	var sv models.SettingVariable
	require.NoError(t, svc.db.WithContext(ctx).Where("key = ?", "trivyImage").First(&sv).Error)
	require.Equal(t, DefaultTrivyImage, sv.Value)
}

func TestSettingsService_GetSettings_UnknownKeysIgnored(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	require.NoError(t, svc.db.WithContext(ctx).
		Create(&models.SettingVariable{Key: "someUnknownKey", Value: "x"}).Error)

	_, err = svc.GetSettings(ctx)
	require.NoError(t, err)
}

func TestSettingsService_GetSettings_UsesCachedSnapshotWithoutDatabase(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	require.NoError(t, svc.SetStringSetting(ctx, "baseServerUrl", "http://cached"))

	// GetSettings should clone the in-memory snapshot and not touch the database.
	svc.db = nil

	settings, err := svc.GetSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, "http://cached", settings.BaseServerURL.Value)
}

func TestSettingsService_AvatarMaxUploadSizeDefaultAndUpdate(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	current, err := svc.GetSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, "2", current.AvatarMaxUploadSizeMb.Value)

	updatedValue := "8"
	_, err = svc.UpdateSettings(ctx, settings.Update{
		AvatarMaxUploadSizeMb: &updatedValue,
	})
	require.NoError(t, err)

	current, err = svc.GetSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, "8", current.AvatarMaxUploadSizeMb.Value)
}

func TestSettingsService_PruneUnknownSettings_RemovesStaleKeys(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	require.NoError(t, svc.UpdateSetting(ctx, "projectsDirectory", "/tmp/projects"))
	require.NoError(t, svc.UpdateSetting(ctx, "encryptionKey", "test-encryption-key"))
	require.NoError(t, svc.UpdateSetting(ctx, "unknownKey", "value"))

	require.NoError(t, svc.PruneUnknownSettings(ctx))

	var sv models.SettingVariable
	err = svc.db.WithContext(ctx).Where("key = ?", "unknownKey").First(&sv).Error
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)

	var sv2 models.SettingVariable
	err = svc.db.WithContext(ctx).Where("key = ?", "projectsDirectory").First(&sv2).Error
	require.NoError(t, err)

	var sv3 models.SettingVariable
	err = svc.db.WithContext(ctx).Where("key = ?", "encryptionKey").First(&sv3).Error
	require.NoError(t, err)
}

func TestSettingsService_GetSettings_EnvOverride_OidcMergeAccounts(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	t.Setenv("OIDC_MERGE_ACCOUNTS", "true")

	svc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	settings, err := svc.GetSettings(ctx)
	require.NoError(t, err)
	require.True(t, settings.OidcMergeAccounts.IsTrue())
}

func TestSettingsService_GetSettings_EnvOverride_TrivyScanTimeout(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	t.Setenv("TRIVY_SCAN_TIMEOUT", "1800")

	svc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	settings, err := svc.GetSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, 1800, settings.TrivyScanTimeout.AsInt())
}

func TestSettingsService_GetSettings_EnvOverride_TrivyResourceLimits(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	t.Setenv("TRIVY_RESOURCE_LIMITS_ENABLED", "false")
	t.Setenv("TRIVY_CPU_LIMIT", "2.5")
	t.Setenv("TRIVY_MEMORY_LIMIT_MB", "2048")

	svc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	settings, err := svc.GetSettings(ctx)
	require.NoError(t, err)
	require.False(t, settings.TrivyResourceLimitsEnabled.IsTrue())
	require.Equal(t, "2.5", settings.TrivyCpuLimit.Value)
	require.Equal(t, 2048, settings.TrivyMemoryLimitMb.AsInt())
}

func TestSettingsService_GetSettings_EnvOverride_TrivyNetwork(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	t.Setenv("TRIVY_NETWORK", "arcane-external")

	svc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	settings, err := svc.GetSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, "arcane-external", settings.TrivyNetwork.Value)
}

func TestSettingsService_GetSettings_EnvOverride_FollowProjectSymlinks(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	t.Setenv("FOLLOW_PROJECT_SYMLINKS", "true")

	svc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	settings, err := svc.GetSettings(ctx)
	require.NoError(t, err)
	require.True(t, settings.FollowProjectSymlinks.IsTrue())
}

func TestSettingsService_GetSettings_EnvOverride_TrivyRuntimeSecurity(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	t.Setenv("TRIVY_SECURITY_OPTS", "label=disable,\nlabel=type:container_runtime_t")
	t.Setenv("TRIVY_PRIVILEGED", "true")

	svc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	settings, err := svc.GetSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, "label=disable,\nlabel=type:container_runtime_t", settings.TrivySecurityOpts.Value)
	require.True(t, settings.TrivyPrivileged.IsTrue())
}

func TestSettingsService_GetStringSetting_EnvOverride_SwarmStackSourcesDirectory(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	t.Setenv("SWARM_STACK_SOURCES_DIRECTORY", "/mnt/swarm-from-env")

	svc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.UpdateSetting(ctx, "swarmStackSourcesDirectory", "/tmp/swarm-from-db"))

	require.Equal(t, "/mnt/swarm-from-env", svc.GetStringSetting(ctx, "swarmStackSourcesDirectory", "/fallback"))
	require.Equal(t, "/mnt/swarm-from-env", svc.GetSettingsConfig().SwarmStackSourcesDirectory.Value)
}

func TestSettingsService_isEnvOverrideActiveInternal(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)

	svc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.False(t, svc.isEnvOverrideActiveInternal("oidcEnabled"))

	t.Setenv("OIDC_ENABLED", "false")
	svcWithOverride, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.True(t, svcWithOverride.isEnvOverrideActiveInternal("oidcEnabled"))

	t.Setenv("AUTH_SESSION_TIMEOUT", "120")
	svcWithNonOverrideEnv, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.False(t, svcWithNonOverrideEnv.isEnvOverrideActiveInternal("authSessionTimeout"))
}

func TestSettingsService_GetSetHelpers(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := NewSettingsService(ctx, db)
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
	svc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	require.NoError(t, svc.UpdateSetting(ctx, "pruneImageMode", "all"))

	var sv models.SettingVariable
	require.NoError(t, svc.db.WithContext(ctx).Where("key = ?", "pruneImageMode").First(&sv).Error)
	require.Equal(t, "all", sv.Value)
}

func TestSettingsService_UpdateSetting_RefreshesCachedSnapshot(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	require.Equal(t, "http://localhost", svc.GetSettingsConfig().BaseServerURL.Value)
	require.NoError(t, svc.UpdateSetting(ctx, "baseServerUrl", "https://arcane.test"))

	require.Equal(t, "https://arcane.test", svc.GetSettingsConfig().BaseServerURL.Value)

	settings, err := svc.GetSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, "https://arcane.test", settings.BaseServerURL.Value)
}

func TestSettingsService_UpdateSettings_PruneModesDoNotTriggerScheduledPruneCallback(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	callbackCalls := 0
	svc.OnScheduledPruneSettingsChanged = func(context.Context) {
		callbackCalls++
	}

	_, err = svc.UpdateSettings(ctx, settings.Update{
		PruneImageMode:      new("all"),
		PruneContainerUntil: new("24h"),
	})
	require.NoError(t, err)
	require.Equal(t, 0, callbackCalls)
}

func TestSettingsService_UpdateSettings_ScheduledPruneScheduleTriggersCallback(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	callbackCalls := 0
	svc.OnScheduledPruneSettingsChanged = func(context.Context) {
		callbackCalls++
	}

	_, err = svc.UpdateSettings(ctx, settings.Update{
		ScheduledPruneEnabled: new("true"),
	})
	require.NoError(t, err)
	require.Equal(t, 1, callbackCalls)
}

func BenchmarkSettingsService_GetSettings(b *testing.B) {
	ctx := context.Background()
	db, err := gorm.Open(glsqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		b.Fatal(err)
	}
	if err := db.AutoMigrate(&models.SettingVariable{}); err != nil {
		b.Fatal(err)
	}
	settingsDB := &database.DB{DB: db}
	svc, err := NewSettingsService(ctx, settingsDB)
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		settings, err := svc.GetSettings(ctx)
		if err != nil {
			b.Fatal(err)
		}
		if settings == nil {
			b.Fatal("settings should not be nil")
		}
	}
}

func TestSettingsService_EnsureEncryptionKey(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	k1, err := svc.EnsureEncryptionKey(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, k1)

	k2, err := svc.EnsureEncryptionKey(ctx)
	require.NoError(t, err)
	require.Equal(t, k1, k2, "encryption key should be stable between calls")

	var sv models.SettingVariable
	require.NoError(t, svc.db.WithContext(ctx).Where("key = ?", "encryptionKey").First(&sv).Error)
	require.Equal(t, k1, sv.Value)
}

func TestSettingsService_LoadDatabaseSettings_ReloadsChanges(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := NewSettingsService(ctx, db)
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
	svc, err := NewSettingsService(ctx, db)
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

	svc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	require.NoError(t, svc.PersistEnvSettingsIfMissing(ctx))

	var sv models.SettingVariable
	require.NoError(t, svc.db.WithContext(ctx).Where("key = ?", "trivyImage").First(&sv).Error)
	require.Equal(t, DefaultTrivyImage, sv.Value)
	require.Equal(t, DefaultTrivyImage, svc.GetSettingsConfig().TrivyImage.Value)
}

func TestSettingsService_UpdateSettings_RefreshesCache(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	newDir := "custom/projects2"
	req := settings.Update{
		ProjectsDirectory: &newDir,
	}

	_, err = svc.UpdateSettings(ctx, req)
	require.NoError(t, err)

	// ListSettings uses the cached snapshot; should reflect updated value
	list := svc.ListSettings(models.SettingVisibilityAll)
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
	svc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	settingsList, err := svc.UpdateSettings(ctx, settings.Update{
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
	svc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	var callbackPayload []libarcane.SettingUpdate
	svc.OnTimeoutSettingsChanged = func(_ context.Context, timeoutSettings []libarcane.SettingUpdate) {
		callbackPayload = timeoutSettings
	}

	_, err = svc.UpdateSettings(ctx, settings.Update{TrivyScanTimeout: new("1200")})
	require.NoError(t, err)

	require.NotNil(t, callbackPayload)
	require.Contains(t, callbackPayload, libarcane.SettingUpdate{Key: "trivyScanTimeout", Value: "1200"})
}

func TestSettingsService_UpdateSettings_TimeoutCallbackIncludesTrivyResourceLimits(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	var callbackPayload []libarcane.SettingUpdate
	svc.OnTimeoutSettingsChanged = func(_ context.Context, timeoutSettings []libarcane.SettingUpdate) {
		callbackPayload = timeoutSettings
	}

	_, err = svc.UpdateSettings(ctx, settings.Update{
		TrivyResourceLimitsEnabled: new("false"),
		TrivyCpuLimit:              new("2.5"),
		TrivyMemoryLimitMb:         new("3072"),
	})
	require.NoError(t, err)

	require.NotNil(t, callbackPayload)
	require.Contains(t, callbackPayload, libarcane.SettingUpdate{Key: "trivyResourceLimitsEnabled", Value: "false"})
	require.Contains(t, callbackPayload, libarcane.SettingUpdate{Key: "trivyCpuLimit", Value: "2.5"})
	require.Contains(t, callbackPayload, libarcane.SettingUpdate{Key: "trivyMemoryLimitMb", Value: "3072"})
}

func TestSettingsService_UpdateSettings_TimeoutCallbackIncludesTrivyConcurrentScanContainers(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	var callbackPayload []libarcane.SettingUpdate
	svc.OnTimeoutSettingsChanged = func(_ context.Context, timeoutSettings []libarcane.SettingUpdate) {
		callbackPayload = timeoutSettings
	}

	_, err = svc.UpdateSettings(ctx, settings.Update{TrivyConcurrentScanContainers: new("4")})
	require.NoError(t, err)

	require.NotNil(t, callbackPayload)
	require.Contains(t, callbackPayload, libarcane.SettingUpdate{Key: "trivyConcurrentScanContainers", Value: "4"})
}

func TestSettingsService_UpdateSettings_TrivyNetworkTriggersVulnerabilityCallback(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	callbackCalled := false
	svc.OnVulnerabilityScanSettingsChanged = func(_ context.Context) {
		callbackCalled = true
	}

	_, err = svc.UpdateSettings(ctx, settings.Update{TrivyNetwork: new("arcane-external")})
	require.NoError(t, err)
	require.True(t, callbackCalled)
}

func TestSettingsService_UpdateSettings_TrivyNetworkDoesNotTriggerTimeoutCallback(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	var callbackPayload []libarcane.SettingUpdate
	svc.OnTimeoutSettingsChanged = func(_ context.Context, timeoutSettings []libarcane.SettingUpdate) {
		callbackPayload = timeoutSettings
	}

	_, err = svc.UpdateSettings(ctx, settings.Update{TrivyNetwork: new("arcane-external")})
	require.NoError(t, err)
	require.Nil(t, callbackPayload)
}

func TestSettingsService_UpdateSettings_TrivyRuntimeSecurityTriggersVulnerabilityCallback(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	callbackCalled := false
	svc.OnVulnerabilityScanSettingsChanged = func(_ context.Context) {
		callbackCalled = true
	}

	_, err = svc.UpdateSettings(ctx, settings.Update{
		TrivySecurityOpts: new("label=disable"),
		TrivyPrivileged:   new("true"),
	})
	require.NoError(t, err)
	require.True(t, callbackCalled)
}

func TestSettingsService_UpdateSettings_TrivyRuntimeSecurityDoesNotTriggerTimeoutCallback(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	var callbackPayload []libarcane.SettingUpdate
	svc.OnTimeoutSettingsChanged = func(_ context.Context, timeoutSettings []libarcane.SettingUpdate) {
		callbackPayload = timeoutSettings
	}

	_, err = svc.UpdateSettings(ctx, settings.Update{
		TrivySecurityOpts: new("label=disable"),
		TrivyPrivileged:   new("true"),
	})
	require.NoError(t, err)
	require.Nil(t, callbackPayload)
}

func TestSettingsService_UpdateSettings_TrivyPreserveCacheOnVolumePrunePersists(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	_, err = svc.UpdateSettings(ctx, settings.Update{TrivyPreserveCacheOnVolumePrune: new("false")})
	require.NoError(t, err)

	current, err := svc.GetSettings(ctx)
	require.NoError(t, err)
	require.False(t, current.TrivyPreserveCacheOnVolumePrune.IsTrue())

	var stored models.SettingVariable
	err = svc.db.WithContext(ctx).Where("key = ?", "trivyPreserveCacheOnVolumePrune").First(&stored).Error
	require.NoError(t, err)
	require.Equal(t, "false", stored.Value)
}

func TestSettingsService_UpdateSettings_TrivyPreserveCacheOnVolumePruneDoesNotTriggerTimeoutCallback(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	var callbackPayload []libarcane.SettingUpdate
	svc.OnTimeoutSettingsChanged = func(_ context.Context, timeoutSettings []libarcane.SettingUpdate) {
		callbackPayload = timeoutSettings
	}

	_, err = svc.UpdateSettings(ctx, settings.Update{TrivyPreserveCacheOnVolumePrune: new("false")})
	require.NoError(t, err)
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
	require.NoError(t, db.DB.Create(&models.SettingVariable{Key: internalKey, Value: internalVal}).Error)

	svc, err := NewSettingsService(ctx, db)
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
	svc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	// Seed with relative path
	require.NoError(t, svc.UpdateSetting(ctx, "projectsDirectory", "data/projects"))

	// Run normalization without env var set (empty string)
	err = svc.NormalizeProjectsDirectory(ctx, "")
	require.NoError(t, err)

	// Verify it was updated to absolute path
	var setting models.SettingVariable
	require.NoError(t, svc.db.WithContext(ctx).Where("key = ?", "projectsDirectory").First(&setting).Error)

	// Should be converted to absolute path
	expectedPath, _ := filepath.Abs("data/projects")
	require.Equal(t, expectedPath, setting.Value)
	require.True(t, filepath.IsAbs(setting.Value), "path should be absolute")
}

func TestSettingsService_NormalizeProjectsDirectory_SkipsWhenEnvSet(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	// Seed with relative path
	require.NoError(t, svc.UpdateSetting(ctx, "projectsDirectory", "data/projects"))

	// Run normalization WITH env var set
	err = svc.NormalizeProjectsDirectory(ctx, "/custom/env/path")
	require.NoError(t, err)

	// Verify it was NOT changed
	var setting models.SettingVariable
	require.NoError(t, svc.db.WithContext(ctx).Where("key = ?", "projectsDirectory").First(&setting).Error)
	require.Equal(t, "data/projects", setting.Value, "should not change when env var is set")
}

func TestSettingsService_NormalizeProjectsDirectory_LeavesOtherPathsUnchanged(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	customPath := "/custom/projects/path"
	require.NoError(t, svc.UpdateSetting(ctx, "projectsDirectory", customPath))

	// Run normalization
	err = svc.NormalizeProjectsDirectory(ctx, "")
	require.NoError(t, err)

	// Verify it was NOT changed
	var setting models.SettingVariable
	require.NoError(t, svc.db.WithContext(ctx).Where("key = ?", "projectsDirectory").First(&setting).Error)
	require.Equal(t, customPath, setting.Value, "should not change custom paths")
}

func TestSettingsService_NormalizeProjectsDirectory_HandlesNotFound(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	// Don't create the setting at all

	// Run normalization - should not error
	err = svc.NormalizeProjectsDirectory(ctx, "")
	require.NoError(t, err)
}

func TestSettingsService_NormalizeProjectsDirectory_UpdatesCacheAfterNormalization(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := NewSettingsService(ctx, db)
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
