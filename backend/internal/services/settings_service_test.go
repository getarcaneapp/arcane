package services

import (
	"context"
	"encoding/json"
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

	// Spot-check a couple keys exist
	for _, key := range []string{"authLocalEnabled", "projectsDirectory"} {
		var sv models.SettingVariable
		err := svc.db.WithContext(ctx).Where("key = ?", key).First(&sv).Error
		require.NoErrorf(t, err, "missing default key %s", key)
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
