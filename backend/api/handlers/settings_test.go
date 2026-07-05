package handlers

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/edge"
	apitypes "github.com/getarcaneapp/arcane/types/v2/settings"
	sqlite "github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
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

func TestSettingsHandler_UpdateLocalEnvironment_RejectsUnreadableProjectsDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission-denied behavior is not portable to Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("test requires a non-root UID to trigger permission-denied on ReadDir")
	}

	ctx := context.Background()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.SettingVariable{}))

	settingsSvc, err := services.NewSettingsService(ctx, &database.DB{DB: db})
	require.NoError(t, err)

	originalDir := settingsSvc.GetSettingsConfig().ProjectsDirectory.Value

	unreadable := t.TempDir()
	require.NoError(t, os.Chmod(unreadable, 0))
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o700) })

	handler := &SettingsHandler{settingsService: settingsSvc, cfg: &config.Config{}}

	_, err = handler.updateSettingsForLocalEnvironment(ctx, apitypes.Update{ProjectsDirectory: new(unreadable)})
	require.Error(t, err)

	var statusErr huma.StatusError
	require.ErrorAs(t, err, &statusErr, "expected a huma status error")
	require.Equal(t, 400, statusErr.GetStatus())
	require.True(t, strings.Contains(err.Error(), "cannot read projects directory"), "error message should explain the failure: %s", err.Error())

	require.Equal(t, originalDir, settingsSvc.GetSettingsConfig().ProjectsDirectory.Value, "projectsDirectory must not be persisted on validation failure")
}

func runtimeSettingKeysInternal(settings []apitypes.PublicSetting) map[string]string {
	keys := make(map[string]string, len(settings))
	for _, setting := range settings {
		keys[setting.Key] = setting.Value
	}

	return keys
}
