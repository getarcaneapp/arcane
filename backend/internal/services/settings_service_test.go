package services

import (
	"context"
	"encoding/json"
	"testing"

	glsqlite "github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/ofkm/arcane-backend/internal/config"
	"github.com/ofkm/arcane-backend/internal/database"
	"github.com/ofkm/arcane-backend/internal/dto"
	"github.com/ofkm/arcane-backend/internal/models"
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

func TestSettingsService_SyncOidcEnvToDatabase(t *testing.T) {
	// Set env BEFORE creating service so config loader (if any) sees them
	t.Setenv("OIDC_ENABLED", "false")
	t.Setenv("OIDC_CLIENT_ID", "cid")
	t.Setenv("OIDC_CLIENT_SECRET", "csec")
	t.Setenv("OIDC_ISSUER_URL", "https://issuer.example")
	t.Setenv("OIDC_SCOPES", "openid profile email")
	t.Setenv("OIDC_ADMIN_CLAIM", "roles")
	t.Setenv("OIDC_ADMIN_VALUE", "admin")

	ctx := context.Background()
	db := setupSettingsTestDB(t)
	svc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	// (Re)load settings after env prepared
	vars, err := svc.SyncOidcEnvToDatabase(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, vars)

	var enabled models.SettingVariable
	require.NoError(t, svc.db.WithContext(ctx).Where("key = ?", "authOidcEnabled").First(&enabled).Error)
	require.Equal(t, "true", enabled.Value)

	var cfgVar models.SettingVariable
	require.NoError(t, svc.db.WithContext(ctx).Where("key = ?", "authOidcConfig").First(&cfgVar).Error)

	var oc models.OidcConfig
	require.NoError(t, json.Unmarshal([]byte(cfgVar.Value), &oc))
	require.Equal(t, "cid", oc.ClientID)
	require.Equal(t, "csec", oc.ClientSecret)
	require.Equal(t, "https://issuer.example", oc.IssuerURL)
	require.Equal(t, "openid profile email", oc.Scopes)
	require.Equal(t, "roles", oc.AdminClaim)
	require.Equal(t, "admin", oc.AdminValue)
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

	updates := dto.UpdateSettingsDto{
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
	req := dto.UpdateSettingsDto{
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
