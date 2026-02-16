package services

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	glsqlite "github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/types/settings"
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
	for _, key := range []string{"authLocalEnabled", "projectsDirectory", "autoUpdateExcludedContainers", "vulnerabilityScanEnabled", "vulnerabilityScanInterval", "trivyResourceLimitsEnabled", "trivyCpuLimit", "trivyMemoryLimitMb"} {
		var sv models.SettingVariable
		err := svc.db.WithContext(ctx).Where("key = ?", key).First(&sv).Error
		require.NoErrorf(t, err, "missing default key %s", key)

		switch key {
		case "autoUpdateExcludedContainers":
			require.Equal(t, "", sv.Value)
		case "vulnerabilityScanEnabled":
			require.Equal(t, "false", sv.Value)
		case "vulnerabilityScanInterval":
			require.Equal(t, "0 0 0 * * *", sv.Value)
		case "trivyResourceLimitsEnabled":
			require.Equal(t, "true", sv.Value)
		case "trivyCpuLimit":
			require.Equal(t, "1", sv.Value)
		case "trivyMemoryLimitMb":
			require.Equal(t, "0", sv.Value)
		}
	}
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

	svc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	// Default in DB is false
	settings1, err := svc.GetSettings(ctx)
	require.NoError(t, err)
	require.False(t, settings1.OidcMergeAccounts.IsTrue())

	// Env override should take precedence
	t.Setenv("OIDC_MERGE_ACCOUNTS", "true")
	settings2, err := svc.GetSettings(ctx)
	require.NoError(t, err)
	require.True(t, settings2.OidcMergeAccounts.IsTrue())
}

func TestSettingsService_GetSettings_EnvOverride_TrivyScanTimeout(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)

	svc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	settings1, err := svc.GetSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, 900, settings1.TrivyScanTimeout.AsInt())

	t.Setenv("TRIVY_SCAN_TIMEOUT", "1800")
	settings2, err := svc.GetSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, 1800, settings2.TrivyScanTimeout.AsInt())
}

func TestSettingsService_GetSettings_EnvOverride_TrivyResourceLimits(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)

	svc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	settings1, err := svc.GetSettings(ctx)
	require.NoError(t, err)
	require.True(t, settings1.TrivyResourceLimitsEnabled.IsTrue())
	require.Equal(t, "1", settings1.TrivyCpuLimit.Value)
	require.Equal(t, 0, settings1.TrivyMemoryLimitMb.AsInt())

	t.Setenv("TRIVY_RESOURCE_LIMITS_ENABLED", "false")
	t.Setenv("TRIVY_CPU_LIMIT", "2.5")
	t.Setenv("TRIVY_MEMORY_LIMIT_MB", "2048")

	settings2, err := svc.GetSettings(ctx)
	require.NoError(t, err)
	require.False(t, settings2.TrivyResourceLimitsEnabled.IsTrue())
	require.Equal(t, "2.5", settings2.TrivyCpuLimit.Value)
	require.Equal(t, 2048, settings2.TrivyMemoryLimitMb.AsInt())
}

func TestSettingsService_isEnvOverrideActiveInternal(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	// Not set => not forced
	require.False(t, svc.isEnvOverrideActiveInternal("oidcEnabled"))

	// Explicit false still counts as active env override
	t.Setenv("OIDC_ENABLED", "false")
	require.True(t, svc.isEnvOverrideActiveInternal("oidcEnabled"))

	// Empty value should not be treated as active (matches applyEnvOverrides behavior)
	t.Setenv("OIDC_ENABLED", "")
	require.False(t, svc.isEnvOverrideActiveInternal("oidcEnabled"))

	// Non-envOverride keys should never be forced via this helper
	t.Setenv("AUTH_SESSION_TIMEOUT", "120")
	require.False(t, svc.isEnvOverrideActiveInternal("authSessionTimeout"))
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

	// Use an existing key ("pruneMode") instead of a non-existent one
	require.NoError(t, svc.UpdateSetting(ctx, "pruneMode", "all"))

	var sv models.SettingVariable
	require.NoError(t, svc.db.WithContext(ctx).Where("key = ?", "pruneMode").First(&sv).Error)
	require.Equal(t, "all", sv.Value)
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

func TestSettingsService_UpdateSettings_MergeOidcSecret(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	// Seed existing OIDC config with a secret
	existing := models.OidcConfig{
		ClientID:     "old",
		ClientSecret: "keep-this",
		IssuerURL:    "https://issuer",
	}
	b, err := json.Marshal(existing)
	require.NoError(t, err)
	require.NoError(t, svc.UpdateSetting(ctx, "authOidcConfig", string(b)))

	// Incoming update missing clientSecret should preserve existing one
	incoming := models.OidcConfig{
		ClientID:  "new",
		IssuerURL: "https://issuer",
	}
	nb, err := json.Marshal(incoming)
	require.NoError(t, err)
	s := string(nb)

	updates := settings.Update{
		AuthOidcConfig: &s,
	}
	_, err = svc.UpdateSettings(ctx, updates)
	require.NoError(t, err)

	var cfgVar models.SettingVariable
	require.NoError(t, svc.db.WithContext(ctx).Where("key = ?", "authOidcConfig").First(&cfgVar).Error)

	var merged models.OidcConfig
	require.NoError(t, json.Unmarshal([]byte(cfgVar.Value), &merged))
	require.Equal(t, "new", merged.ClientID)
	require.Equal(t, "keep-this", merged.ClientSecret)
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
	list := svc.ListSettings(true)
	found := false
	for _, sv := range list {
		if sv.Key == "projectsDirectory" {
			found = true
			require.Equal(t, newDir, sv.Value)
		}
	}
	require.True(t, found, "projectsDirectory setting not found in cached list")
}

func TestSettingsService_UpdateSettings_TimeoutCallbackIncludesTrivyScanTimeout(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	var callbackPayload map[string]string
	svc.OnTimeoutSettingsChanged = func(_ context.Context, timeoutSettings map[string]string) {
		callbackPayload = timeoutSettings
	}

	newTimeout := "1200"
	_, err = svc.UpdateSettings(ctx, settings.Update{TrivyScanTimeout: &newTimeout})
	require.NoError(t, err)

	require.NotNil(t, callbackPayload)
	require.Equal(t, "1200", callbackPayload["trivyScanTimeout"])
}

func TestSettingsService_UpdateSettings_TimeoutCallbackIncludesTrivyResourceLimits(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	var callbackPayload map[string]string
	svc.OnTimeoutSettingsChanged = func(_ context.Context, timeoutSettings map[string]string) {
		callbackPayload = timeoutSettings
	}

	limitsEnabled := "false"
	cpuLimit := "2.5"
	memoryLimit := "3072"
	_, err = svc.UpdateSettings(ctx, settings.Update{
		TrivyResourceLimitsEnabled: &limitsEnabled,
		TrivyCpuLimit:              &cpuLimit,
		TrivyMemoryLimitMb:         &memoryLimit,
	})
	require.NoError(t, err)

	require.NotNil(t, callbackPayload)
	require.Equal(t, "false", callbackPayload["trivyResourceLimitsEnabled"])
	require.Equal(t, "2.5", callbackPayload["trivyCpuLimit"])
	require.Equal(t, "3072", callbackPayload["trivyMemoryLimitMb"])
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

func TestSettingsService_MigrateOidcConfigToFields(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	// Seed legacy OIDC JSON config
	legacyConfig := models.OidcConfig{
		ClientID:     "legacy-client-id",
		ClientSecret: "legacy-secret",
		IssuerURL:    "https://legacy-issuer.example",
		Scopes:       "openid email profile",
		AdminClaim:   "groups",
		AdminValue:   "admin",
	}
	b, err := json.Marshal(legacyConfig)
	require.NoError(t, err)
	require.NoError(t, svc.UpdateSetting(ctx, "authOidcConfig", string(b)))

	// Run migration
	err = svc.MigrateOidcConfigToFields(ctx)
	require.NoError(t, err)

	// Verify individual fields were populated
	var clientId models.SettingVariable
	require.NoError(t, svc.db.WithContext(ctx).Where("key = ?", "oidcClientId").First(&clientId).Error)
	require.Equal(t, "legacy-client-id", clientId.Value)

	var clientSecret models.SettingVariable
	require.NoError(t, svc.db.WithContext(ctx).Where("key = ?", "oidcClientSecret").First(&clientSecret).Error)
	require.Equal(t, "legacy-secret", clientSecret.Value)

	var issuerUrl models.SettingVariable
	require.NoError(t, svc.db.WithContext(ctx).Where("key = ?", "oidcIssuerUrl").First(&issuerUrl).Error)
	require.Equal(t, "https://legacy-issuer.example", issuerUrl.Value)

	var scopes models.SettingVariable
	require.NoError(t, svc.db.WithContext(ctx).Where("key = ?", "oidcScopes").First(&scopes).Error)
	require.Equal(t, "openid email profile", scopes.Value)

	var adminClaim models.SettingVariable
	require.NoError(t, svc.db.WithContext(ctx).Where("key = ?", "oidcAdminClaim").First(&adminClaim).Error)
	require.Equal(t, "groups", adminClaim.Value)

	var adminValue models.SettingVariable
	require.NoError(t, svc.db.WithContext(ctx).Where("key = ?", "oidcAdminValue").First(&adminValue).Error)
	require.Equal(t, "admin", adminValue.Value)
}

func TestSettingsService_MigrateOidcConfigToFields_SkipsIfAlreadyMigrated(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	// Pre-populate individual field
	require.NoError(t, svc.UpdateSetting(ctx, "oidcClientId", "already-migrated"))

	// Seed legacy config too
	legacyConfig := models.OidcConfig{
		ClientID:  "old-id",
		IssuerURL: "https://old-issuer.example",
	}
	b, err := json.Marshal(legacyConfig)
	require.NoError(t, err)
	require.NoError(t, svc.UpdateSetting(ctx, "authOidcConfig", string(b)))

	// Run migration - should skip since individual field is populated
	err = svc.MigrateOidcConfigToFields(ctx)
	require.NoError(t, err)

	// Verify field was NOT overwritten
	var clientId models.SettingVariable
	require.NoError(t, svc.db.WithContext(ctx).Where("key = ?", "oidcClientId").First(&clientId).Error)
	require.Equal(t, "already-migrated", clientId.Value)
}

func TestSettingsService_MigrateOidcConfigToFields_RealWorldJSON(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	// Test with real-world JSON format (as stored in database)
	realWorldJSON := `{"clientId":"ab92b6cf-283d-4764-9308-92a9b9496bf1","clientSecret":"super-secret-value","issuerUrl":"https://id.ofkm.us","scopes":"openid email profile groups","adminClaim":"groups","adminValue":"_arcane_admins"}`
	require.NoError(t, svc.UpdateSetting(ctx, "authOidcConfig", realWorldJSON))

	// Run migration
	err = svc.MigrateOidcConfigToFields(ctx)
	require.NoError(t, err)

	// Verify all individual fields were populated correctly
	var clientId models.SettingVariable
	require.NoError(t, svc.db.WithContext(ctx).Where("key = ?", "oidcClientId").First(&clientId).Error)
	require.Equal(t, "ab92b6cf-283d-4764-9308-92a9b9496bf1", clientId.Value)

	var clientSecret models.SettingVariable
	require.NoError(t, svc.db.WithContext(ctx).Where("key = ?", "oidcClientSecret").First(&clientSecret).Error)
	require.Equal(t, "super-secret-value", clientSecret.Value)

	var issuerUrl models.SettingVariable
	require.NoError(t, svc.db.WithContext(ctx).Where("key = ?", "oidcIssuerUrl").First(&issuerUrl).Error)
	require.Equal(t, "https://id.ofkm.us", issuerUrl.Value)

	var scopes models.SettingVariable
	require.NoError(t, svc.db.WithContext(ctx).Where("key = ?", "oidcScopes").First(&scopes).Error)
	require.Equal(t, "openid email profile groups", scopes.Value)

	var adminClaim models.SettingVariable
	require.NoError(t, svc.db.WithContext(ctx).Where("key = ?", "oidcAdminClaim").First(&adminClaim).Error)
	require.Equal(t, "groups", adminClaim.Value)

	var adminValue models.SettingVariable
	require.NoError(t, svc.db.WithContext(ctx).Where("key = ?", "oidcAdminValue").First(&adminValue).Error)
	require.Equal(t, "_arcane_admins", adminValue.Value)
}

func TestSettingsService_MigrateOidcConfigToFields_EmptyConfig(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	// Empty config should not cause errors
	require.NoError(t, svc.UpdateSetting(ctx, "authOidcConfig", "{}"))

	err = svc.MigrateOidcConfigToFields(ctx)
	require.NoError(t, err)

	// Verify fields remain empty
	var clientId models.SettingVariable
	require.NoError(t, svc.db.WithContext(ctx).Where("key = ?", "oidcClientId").First(&clientId).Error)
	require.Empty(t, clientId.Value)
}

func TestSettingsService_MigrateOidcConfigToFields_InvalidJSON(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	// Invalid JSON should not cause errors (gracefully handled)
	require.NoError(t, svc.UpdateSetting(ctx, "authOidcConfig", "not valid json"))

	err = svc.MigrateOidcConfigToFields(ctx)
	require.NoError(t, err) // Should not return error, just skip

	// Verify fields remain empty
	var clientId models.SettingVariable
	require.NoError(t, svc.db.WithContext(ctx).Where("key = ?", "oidcClientId").First(&clientId).Error)
	require.Empty(t, clientId.Value)
}

func TestSettingsService_MigrateOidcConfigToFields_DefaultScopes(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.EnsureDefaultSettings(ctx))

	// Config without scopes should get default scopes
	configWithoutScopes := `{"clientId":"test-client","issuerUrl":"https://test.example"}`
	require.NoError(t, svc.UpdateSetting(ctx, "authOidcConfig", configWithoutScopes))

	err = svc.MigrateOidcConfigToFields(ctx)
	require.NoError(t, err)

	var scopes models.SettingVariable
	require.NoError(t, svc.db.WithContext(ctx).Where("key = ?", "oidcScopes").First(&scopes).Error)
	require.Equal(t, "openid email profile", scopes.Value)
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
