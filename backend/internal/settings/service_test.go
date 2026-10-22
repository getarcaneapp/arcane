package settings

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"go.getarcane.app/sys/crypto"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/internal/config"
	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane"
)

func setupSettingsTestDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&SettingVariable{}))
	return &database.DB{DB: db}
}

func newSettingsServiceForTest(t testing.TB, ctx context.Context, db *database.DB) (*SettingsService, error) {
	t.Helper()
	crypto.InitEncryption(&crypto.Config{Environment: "development"})
	env, err := LoadEnv()
	require.NoError(t, err)
	cfg, err := config.Load()
	require.NoError(t, err)
	svc, err := NewSettingsService(ctx, db, cfg, env)
	if err == nil {
		t.Cleanup(func() { require.NoError(t, svc.Stop(context.WithoutCancel(t.Context()))) })
	}
	return svc, err
}

func waitForSettingsNotifications(t *testing.T, svc *SettingsService) {
	t.Helper()
	done := make(chan struct{})
	svc.writes.Lock()
	err := svc.enqueueEffect(func() { close(done) })
	svc.writes.Unlock()
	require.NoError(t, err)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("settings effects did not finish")
	}
}

func TestSettingsService_ImageEventWatcherSettingPersists(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)
	require.False(t, svc.GetBoolSetting(ctx, "imageEventWatcherEnabled"))

	err = svc.UpdateSettings(ctx, types.SettingsUpdate{"imageEventWatcherEnabled": "true"})
	require.NoError(t, err)
	require.True(t, svc.GetBoolSetting(ctx, "imageEventWatcherEnabled"))

	var stored SettingVariable
	require.NoError(t, db.WithContext(ctx).Where("key = ?", "imageEventWatcherEnabled").First(&stored).Error)
	require.Equal(t, "true", stored.Value)
}

func TestSettingsService_GetSettings_UnknownKeysIgnored(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	require.NoError(t, svc.db.WithContext(ctx).
		Create(&SettingVariable{Key: "someUnknownKey", Value: "x"}).Error)

	_, err = svc.GetSettings(ctx)
	require.NoError(t, err)
}

func TestSettingsService_GetSettings_UsesCachedSnapshotWithoutDatabase(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	require.NoError(t, svc.SetStringSetting(ctx, "baseServerUrl", "http://cached"))

	// GetSettings should clone the in-memory snapshot and not touch the database.
	svc.db = nil

	settingsCfg, err := svc.GetSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, "http://cached", settingsCfg.BaseServerURL.Value)
}

func TestSettingsService_AvatarMaxUploadSizeDefaultAndUpdate(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	current, err := svc.GetSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, "2", current.AvatarMaxUploadSizeMb.Value)

	updatedValue := "8"
	err = svc.UpdateSettings(ctx, types.SettingsUpdate{
		"avatarMaxUploadSizeMb": updatedValue,
	})
	require.NoError(t, err)

	current, err = svc.GetSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, "8", current.AvatarMaxUploadSizeMb.Value)
}

func TestSettingsServiceUpdateSettingsRejectsOIDCIssuerChangeWithStoredSecret(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.UpdateSetting(ctx, "oidcIssuerUrl", "https://issuer.example.com"))
	require.NoError(t, svc.UpdateSetting(ctx, "oidcClientSecret", "old-client-secret"))

	err = svc.UpdateSettings(ctx, types.SettingsUpdate{
		"oidcIssuerUrl": "https://attacker.example.com",
	})
	require.ErrorIs(t, err, common.ErrValidation)

	current, loadErr := svc.GetSettings(ctx)
	require.NoError(t, loadErr)
	require.Equal(t, "https://issuer.example.com", current.OidcIssuerUrl.Value)
	require.Equal(t, "old-client-secret", current.OidcClientSecret.Value)
}

func TestSettingsServiceUpdateSettingsAllowsOIDCIssuerChangeWhenClearingSecret(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.UpdateSetting(ctx, "oidcIssuerUrl", "https://issuer.example.com"))
	require.NoError(t, svc.UpdateSetting(ctx, "oidcClientSecret", "old-client-secret"))

	err = svc.UpdateSettings(ctx, types.SettingsUpdate{
		"oidcIssuerUrl":    "https://replacement.example.com",
		"oidcClientSecret": "",
	})
	require.NoError(t, err)

	current, loadErr := svc.GetSettings(ctx)
	require.NoError(t, loadErr)
	require.Equal(t, "https://replacement.example.com", current.OidcIssuerUrl.Value)
	require.Empty(t, current.OidcClientSecret.Value)
}

func TestSettingsServiceUpdateSettingsRejectsEnablingOIDCWithoutSecret(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	err = svc.UpdateSettings(ctx, types.SettingsUpdate{
		"oidcEnabled":   "true",
		"oidcClientId":  "arcane",
		"oidcIssuerUrl": "https://issuer.example.com",
	})
	require.ErrorIs(t, err, common.ErrValidation)

	current, loadErr := svc.GetSettings(ctx)
	require.NoError(t, loadErr)
	require.Equal(t, "false", current.OidcEnabled.Value)
}

func TestSettingsServiceUpdateSettingsAllowsOIDCIssuerChangeWithReplacementSecret(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.UpdateSetting(ctx, "oidcIssuerUrl", "https://issuer.example.com"))
	require.NoError(t, svc.UpdateSetting(ctx, "oidcClientSecret", "old-client-secret"))

	require.NoError(t, svc.UpdateSetting(ctx, "oidcProviderName", "  Cafe\u0301 SSO  "))
	before, loadErr := svc.GetSettings(ctx)
	require.NoError(t, loadErr)
	require.Equal(t, "Café SSO", before.OidcProviderName.Value)

	err = svc.UpdateSettings(ctx, types.SettingsUpdate{
		"oidcProviderName": "  Cafe\u0301 Login  ",
		"oidcIssuerUrl":    "https://replacement.example.com",
		"oidcClientSecret": "new-client-secret",
	})
	require.NoError(t, err)

	current, loadErr := svc.GetSettings(ctx)
	require.NoError(t, loadErr)
	require.Equal(t, "https://replacement.example.com", current.OidcIssuerUrl.Value)
	require.Equal(t, "new-client-secret", current.OidcClientSecret.Value)
	require.Equal(t, "Café Login", current.OidcProviderName.Value)
}

func TestSettingsServiceUpdateSettingsRejectsTrivyServerChangeWithStoredToken(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.UpdateSetting(ctx, "trivyServerUrl", "https://trivy.example.com"))
	require.NoError(t, svc.UpdateSetting(ctx, "trivyServerToken", "old-trivy-token"))

	err = svc.UpdateSettings(ctx, types.SettingsUpdate{
		"trivyServerUrl": "https://attacker.example.com",
	})
	require.ErrorIs(t, err, common.ErrValidation)

	current, loadErr := svc.GetSettings(ctx)
	require.NoError(t, loadErr)
	require.Equal(t, "https://trivy.example.com", current.TrivyServerUrl.Value)
	require.Equal(t, "old-trivy-token", current.TrivyServerToken.Value)
}

func TestSettingsServiceUpdateSettingsAllowsTrivyServerChangeWithReplacementToken(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.UpdateSetting(ctx, "trivyServerUrl", "https://trivy.example.com"))
	require.NoError(t, svc.UpdateSetting(ctx, "trivyServerToken", "old-trivy-token"))

	err = svc.UpdateSettings(ctx, types.SettingsUpdate{
		"trivyServerUrl":   "https://replacement.example.com",
		"trivyServerToken": "new-trivy-token",
	})
	require.NoError(t, err)

	current, loadErr := svc.GetSettings(ctx)
	require.NoError(t, loadErr)
	require.Equal(t, "https://replacement.example.com", current.TrivyServerUrl.Value)
	require.Equal(t, "new-trivy-token", current.TrivyServerToken.Value)
}

func TestSettingsServiceUpdateSettingsAllowsClearingTrivyServerWithStoredToken(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.UpdateSetting(ctx, "trivyServerUrl", "https://trivy.example.com"))
	require.NoError(t, svc.UpdateSetting(ctx, "trivyServerToken", "old-trivy-token"))

	err = svc.UpdateSettings(ctx, types.SettingsUpdate{
		"trivyServerUrl": "",
	})
	require.NoError(t, err)

	current, loadErr := svc.GetSettings(ctx)
	require.NoError(t, loadErr)
	require.Empty(t, current.TrivyServerUrl.Value)
	require.Equal(t, "old-trivy-token", current.TrivyServerToken.Value)
}

func TestSettingsServiceUpdateSettingsAllowsTrivyServerChangeWithoutStoredToken(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.UpdateSetting(ctx, "trivyServerUrl", "https://trivy.example.com"))

	err = svc.UpdateSettings(ctx, types.SettingsUpdate{
		"trivyServerUrl": "https://replacement.example.com",
	})
	require.NoError(t, err)

	current, loadErr := svc.GetSettings(ctx)
	require.NoError(t, loadErr)
	require.Equal(t, "https://replacement.example.com", current.TrivyServerUrl.Value)
}

func TestSettingsService_PruneUnknownSettings_RemovesStaleKeys(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTest(t, ctx, db)
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
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	t.Setenv("ARCANE_OIDC_MERGE_ACCOUNTS", "true")

	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	settingsCfg, err := svc.GetSettings(ctx)
	require.NoError(t, err)
	require.True(t, settingsCfg.OidcMergeAccounts.IsTrue())
}

func TestSettingsService_GetSettings_EnvOverride_TrivyScanTimeout(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	t.Setenv("ARCANE_TRIVY_SCAN_TIMEOUT", "1800")

	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	settingsCfg, err := svc.GetSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, 1800, settingsCfg.TrivyScanTimeout.AsInt())
}

func TestSettingsService_GetSettings_EnvOverride_TrivyResourceLimits(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	t.Setenv("ARCANE_TRIVY_RESOURCE_LIMITS_ENABLED", "false")
	t.Setenv("ARCANE_TRIVY_CPU_LIMIT", "2.5")
	t.Setenv("ARCANE_TRIVY_MEMORY_LIMIT_MB", "2048")

	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	settingsCfg, err := svc.GetSettings(ctx)
	require.NoError(t, err)
	require.False(t, settingsCfg.TrivyResourceLimitsEnabled.IsTrue())
	require.Equal(t, "2.5", settingsCfg.TrivyCpuLimit.Value)
	require.Equal(t, 2048, settingsCfg.TrivyMemoryLimitMb.AsInt())
}

func TestSettingsService_GetSettings_EnvOverride_TrivyNetwork(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	t.Setenv("ARCANE_TRIVY_NETWORK", "arcane-external")

	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	settingsCfg, err := svc.GetSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, "arcane-external", settingsCfg.TrivyNetwork.Value)
}

func TestSettingsService_GetSettings_EnvOverride_FollowProjectSymlinks(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	t.Setenv("ARCANE_FOLLOW_PROJECT_SYMLINKS", "true")

	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	settingsCfg, err := svc.GetSettings(ctx)
	require.NoError(t, err)
	require.True(t, settingsCfg.FollowProjectSymlinks.IsTrue())
}

func TestSettingsService_GetSettings_EnvOverride_TrivyRuntimeSecurity(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	t.Setenv("ARCANE_TRIVY_SECURITY_OPTS", "label=disable,\nlabel=type:container_runtime_t")
	t.Setenv("ARCANE_TRIVY_PRIVILEGED", "true")

	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	settingsCfg, err := svc.GetSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, "label=disable,\nlabel=type:container_runtime_t", settingsCfg.TrivySecurityOpts.Value)
	require.True(t, settingsCfg.TrivyPrivileged.IsTrue())
}

func TestSettingsService_GetStringSetting_EnvOverride_SwarmStackSourcesDirectory(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	t.Setenv("ARCANE_SWARM_STACK_SOURCES_DIRECTORY", "/mnt/swarm-from-env")

	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.UpdateSetting(ctx, "swarmStackSourcesDirectory", "/tmp/swarm-from-db"))

	require.Equal(t, "/mnt/swarm-from-env", svc.GetStringSetting(ctx, "swarmStackSourcesDirectory"))
	require.Equal(t, "/mnt/swarm-from-env", svc.GetSettingsConfig().SwarmStackSourcesDirectory.Value)
}

func TestSettingsService_SnapshotSources(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	t.Setenv("ARCANE_TRIVY_NETWORK", "arcane-external")
	require.NoError(t, db.Create(&SettingVariable{Key: "baseServerUrl", Value: "https://db.example"}).Error)
	require.NoError(t, db.Create(&SettingVariable{Key: "trivyNetwork", Value: "db-network"}).Error)

	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	_, byKey := settingsFields()
	sources := svc.snapshot.Load().sources
	require.Equal(t, SourceDefault, sources[byKey["defaultShell"].position])
	require.Equal(t, SourceDB, sources[byKey["baseServerUrl"].position])
	require.Equal(t, SourceEnv, sources[byKey["trivyNetwork"].position])
	require.Equal(t, "/bin/sh", svc.GetStringSetting(ctx, "defaultShell"))
	require.Equal(t, "https://db.example", svc.GetStringSetting(ctx, "baseServerUrl"))
	require.Equal(t, "arcane-external", svc.GetStringSetting(ctx, "trivyNetwork"))
}

func TestSettingsService_InvalidStoredValueFallsBackToDefault(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	require.NoError(t, db.Create(&SettingVariable{Key: "autoHealInterval", Value: "not a cron"}).Error)
	require.NoError(t, db.Create(&SettingVariable{Key: "enableGravatar", Value: "yes"}).Error)

	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	_, byKey := settingsFields()
	require.Equal(t, SourceDefault, svc.snapshot.Load().sources[byKey["autoHealInterval"].position])
	require.Equal(t, "0 */5 * * * *", svc.GetStringSetting(ctx, "autoHealInterval"))
	require.False(t, svc.GetBoolSetting(ctx, "enableGravatar"))
}

func TestSettingsService_EnvLockRejectsChangesAndAllowsSameValue(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	t.Setenv("ARCANE_OIDC_MERGE_ACCOUNTS", "false")

	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	locked, reason := svc.IsLocked("oidcMergeAccounts")
	require.True(t, locked)
	require.Equal(t, LockReasonEnv, reason)
	locked, _ = svc.IsLocked("authSessionTimeout")
	require.False(t, locked)
	locked, _ = svc.IsLocked("instanceId")
	require.False(t, locked)

	err = svc.UpdateSettings(ctx, types.SettingsUpdate{"oidcMergeAccounts": "true"})
	require.ErrorIs(t, err, common.ErrValidation)
	require.ErrorContains(t, err, "ARCANE_OIDC_MERGE_ACCOUNTS")

	err = svc.UpdateSettings(ctx, types.SettingsUpdate{
		"oidcMergeAccounts":  "false",
		"authSessionTimeout": "120",
	})
	require.NoError(t, err)
	require.Equal(t, 120, svc.GetIntSetting(ctx, "authSessionTimeout"))

	var count int64
	require.NoError(t, db.Model(&SettingVariable{}).Where("key = ?", "oidcMergeAccounts").Count(&count).Error)
	require.Zero(t, count)
}

func TestSettingsService_ReadOnlyGate(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	t.Setenv("ARCANE_UI_CONFIGURATION_DISABLED", "true")
	t.Setenv("ARCANE_TRIVY_NETWORK", "arcane-external")

	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	locked, reason := svc.IsLocked("baseServerUrl")
	require.True(t, locked)
	require.Equal(t, LockReasonReadOnly, reason)
	_, reason = svc.IsLocked("trivyNetwork")
	require.Equal(t, LockReasonEnv, reason)
	locked, _ = svc.IsLocked("agentToken")
	require.False(t, locked)

	err = svc.UpdateSettings(ctx, types.SettingsUpdate{"baseServerUrl": "https://changed.example"})
	require.ErrorIs(t, err, common.ErrValidation)
	require.ErrorContains(t, err, "UI configuration is disabled")
	err = svc.UpdateSettingValues(ctx, []libarcane.SettingUpdate{{Key: "pollingInterval", Value: "0 */10 * * * *"}})
	require.ErrorIs(t, err, common.ErrValidation)

	err = svc.UpdateSettings(ctx, types.SettingsUpdate{"baseServerUrl": "http://localhost"})
	require.NoError(t, err)
	require.NoError(t, svc.SetStringSetting(ctx, "agentToken", "token"))
}

func TestSettingsService_GetSetHelpers(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	// Defaults for missing keys
	require.False(t, svc.GetBoolSetting(ctx, "nonexistentBool"))
	require.Zero(t, svc.GetIntSetting(ctx, "nonexistentInt"))
	require.Empty(t, svc.GetStringSetting(ctx, "nonexistentStr"))

	// Set and read back
	require.NoError(t, svc.SetBoolSetting(ctx, "enableGravatar", true))
	require.True(t, svc.GetBoolSetting(ctx, "enableGravatar"))

	require.NoError(t, svc.SetIntSetting(ctx, "authSessionTimeout", 123))
	require.Equal(t, 123, svc.GetIntSetting(ctx, "authSessionTimeout"))

	require.NoError(t, svc.SetStringSetting(ctx, "baseServerUrl", "http://localhost"))
	require.Equal(t, "http://localhost", svc.GetStringSetting(ctx, "baseServerUrl"))
}

func TestSettingsService_UpdateSetting(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	require.NoError(t, svc.UpdateSetting(ctx, "pruneImageMode", "all"))

	var sv SettingVariable
	require.NoError(t, svc.db.WithContext(ctx).Where("key = ?", "pruneImageMode").First(&sv).Error)
	require.Equal(t, "all", sv.Value)
}

func TestSettingsService_UpdateSettingRejectsInvalidCronBeforePersistence(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	require.NoError(t, svc.UpdateSetting(ctx, "pollingInterval", "*/5 * * * * *"))
	require.Error(t, svc.UpdateSetting(ctx, "pollingInterval", "not a cron schedule"))

	var setting SettingVariable
	require.NoError(t, svc.db.WithContext(ctx).Where("key = ?", "pollingInterval").First(&setting).Error)
	require.Equal(t, "*/5 * * * * *", setting.Value)
}

func TestSettingsService_UpdateSetting_RefreshesCachedSnapshot(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	require.Equal(t, "http://localhost", svc.GetSettingsConfig().BaseServerURL.Value)
	require.NoError(t, svc.UpdateSetting(ctx, "baseServerUrl", "https://arcane.test"))

	require.Equal(t, "https://arcane.test", svc.GetSettingsConfig().BaseServerURL.Value)

	settingsCfg, err := svc.GetSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, "https://arcane.test", settingsCfg.BaseServerURL.Value)
}

func TestSettingsService_UpdateSettings_PruneModesDoNotTriggerScheduledPruneCallback(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	callbackCalls := 0
	svc.SubscribeSettingsChanges([]string{"scheduledPruneEnabled", "scheduledPruneInterval"}, func([]libarcane.SettingUpdate) {
		callbackCalls++
	})

	err = svc.UpdateSettings(ctx, types.SettingsUpdate{
		"pruneImageMode":      "all",
		"pruneContainerUntil": "24h",
	})
	require.NoError(t, err)
	waitForSettingsNotifications(t, svc)
	require.Equal(t, 0, callbackCalls)
}

func TestSettingsService_UpdateSettings_ScheduledPruneScheduleTriggersCallback(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	callbackCalls := 0
	svc.SubscribeSettingsChanges([]string{"scheduledPruneEnabled", "scheduledPruneInterval"}, func([]libarcane.SettingUpdate) {
		callbackCalls++
	})

	err = svc.UpdateSettings(ctx, types.SettingsUpdate{
		"scheduledPruneEnabled": "true",
	})
	require.NoError(t, err)
	waitForSettingsNotifications(t, svc)
	require.Equal(t, 1, callbackCalls)
}

func TestSettingsServiceActorSerializesConcurrentUpdates(t *testing.T) {
	ctx := t.Context()
	projectsDir := t.TempDir()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	start := make(chan struct{})
	results := make(chan error, 2)
	go func() {
		<-start
		updateErr := svc.UpdateSettings(ctx, types.SettingsUpdate{"projectsDirectory": projectsDir})
		results <- updateErr
	}()
	go func() {
		<-start
		updateErr := svc.UpdateSettings(ctx, types.SettingsUpdate{"templatesDirectory": "/data/templates-b"})
		results <- updateErr
	}()
	close(start)

	require.NoError(t, <-results)
	require.NoError(t, <-results)
	current := svc.GetSettingsConfig()
	require.Equal(t, projectsDir, current.ProjectsDirectory.Value)
	require.Equal(t, "/data/templates-b", current.TemplatesDirectory.Value)
}

func TestSettingsServiceActorPublishesSnapshotBeforeAsynchronousNotification(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	callbackStarted := make(chan string, 1)
	releaseCallback := make(chan struct{})
	svc.SubscribeSettingsChanges([]string{"baseServerUrl"}, func(_ []libarcane.SettingUpdate) {
		callbackStarted <- svc.GetSettingsConfig().BaseServerURL.Value
		<-releaseCallback
	})

	updateResult := make(chan error, 1)
	go func() {
		updateErr := svc.UpdateSettings(ctx, types.SettingsUpdate{"baseServerUrl": "https://actor.example"})
		updateResult <- updateErr
	}()

	require.Equal(t, "https://actor.example", <-callbackStarted)
	select {
	case updateErr := <-updateResult:
		require.NoError(t, updateErr)
	case <-time.After(time.Second):
		require.FailNow(t, "settings update waited for notification callback")
	}

	secondUpdate := make(chan error, 1)
	go func() {
		updateErr := svc.UpdateSettings(ctx, types.SettingsUpdate{"projectsDirectory": t.TempDir()})
		secondUpdate <- updateErr
	}()
	select {
	case secondUpdateErr := <-secondUpdate:
		require.NoError(t, secondUpdateErr)
	case <-time.After(time.Second):
		require.FailNow(t, "settings subscriber blocked the next settings writer")
	}
	close(releaseCallback)
}

func TestSettingsServiceActorNotifiesSubscriberOnceForMultipleMatchingKeys(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	var calls atomic.Int32
	notified := make(chan []libarcane.SettingUpdate, 1)
	svc.SubscribeSettingsChanges([]string{"pollingEnabled", "pollingInterval"}, func(updates []libarcane.SettingUpdate) {
		calls.Add(1)
		notified <- updates
	})

	err = svc.UpdateSettings(ctx, types.SettingsUpdate{
		"pollingEnabled":  "false",
		"pollingInterval": "0 */5 * * * *",
	})
	require.NoError(t, err)
	require.Len(t, <-notified, 2)
	require.Equal(t, int32(1), calls.Load())
}

func TestSettingsServiceActorDoesNotPublishSensitiveValues(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	var calls atomic.Int32
	svc.SubscribeSettingsChanges([]string{"oidcClientSecret"}, func([]libarcane.SettingUpdate) {
		calls.Add(1)
	})
	err = svc.UpdateSettings(ctx, types.SettingsUpdate{"oidcClientSecret": "should-not-leave-settings-service"})
	require.NoError(t, err)
	waitForSettingsNotifications(t, svc)
	require.Zero(t, calls.Load())
}

func TestSettingsServiceActorSerializesContainerExclusionUpdates(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	start := make(chan struct{})
	results := make(chan error, 2)
	for _, name := range []string{"api", "worker"} {
		go func() {
			<-start
			results <- svc.SetContainerAutoUpdateExclusion(ctx, name, true)
		}()
	}
	close(start)

	require.NoError(t, <-results)
	require.NoError(t, <-results)
	require.ElementsMatch(t, []string{"api", "worker"}, strings.Split(svc.GetStringSetting(ctx, "autoUpdateExcludedContainers"), ","))
}

func BenchmarkSettingsService_GetSettings(b *testing.B) {
	ctx := b.Context()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		require.FailNowf(b, "benchmark database setup failed", "%v", err)
	}
	if autoMigrateErr := db.AutoMigrate(&SettingVariable{}); autoMigrateErr != nil {
		require.FailNowf(b, "benchmark database migration failed", "%v", autoMigrateErr)
	}
	settingsDB := &database.DB{DB: db}
	svc, err := newSettingsServiceForTest(b, ctx, settingsDB)
	if err != nil {
		require.FailNowf(b, "benchmark service setup failed", "%v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		settingsCfg, getSettingsErr := svc.GetSettings(ctx)
		if getSettingsErr != nil {
			require.FailNowf(b, "benchmark GetSettings failed", "%v", getSettingsErr)
		}
		if settingsCfg == nil {
			require.FailNow(b, "GetSettings returned nil settings")
		}
	}
}

func TestResolveEncryptionKey(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	envKey, err := ResolveEncryptionKey(ctx, db, &config.Config{EncryptionKey: "env-key", Environment: config.AppEnvironmentDevelopment})
	require.NoError(t, err)
	require.Equal(t, "env-key", envKey)

	prodKey, err := ResolveEncryptionKey(ctx, db, &config.Config{Environment: config.AppEnvironmentProduction})
	require.NoError(t, err)
	require.Empty(t, prodKey)
	require.Zero(t, db.WithContext(ctx).Where("key = ?", "encryptionKey").Find(&[]SettingVariable{}).RowsAffected)

	k1, err := ResolveEncryptionKey(ctx, db, &config.Config{Environment: config.AppEnvironmentDevelopment})
	require.NoError(t, err)
	require.NotEmpty(t, k1)

	k2, err := ResolveEncryptionKey(ctx, db, &config.Config{AgentMode: true, Environment: config.AppEnvironmentProduction})
	require.NoError(t, err)
	require.Equal(t, k1, k2, "encryption key should be stable between calls")

	var sv SettingVariable
	require.NoError(t, svc.db.WithContext(ctx).Where("key = ?", "encryptionKey").First(&sv).Error)
	require.Equal(t, k1, sv.Value)
	crypto.InitEncryption(&crypto.Config{EncryptionKey: k1, Environment: "development"})

	browserKey1, err := svc.EnsureBrowserSessionSigningKey(ctx)
	require.NoError(t, err)
	require.Len(t, browserKey1, browserSessionSigningKeySize)
	browserKey2, err := svc.EnsureBrowserSessionSigningKey(ctx)
	require.NoError(t, err)
	require.Equal(t, browserKey1, browserKey2, "browser signing key should be stable between calls")

	var browserSetting SettingVariable
	require.NoError(t, svc.db.WithContext(ctx).Where("key = ?", "browserSessionSigningKey").First(&browserSetting).Error)
	require.NotEmpty(t, browserSetting.Value)
	require.NotEqual(t, base64.StdEncoding.EncodeToString(browserKey1), browserSetting.Value)
}

func TestSettingsService_AgentTokenUsesStoredTokenWhenEnvUnset(t *testing.T) {
	ctx := t.Context()
	svc, err := newSettingsServiceForTest(t, ctx, setupSettingsTestDB(t))
	require.NoError(t, err)
	require.Empty(t, svc.AgentToken(ctx))

	paired, err := svc.PairAgentToken(ctx, false)
	require.NoError(t, err)
	require.NotEmpty(t, paired)
	require.Equal(t, paired, svc.AgentToken(ctx))

	same, err := svc.PairAgentToken(ctx, false)
	require.NoError(t, err)
	require.Equal(t, paired, same)

	rotated, err := svc.PairAgentToken(ctx, true)
	require.NoError(t, err)
	require.NotEqual(t, paired, rotated)
	require.Equal(t, rotated, svc.AgentToken(ctx))
}

func TestSettingsService_AgentTokenPrefersEnv(t *testing.T) {
	t.Setenv("ARCANE_AGENT_TOKEN", "env-agent-token")
	ctx := t.Context()
	svc, err := newSettingsServiceForTest(t, ctx, setupSettingsTestDB(t))
	require.NoError(t, err)
	require.NoError(t, svc.UpdateSetting(ctx, "agentToken", "stored-agent-token"))

	require.Equal(t, "env-agent-token", svc.AgentToken(ctx))

	paired, err := svc.PairAgentToken(ctx, false)
	require.NoError(t, err)
	require.Equal(t, "env-agent-token", paired)

	_, err = svc.PairAgentToken(ctx, true)
	require.ErrorIs(t, err, common.ErrAgentTokenEnvManaged)
	require.Equal(t, "stored-agent-token", svc.GetStringSetting(ctx, "agentToken"))
}

func TestSettingsService_LoadDatabaseSettings_ReloadsChanges(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	// Initially empty DB -> defaults (not persisted yet)

	// Update a value directly in DB
	require.NoError(t, svc.UpdateSetting(ctx, "projectsDirectory", "custom/projects"))

	// Force reload
	require.NoError(t, svc.LoadDatabaseSettings(ctx))

	cfg := svc.GetSettingsConfig()
	require.Equal(t, "custom/projects", cfg.ProjectsDirectory.Value)
}

func TestSettingsService_LoadDatabaseSettings_UIConfigurationDisabled_Env(t *testing.T) {
	// Set env + disable flag BEFORE service init
	t.Setenv("ARCANE_UI_CONFIGURATION_DISABLED", "true")
	t.Setenv("ARCANE_PROJECTS_DIRECTORY", "env/projects")
	// One value as a variable, one as a Docker secret file.
	baseURLFile := filepath.Join(t.TempDir(), "base-server-url")
	require.NoError(t, os.WriteFile(baseURLFile, []byte("https://env.example\n"), 0o600))
	t.Setenv("ARCANE_BASE_SERVER_URL_FILE", baseURLFile)

	ctx := t.Context()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	// Reload explicitly (NewSettingsService already did, but explicit for clarity)
	require.NoError(t, svc.LoadDatabaseSettings(ctx))

	cfg := svc.GetSettingsConfig()
	require.Equal(t, "env/projects", cfg.ProjectsDirectory.Value)
	require.Equal(t, "https://env.example", cfg.BaseServerURL.Value)
}

func TestSettingsService_UpdateSettings_RefreshesCache(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	newDir := t.TempDir()
	req := types.SettingsUpdate{
		"projectsDirectory": newDir,
	}

	err = svc.UpdateSettings(ctx, req)
	require.NoError(t, err)

	require.True(t, slices.ContainsFunc(svc.SettingsSnapshot(SettingVisibilityAll).Settings, func(state types.SettingState) bool {
		return state.Key == "projectsDirectory" && state.Value == newDir && state.Source == string(SourceDB)
	}), "projectsDirectory setting not found in cached snapshot")
}

func TestSettingsService_UpdateSettings_ReturnsEnvOverriddenValues(t *testing.T) {
	t.Setenv("DOCKER_HOST", "tcp://env-docker:2375")

	ctx := t.Context()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	err = svc.UpdateSettings(ctx, types.SettingsUpdate{
		"projectsDirectory": t.TempDir(),
	})
	require.NoError(t, err)

	require.True(t, slices.ContainsFunc(svc.SettingsSnapshot(SettingVisibilityNonAdmin).Settings, func(state types.SettingState) bool {
		return state.Key == "dockerHost" && state.Value == "tcp://env-docker:2375"
	}), "dockerHost setting not found in snapshot")
}

func TestSettingsService_SettingsSnapshot(t *testing.T) {
	t.Setenv("DOCKER_HOST", "tcp://env-docker:2375")

	ctx := t.Context()
	svc, err := newSettingsServiceForTest(t, ctx, setupSettingsTestDB(t))
	require.NoError(t, err)
	require.NoError(t, svc.UpdateSettings(ctx, types.SettingsUpdate{
		"baseServerUrl":    "https://arcane.example",
		"oidcClientSecret": "client-secret",
	}))

	snapshot := svc.SettingsSnapshot(SettingVisibilityAll)
	states := make(map[string]types.SettingState, len(snapshot.Settings))
	for _, state := range snapshot.Settings {
		states[state.Key] = state
	}

	require.False(t, snapshot.ReadOnly)
	require.Equal(t, types.SettingState{
		Key:        "dockerHost",
		Value:      "tcp://env-docker:2375",
		Type:       "text",
		Default:    "unix:///var/run/docker.sock",
		Source:     string(SourceEnv),
		Locked:     true,
		LockReason: string(LockReasonEnv),
		Scope:      "environment",
		EnvName:    "DOCKER_HOST",
	}, states["dockerHost"])
	require.Equal(t, types.SettingState{
		Key:     "baseServerUrl",
		Value:   "https://arcane.example",
		Type:    "text",
		Default: "http://localhost",
		Source:  string(SourceDB),
		Scope:   "environment",
		EnvName: "ARCANE_BASE_SERVER_URL",
	}, states["baseServerUrl"])
	require.Equal(t, types.SettingState{
		Key:       "oidcClientSecret",
		Value:     redactionMask,
		Type:      "password",
		Source:    string(SourceDB),
		Sensitive: true,
		Scope:     "manager",
		EnvName:   "ARCANE_OIDC_CLIENT_SECRET",
	}, states["oidcClientSecret"])
	require.Equal(t, string(SourceDefault), states["defaultShell"].Source)
	require.NotContains(t, states, "agentToken")
	require.NotContains(t, states, "notificationsCategory")

	nonAdminKeys := []string{}
	for _, state := range svc.SettingsSnapshot(SettingVisibilityNonAdmin).Settings {
		nonAdminKeys = append(nonAdminKeys, state.Key)
	}
	require.Contains(t, nonAdminKeys, "dockerHost")
	require.NotContains(t, nonAdminKeys, "oidcClientSecret")
	require.NotContains(t, nonAdminKeys, "baseServerUrl")
}

func TestSettingsService_UpdateSettings_MaskedAndInternalKeys(t *testing.T) {
	ctx := t.Context()
	svc, err := newSettingsServiceForTest(t, ctx, setupSettingsTestDB(t))
	require.NoError(t, err)
	require.NoError(t, svc.UpdateSettings(ctx, types.SettingsUpdate{"depotToken": "depot-secret"}))

	require.NoError(t, svc.UpdateSettings(ctx, types.SettingsUpdate{
		"depotToken":     redactionMask,
		"depotProjectId": "project",
	}))
	require.Equal(t, "depot-secret", svc.GetSettingsConfig().DepotToken.Value)
	require.Equal(t, "project", svc.GetSettingsConfig().DepotProjectId.Value)

	for _, key := range []string{"agentToken", "notARealSetting"} {
		err = svc.UpdateSettings(ctx, types.SettingsUpdate{key: "value"})
		require.ErrorIs(t, err, common.ErrValidation)
		fieldErr, ok := errors.AsType[*types.FieldError](err)
		require.True(t, ok)
		require.Equal(t, key, fieldErr.Field)
	}
	require.Empty(t, svc.GetSettingsConfig().AgentToken.Value)
}

func TestSettingsService_UpdateSettings_TimeoutCallbackIncludesTrivyScanTimeout(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	var callbackPayload []libarcane.SettingUpdate
	svc.SubscribeSettingsChanges(PropagatedKeys(), func(timeoutSettings []libarcane.SettingUpdate) {
		callbackPayload = timeoutSettings
	})

	err = svc.UpdateSettings(ctx, types.SettingsUpdate{"trivyScanTimeout": "1200"})
	require.NoError(t, err)
	waitForSettingsNotifications(t, svc)

	require.NotNil(t, callbackPayload)
	require.Contains(t, callbackPayload, libarcane.SettingUpdate{Key: "trivyScanTimeout", Value: "1200"})
}

func TestSettingsService_UpdateSettings_TimeoutCallbackIncludesTrivyResourceLimits(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	var callbackPayload []libarcane.SettingUpdate
	svc.SubscribeSettingsChanges(PropagatedKeys(), func(timeoutSettings []libarcane.SettingUpdate) {
		callbackPayload = timeoutSettings
	})

	err = svc.UpdateSettings(ctx, types.SettingsUpdate{
		"trivyResourceLimitsEnabled": "false",
		"trivyCpuLimit":              "2.5",
		"trivyMemoryLimitMb":         "3072",
	})
	require.NoError(t, err)
	waitForSettingsNotifications(t, svc)

	require.NotNil(t, callbackPayload)
	require.Contains(t, callbackPayload, libarcane.SettingUpdate{Key: "trivyResourceLimitsEnabled", Value: "false"})
	require.Contains(t, callbackPayload, libarcane.SettingUpdate{Key: "trivyCpuLimit", Value: "2.5"})
	require.Contains(t, callbackPayload, libarcane.SettingUpdate{Key: "trivyMemoryLimitMb", Value: "3072"})
}

func TestSettingsService_UpdateSettings_TimeoutCallbackIncludesTrivyConcurrentScanContainers(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	var callbackPayload []libarcane.SettingUpdate
	svc.SubscribeSettingsChanges(PropagatedKeys(), func(timeoutSettings []libarcane.SettingUpdate) {
		callbackPayload = timeoutSettings
	})

	err = svc.UpdateSettings(ctx, types.SettingsUpdate{"trivyConcurrentScanContainers": "4"})
	require.NoError(t, err)
	waitForSettingsNotifications(t, svc)

	require.NotNil(t, callbackPayload)
	require.Contains(t, callbackPayload, libarcane.SettingUpdate{Key: "trivyConcurrentScanContainers", Value: "4"})
}

func TestSettingsService_UpdateSettings_TrivyNetworkTriggersVulnerabilityCallback(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	callbackCalled := false
	svc.SubscribeSettingsChanges([]string{"trivyNetwork"}, func(_ []libarcane.SettingUpdate) {
		callbackCalled = true
	})

	err = svc.UpdateSettings(ctx, types.SettingsUpdate{"trivyNetwork": "arcane-external"})
	require.NoError(t, err)
	waitForSettingsNotifications(t, svc)
	require.True(t, callbackCalled)
}

func TestSettingsService_UpdateSettings_TrivyNetworkDoesNotTriggerTimeoutCallback(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	var callbackPayload []libarcane.SettingUpdate
	svc.SubscribeSettingsChanges(PropagatedKeys(), func(timeoutSettings []libarcane.SettingUpdate) {
		callbackPayload = timeoutSettings
	})

	err = svc.UpdateSettings(ctx, types.SettingsUpdate{"trivyNetwork": "arcane-external"})
	require.NoError(t, err)
	waitForSettingsNotifications(t, svc)
	require.Nil(t, callbackPayload)
}

func TestSettingsService_UpdateSettings_TrivyRuntimeSecurityTriggersVulnerabilityCallback(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	callbackCalled := false
	svc.SubscribeSettingsChanges([]string{"trivySecurityOpts", "trivyPrivileged"}, func(_ []libarcane.SettingUpdate) {
		callbackCalled = true
	})

	err = svc.UpdateSettings(ctx, types.SettingsUpdate{
		"trivySecurityOpts": "label=disable",
		"trivyPrivileged":   "true",
	})
	require.NoError(t, err)
	waitForSettingsNotifications(t, svc)
	require.True(t, callbackCalled)
}

func TestSettingsService_UpdateSettings_TrivyRuntimeSecurityDoesNotTriggerTimeoutCallback(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	var callbackPayload []libarcane.SettingUpdate
	svc.SubscribeSettingsChanges(PropagatedKeys(), func(timeoutSettings []libarcane.SettingUpdate) {
		callbackPayload = timeoutSettings
	})

	err = svc.UpdateSettings(ctx, types.SettingsUpdate{
		"trivySecurityOpts": "label=disable",
		"trivyPrivileged":   "true",
	})
	require.NoError(t, err)
	waitForSettingsNotifications(t, svc)
	require.Nil(t, callbackPayload)
}

func TestSettingsService_LoadDatabaseSettings_InternalKeys_EnvMode(t *testing.T) {
	// Set env + disable flag
	t.Setenv("ARCANE_UI_CONFIGURATION_DISABLED", "true")

	ctx := t.Context()
	db := setupSettingsTestDB(t)

	// Pre-populate an internal setting in the DB
	internalKey := "instanceId"
	internalVal := "test-instance-id"
	require.NoError(t, db.DB.Create(&SettingVariable{Key: internalKey, Value: internalVal}).Error)

	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	// Reload explicitly to trigger the env loading path
	require.NoError(t, svc.LoadDatabaseSettings(ctx))

	cfg := svc.GetSettingsConfig()
	// Should have loaded the internal setting from DB even in env mode
	require.Equal(t, internalVal, cfg.InstanceID.Value)
}

func TestSettingsService_NormalizeProjectsDirectory_ConvertsRelativeToAbsolute(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	// Seed with relative path
	require.NoError(t, svc.UpdateSetting(ctx, "projectsDirectory", "data/projects"))

	// Run normalization without env var set (empty string)
	err = svc.NormalizeProjectsDirectory(ctx)
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
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	// Seed with relative path
	require.NoError(t, svc.UpdateSetting(ctx, "projectsDirectory", "data/projects"))

	// Run normalization WITH env var set
	t.Setenv("ARCANE_PROJECTS_DIRECTORY", "/custom/env/path")
	envSvc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, envSvc.NormalizeProjectsDirectory(ctx))

	// Verify it was NOT changed
	var setting SettingVariable
	require.NoError(t, svc.db.WithContext(ctx).Where("key = ?", "projectsDirectory").First(&setting).Error)
	require.Equal(t, "data/projects", setting.Value, "should not change when env var is set")
}

func TestSettingsService_NormalizeProjectsDirectory_LeavesOtherPathsUnchanged(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	customPath := "/custom/projects/path"
	require.NoError(t, svc.UpdateSetting(ctx, "projectsDirectory", customPath))

	// Run normalization
	err = svc.NormalizeProjectsDirectory(ctx)
	require.NoError(t, err)

	// Verify it was NOT changed
	var setting SettingVariable
	require.NoError(t, svc.db.WithContext(ctx).Where("key = ?", "projectsDirectory").First(&setting).Error)
	require.Equal(t, customPath, setting.Value, "should not change custom paths")
}

func TestSettingsService_NormalizeProjectsDirectory_HandlesNotFound(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	// Don't create the setting at all

	// Run normalization - should not error
	err = svc.NormalizeProjectsDirectory(ctx)
	require.NoError(t, err)
}

func TestSettingsService_NormalizeProjectsDirectory_UpdatesCacheAfterNormalization(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	// Set to relative path
	require.NoError(t, svc.UpdateSetting(ctx, "projectsDirectory", "data/projects"))
	require.NoError(t, svc.LoadDatabaseSettings(ctx))

	// Verify cache has relative path
	cfg1 := svc.GetSettingsConfig()
	require.Equal(t, "data/projects", cfg1.ProjectsDirectory.Value)

	// Run normalization
	err = svc.NormalizeProjectsDirectory(ctx)
	require.NoError(t, err)

	// Verify cache was updated to absolute path
	cfg2 := svc.GetSettingsConfig()
	expectedPath, _ := filepath.Abs("data/projects")
	require.Equal(t, expectedPath, cfg2.ProjectsDirectory.Value)
	require.True(t, filepath.IsAbs(cfg2.ProjectsDirectory.Value), "path should be absolute")
}

func TestSettingsService_NormalizeProjectsDirectoryPublishesChange(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)
	require.NoError(t, svc.UpdateSetting(ctx, "projectsDirectory", "data/projects"))

	notified := make(chan []libarcane.SettingUpdate, 1)
	unsubscribe := svc.SubscribeSettingsChanges([]string{"projectsDirectory"}, func(updates []libarcane.SettingUpdate) {
		notified <- updates
	})
	defer unsubscribe()

	require.NoError(t, svc.NormalizeProjectsDirectory(ctx))
	waitForSettingsNotifications(t, svc)
	updates := <-notified
	require.Len(t, updates, 1)
	require.Equal(t, "projectsDirectory", updates[0].Key)
	require.True(t, filepath.IsAbs(updates[0].Value))
}

// TestSettingsServiceEffectiveSnapshotMaterialized verifies reads share one env-override-applied
// snapshot per refresh, and an update rebuilds it with the override still applied.
func TestSettingsServiceEffectiveSnapshotMaterialized(t *testing.T) {
	ctx := t.Context()
	t.Setenv("ARCANE_PROJECTS_DIRECTORY", "/env/projects")

	db := setupSettingsTestDB(t)
	require.NoError(t, db.Create(&SettingVariable{Key: "projectsDirectory", Value: "/db/projects"}).Error)
	require.NoError(t, db.Create(&SettingVariable{Key: "baseServerUrl", Value: "http://before.example"}).Error)

	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	// Env override beats the database value on the effective snapshot.
	require.Equal(t, "/env/projects", svc.GetStringSetting(ctx, "projectsDirectory"))

	// Reads share the materialized snapshot instead of cloning per call.
	first, err := svc.GetSettings(ctx)
	require.NoError(t, err)
	again, err := svc.GetSettings(ctx)
	require.NoError(t, err)
	require.Same(t, first, again)

	// A settings update rebuilds the snapshot: the new value is visible and
	// the env override is still applied.
	require.NoError(t, svc.UpdateSetting(ctx, "baseServerUrl", "http://after.example"))
	require.Equal(t, "http://after.example", svc.GetStringSetting(ctx, "baseServerUrl"))
	require.Equal(t, "/env/projects", svc.GetStringSetting(ctx, "projectsDirectory"))

	refreshed, err := svc.GetSettings(ctx)
	require.NoError(t, err)
	require.NotSame(t, first, refreshed)
}

func TestSettingsService_FeatureDefaultsAndUnknownFeature(t *testing.T) {
	var svc *SettingsService
	require.True(t, svc.IsFeatureEnabled(t.Context(), types.FeatureVulnerabilityManagement))
	require.NoError(t, svc.RequireFeature(t.Context(), types.FeatureVulnerabilityManagement))
	require.False(t, svc.IsFeatureEnabled(t.Context(), types.FeatureID("unknown")))

	svc, err := newSettingsServiceForTest(t, t.Context(), setupSettingsTestDB(t))
	require.NoError(t, err)
	require.True(t, svc.IsFeatureEnabled(t.Context(), types.FeatureVulnerabilityManagement))
	require.True(t, slices.ContainsFunc(svc.SettingsSnapshot(SettingVisibilityPublic).Settings, func(state types.SettingState) bool {
		return state.Key == types.FeatureVulnerabilityManagementSettingKey && state.Value == "true"
	}))
}

func TestSettingsService_FeaturePersistenceAndEnvironmentIsolation(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)
	other, err := newSettingsServiceForTest(t, ctx, setupSettingsTestDB(t))
	require.NoError(t, err)

	var notifications int
	unsubscribe := svc.SubscribeSettingsChanges([]string{types.FeatureVulnerabilityManagementSettingKey}, func([]libarcane.SettingUpdate) { notifications++ })
	defer unsubscribe()
	scheduled := "true"
	ignore := "CVE-2026-12345"
	require.NoError(t, svc.UpdateSetting(ctx, "trivyIgnore", ignore))
	err = svc.UpdateSettings(ctx, types.SettingsUpdate{"vulnerabilityScanEnabled": scheduled})
	require.NoError(t, err)
	disabled := "false"
	err = svc.UpdateSettings(ctx, types.SettingsUpdate{"featureVulnerabilityManagementEnabled": disabled})
	require.NoError(t, err)
	waitForSettingsNotifications(t, svc)
	require.Equal(t, 1, notifications)
	require.False(t, svc.IsFeatureEnabled(ctx, types.FeatureVulnerabilityManagement))
	require.True(t, other.IsFeatureEnabled(ctx, types.FeatureVulnerabilityManagement))
	err = svc.RequireFeature(ctx, types.FeatureVulnerabilityManagement)
	require.ErrorIs(t, err, common.ErrFeatureDisabled)
	require.ErrorIs(t, err, common.ErrForbidden)
	require.Contains(t, err.Error(), string(types.FeatureVulnerabilityManagement))
	apiErr := common.ToAPIError(err)
	require.Equal(t, http.StatusForbidden, apiErr.HTTPStatus())
	require.Equal(t, common.APIErrorCodeFeatureDisabled, apiErr.Code)

	reloaded, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)
	require.False(t, reloaded.IsFeatureEnabled(ctx, types.FeatureVulnerabilityManagement))
	enabled := "true"
	err = svc.UpdateSettings(ctx, types.SettingsUpdate{"featureVulnerabilityManagementEnabled": enabled})
	require.NoError(t, err)
	require.True(t, svc.IsFeatureEnabled(ctx, types.FeatureVulnerabilityManagement))
	cfg, err := svc.GetSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, scheduled, cfg.VulnerabilityScanEnabled.Value)
	require.Equal(t, ignore, cfg.TrivyIgnore.Value)
}

func TestSettingsService_FeatureEnvironmentOverride(t *testing.T) {
	t.Setenv("ARCANE_FEATURE_VULNERABILITY_MANAGEMENT_ENABLED", "false")
	svc, err := newSettingsServiceForTest(t, t.Context(), setupSettingsTestDB(t))
	require.NoError(t, err)
	enabled := "true"
	err = svc.UpdateSettings(t.Context(), types.SettingsUpdate{"featureVulnerabilityManagementEnabled": enabled})
	require.ErrorIs(t, err, common.ErrValidation)
	_, reason := svc.IsLocked(types.FeatureVulnerabilityManagementSettingKey)
	require.Equal(t, LockReasonEnv, reason)
	require.False(t, svc.IsFeatureEnabled(t.Context(), types.FeatureVulnerabilityManagement))
	require.True(t, slices.ContainsFunc(svc.SettingsSnapshot(SettingVisibilityPublic).Settings, func(state types.SettingState) bool {
		return state.Key == types.FeatureVulnerabilityManagementSettingKey && state.Value == "false"
	}))
}

func TestSettingsService_RejectInvalidFeatureBoolean(t *testing.T) {
	for _, invalid := range []string{"", "yes", "0", "TRUE"} {
		t.Run(invalid, func(t *testing.T) {
			svc, err := newSettingsServiceForTest(t, t.Context(), setupSettingsTestDB(t))
			require.NoError(t, err)
			err = svc.UpdateSettings(t.Context(), types.SettingsUpdate{"featureVulnerabilityManagementEnabled": invalid})
			require.ErrorIs(t, err, common.ErrValidation)
			err = svc.UpdateSetting(t.Context(), types.FeatureVulnerabilityManagementSettingKey, invalid)
			require.ErrorIs(t, err, common.ErrValidation)
			err = svc.UpdateSettingValues(t.Context(), []libarcane.SettingUpdate{{Key: types.FeatureVulnerabilityManagementSettingKey, Value: invalid}})
			require.ErrorIs(t, err, common.ErrValidation)
			require.True(t, svc.IsFeatureEnabled(t.Context(), types.FeatureVulnerabilityManagement))
		})
	}
}

func TestSettingsService_SwarmFeatureDefaultsOffAndPersists(t *testing.T) {
	var nilService *SettingsService
	require.False(t, nilService.IsFeatureEnabled(t.Context(), types.FeatureSwarm))

	svc, err := newSettingsServiceForTest(t, t.Context(), setupSettingsTestDB(t))
	require.NoError(t, err)
	require.False(t, svc.IsFeatureEnabled(t.Context(), types.FeatureSwarm))
	require.True(t, slices.ContainsFunc(svc.SettingsSnapshot(SettingVisibilityPublic).Settings, func(state types.SettingState) bool {
		return state.Key == types.FeatureSwarmSettingKey && state.Value == "false"
	}))

	enabled := "true"
	err = svc.UpdateSettings(t.Context(), types.SettingsUpdate{"featureSwarmEnabled": enabled})
	require.NoError(t, err)
	require.True(t, svc.IsFeatureEnabled(t.Context(), types.FeatureSwarm))

	invalid := "yes"
	err = svc.UpdateSettings(t.Context(), types.SettingsUpdate{"featureSwarmEnabled": invalid})
	require.ErrorIs(t, err, common.ErrValidation)
}

func TestSettingsServiceEncryptsSensitiveSettingsAtRest(t *testing.T) {
	t.Setenv("ARCANE_DEPOT_TOKEN", "")
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	require.NoError(t, svc.UpdateSetting(ctx, "oidcClientSecret", "client-secret"))
	var stored SettingVariable
	require.NoError(t, db.WithContext(ctx).Where("key = ?", "oidcClientSecret").First(&stored).Error)
	require.NotEqual(t, "client-secret", stored.Value)
	require.Equal(t, "client-secret", svc.GetSettingsConfig().OidcClientSecret.Value)

	hexToken := strings.Repeat("0123456789abcdef", 4)
	require.NoError(t, db.WithContext(ctx).Save(&SettingVariable{Key: "trivyServerToken", Value: "legacy-token"}).Error)
	require.NoError(t, db.WithContext(ctx).Save(&SettingVariable{Key: "depotToken", Value: hexToken}).Error)
	require.NoError(t, svc.EncryptStoredSensitiveSettings(ctx))
	var storedToken, storedHexToken SettingVariable
	require.NoError(t, db.WithContext(ctx).Where("key = ?", "trivyServerToken").First(&storedToken).Error)
	require.NoError(t, db.WithContext(ctx).Where("key = ?", "depotToken").First(&storedHexToken).Error)
	require.NotEqual(t, "legacy-token", storedToken.Value)
	require.NotEqual(t, hexToken, storedHexToken.Value)
	require.Equal(t, "legacy-token", svc.GetSettingsConfig().TrivyServerToken.Value)
	require.Equal(t, hexToken, svc.GetSettingsConfig().DepotToken.Value)

	var rerunClientSecret SettingVariable
	require.NoError(t, db.WithContext(ctx).Where("key = ?", "oidcClientSecret").First(&rerunClientSecret).Error)
	require.Equal(t, stored.Value, rerunClientSecret.Value)
}

func TestSettingsServiceEncryptsStoredSensitiveSettingsOnlyOnce(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTest(t, ctx, db)
	require.NoError(t, err)

	require.NoError(t, svc.EncryptStoredSensitiveSettings(ctx))
	var marker SettingVariable
	require.NoError(t, db.WithContext(ctx).Where("key = ?", "sensitiveSettingsEncrypted").First(&marker).Error)
	require.Equal(t, "true", marker.Value)

	otherKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	otherEncryptor, err := crypto.New(otherKey)
	require.NoError(t, err)
	foreignCiphertext, err := otherEncryptor.Encrypt("secret-from-another-key")
	require.NoError(t, err)

	require.NoError(t, db.WithContext(ctx).Save(&SettingVariable{Key: "depotToken", Value: foreignCiphertext}).Error)
	require.NoError(t, svc.EncryptStoredSensitiveSettings(ctx))

	var stored SettingVariable
	require.NoError(t, db.WithContext(ctx).Where("key = ?", "depotToken").First(&stored).Error)
	require.Equal(t, foreignCiphertext, stored.Value)
}
