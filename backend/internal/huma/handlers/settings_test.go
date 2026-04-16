package handlers

import (
	"testing"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	apitypes "github.com/getarcaneapp/arcane/types/settings"
	"github.com/stretchr/testify/require"
)

func TestSettingsHandler_AppendRuntimeSettings(t *testing.T) {
	handler := &SettingsHandler{
		cfg: &config.Config{
			UIConfigurationDisabled: true,
			BackupVolumeName:        "custom-backups",
		},
	}

	publicSettings := handler.appendRuntimeSettings(nil, false)
	publicKeys := runtimeSettingKeysInternal(publicSettings)
	require.NotContains(t, publicKeys, "uiConfigDisabled")
	require.NotContains(t, publicKeys, "backupVolumeName")
	require.NotContains(t, publicKeys, "depotConfigured")

	authenticatedSettings := handler.appendRuntimeSettings(nil, true)
	authenticatedKeys := runtimeSettingKeysInternal(authenticatedSettings)
	require.Contains(t, authenticatedKeys, "uiConfigDisabled")
	require.Contains(t, authenticatedKeys, "backupVolumeName")
	require.Equal(t, "true", authenticatedKeys["uiConfigDisabled"])
	require.Equal(t, "custom-backups", authenticatedKeys["backupVolumeName"])
}

func runtimeSettingKeysInternal(settings []apitypes.PublicSetting) map[string]string {
	keys := make(map[string]string, len(settings))
	for _, setting := range settings {
		keys[setting.Key] = setting.Value
	}

	return keys
}
