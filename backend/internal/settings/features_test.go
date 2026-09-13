package settings

import (
	"context"
	"net/http"
	"testing"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/getarcaneapp/arcane/types/v2/features"
	settingstypes "github.com/getarcaneapp/arcane/types/v2/settings"
	"github.com/stretchr/testify/require"
)

func TestSettingsService_FeatureDefaultsAndUnknownFeature(t *testing.T) {
	var svc *SettingsService
	require.True(t, svc.IsFeatureEnabled(t.Context(), features.VulnerabilityManagement))
	require.NoError(t, svc.RequireFeature(t.Context(), features.VulnerabilityManagement))
	require.False(t, svc.IsFeatureEnabled(t.Context(), features.ID("unknown")))

	svc, err := newSettingsServiceForTestInternal(t, t.Context(), setupSettingsTestDB(t))
	require.NoError(t, err)
	require.True(t, svc.IsFeatureEnabled(t.Context(), features.VulnerabilityManagement))
	require.NoError(t, svc.EnsureDefaultSettings(t.Context()))
	var stored SettingVariable
	require.NoError(t, svc.db.Where("key = ?", features.VulnerabilityManagementSettingKey).First(&stored).Error)
	require.Equal(t, "true", stored.Value)
	require.Contains(t, svc.ListSettings(SettingVisibilityPublic), SettingVariable{Key: features.VulnerabilityManagementSettingKey, Value: "true"})
}

func TestSettingsService_FeaturePersistenceAndEnvironmentIsolation(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	svc, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	other, err := newSettingsServiceForTestInternal(t, ctx, setupSettingsTestDB(t))
	require.NoError(t, err)

	var notifications int
	unsubscribe := svc.SubscribeSettingsChanges([]string{features.VulnerabilityManagementSettingKey}, func([]libarcane.SettingUpdate) { notifications++ })
	defer unsubscribe()
	scheduled := "true"
	ignore := "CVE-2026-12345"
	require.NoError(t, svc.UpdateSetting(ctx, "trivyIgnore", ignore))
	_, err = svc.UpdateSettings(ctx, settingstypes.Update{VulnerabilityScanEnabled: &scheduled})
	require.NoError(t, err)
	disabled := "false"
	_, err = svc.UpdateSettings(ctx, settingstypes.Update{FeatureVulnerabilityManagementEnabled: &disabled})
	require.NoError(t, err)
	waitForSettingsNotificationsInternal(t, svc)
	require.Equal(t, 1, notifications)
	require.False(t, svc.IsFeatureEnabled(ctx, features.VulnerabilityManagement))
	require.True(t, other.IsFeatureEnabled(ctx, features.VulnerabilityManagement))
	err = svc.RequireFeature(ctx, features.VulnerabilityManagement)
	require.ErrorIs(t, err, common.ErrFeatureDisabled)
	require.ErrorIs(t, err, common.ErrForbidden)
	require.Contains(t, err.Error(), string(features.VulnerabilityManagement))
	apiErr := common.ToAPIError(err)
	require.Equal(t, http.StatusForbidden, apiErr.HTTPStatus())
	require.Equal(t, common.APIErrorCodeFeatureDisabled, apiErr.Code)

	reloaded, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)
	require.False(t, reloaded.IsFeatureEnabled(ctx, features.VulnerabilityManagement))
	enabled := "true"
	_, err = svc.UpdateSettings(ctx, settingstypes.Update{FeatureVulnerabilityManagementEnabled: &enabled})
	require.NoError(t, err)
	require.True(t, svc.IsFeatureEnabled(ctx, features.VulnerabilityManagement))
	cfg, err := svc.GetSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, scheduled, cfg.VulnerabilityScanEnabled.Value)
	require.Equal(t, ignore, cfg.TrivyIgnore.Value)
}

func TestSettingsService_FeatureEnvironmentOverride(t *testing.T) {
	t.Setenv("FEATURE_VULNERABILITY_MANAGEMENT_ENABLED", "false")
	svc, err := newSettingsServiceForTestInternal(t, t.Context(), setupSettingsTestDB(t))
	require.NoError(t, err)
	enabled := "true"
	_, err = svc.UpdateSettings(t.Context(), settingstypes.Update{FeatureVulnerabilityManagementEnabled: &enabled})
	require.NoError(t, err)
	require.True(t, svc.IsEnvOverrideActive(features.VulnerabilityManagementSettingKey))
	require.False(t, svc.IsFeatureEnabled(t.Context(), features.VulnerabilityManagement))
	require.Contains(t, svc.ListSettings(SettingVisibilityPublic), SettingVariable{Key: features.VulnerabilityManagementSettingKey, Value: "false"})
}

func TestSettingsService_RejectInvalidFeatureBoolean(t *testing.T) {
	for _, invalid := range []string{"", "yes", "0", "TRUE"} {
		t.Run(invalid, func(t *testing.T) {
			svc, err := newSettingsServiceForTestInternal(t, t.Context(), setupSettingsTestDB(t))
			require.NoError(t, err)
			_, err = svc.UpdateSettings(t.Context(), settingstypes.Update{FeatureVulnerabilityManagementEnabled: &invalid})
			require.ErrorIs(t, err, common.ErrValidation)
			err = svc.UpdateSetting(t.Context(), features.VulnerabilityManagementSettingKey, invalid)
			require.ErrorIs(t, err, common.ErrValidation)
			err = svc.UpdateSettingValues(context.Background(), []libarcane.SettingUpdate{{Key: features.VulnerabilityManagementSettingKey, Value: invalid}})
			require.ErrorIs(t, err, common.ErrValidation)
			require.True(t, svc.IsFeatureEnabled(t.Context(), features.VulnerabilityManagement))
		})
	}
}
