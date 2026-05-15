package handlers

import (
	"path/filepath"
	"testing"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/pkg/libarcane/edge"
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

	publicSettings := handler.appendRuntimeSettingsInternal(nil, false)
	publicKeys := runtimeSettingKeysInternal(publicSettings)
	require.NotContains(t, publicKeys, "uiConfigDisabled")
	require.NotContains(t, publicKeys, "backupVolumeName")
	require.NotContains(t, publicKeys, "depotConfigured")
	require.NotContains(t, publicKeys, "edgeMTLSManagerCAAvailable")

	authenticatedSettings := handler.appendRuntimeSettingsInternal(nil, true)
	authenticatedKeys := runtimeSettingKeysInternal(authenticatedSettings)
	require.Contains(t, authenticatedKeys, "uiConfigDisabled")
	require.Contains(t, authenticatedKeys, "backupVolumeName")
	require.Contains(t, authenticatedKeys, "edgeMTLSManagerCAAvailable")
	require.Equal(t, "true", authenticatedKeys["uiConfigDisabled"])
	require.Equal(t, "custom-backups", authenticatedKeys["backupVolumeName"])
	require.Equal(t, "false", authenticatedKeys["edgeMTLSManagerCAAvailable"])
}

func TestSettingsHandler_AppendRuntimeSettings_DoesNotGenerateEdgeMTLSCA(t *testing.T) {
	assetsDir := t.TempDir()
	handler := &SettingsHandler{
		cfg: &config.Config{
			EdgeMTLSMode:      edge.EdgeMTLSModeRequired,
			EdgeMTLSAssetsDir: assetsDir,
		},
	}

	authenticatedSettings := handler.appendRuntimeSettingsInternal(nil, true)
	authenticatedKeys := runtimeSettingKeysInternal(authenticatedSettings)

	require.Equal(t, "false", authenticatedKeys["edgeMTLSManagerCAAvailable"])
	require.NoFileExists(t, filepath.Join(assetsDir, "ca.crt"))
	require.NoFileExists(t, filepath.Join(assetsDir, "ca.key"))
}

func runtimeSettingKeysInternal(settings []apitypes.PublicSetting) map[string]string {
	keys := make(map[string]string, len(settings))
	for _, setting := range settings {
		keys[setting.Key] = setting.Value
	}

	return keys
}
