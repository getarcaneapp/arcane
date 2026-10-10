package settings

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v2/settings"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/edge"
)

func TestSettingsHandlerAppendRuntimeSettings(t *testing.T) {
	handler := &SettingsHandler{cfg: &config.Config{
		UIConfigurationDisabled:       true,
		BackupVolumeName:              "custom-backups",
		ProjectWorkspaceMaxFileSizeMB: 12,
		VolumeWorkspaceMaxFileSizeMB:  18,
	}}

	publicKeys := runtimeSettingKeys(handler.appendRuntimeSettings(nil, false))
	require.NotContains(t, publicKeys, "uiConfigDisabled")
	require.NotContains(t, publicKeys, "backupVolumeName")
	require.NotContains(t, publicKeys, "depotConfigured")
	require.NotContains(t, publicKeys, "edgeMTLSManagerCAAvailable")

	authenticatedKeys := runtimeSettingKeys(handler.appendRuntimeSettings(nil, true))
	require.Equal(t, "12", authenticatedKeys[projectWorkspaceMaxFileSizeSettingKey])
	require.Equal(t, "18", authenticatedKeys[volumeWorkspaceMaxFileSizeSettingKey])
	require.Equal(t, "true", authenticatedKeys["uiConfigDisabled"])
	require.Equal(t, "custom-backups", authenticatedKeys["backupVolumeName"])
	require.Equal(t, "false", authenticatedKeys["edgeMTLSManagerCAAvailable"])
}

func TestSettingsHandlerAppendRuntimeSettingsDoesNotGenerateEdgeMTLSCA(t *testing.T) {
	assetsDir := t.TempDir()
	handler := &SettingsHandler{cfg: &config.Config{
		EdgeMTLSMode:      edge.EdgeMTLSModeRequired,
		EdgeMTLSAssetsDir: assetsDir,
	}}

	authenticatedKeys := runtimeSettingKeys(handler.appendRuntimeSettings(nil, true))
	require.Equal(t, "false", authenticatedKeys["edgeMTLSManagerCAAvailable"])
	require.NoFileExists(t, filepath.Join(assetsDir, "ca.crt"))
	require.NoFileExists(t, filepath.Join(assetsDir, "ca.key"))
}

func TestSettingsHandlerUpdateLocalEnvironmentRejectsUnreadableProjectsDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission-denied behavior is not portable to Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("test requires a non-root UID to trigger permission-denied on ReadDir")
	}

	ctx := t.Context()
	settingsService, err := newSettingsServiceForTest(t, ctx, setupSettingsTestDB(t))
	require.NoError(t, err)
	originalDir := settingsService.GetSettingsConfig().ProjectsDirectory.Value

	unreadable := t.TempDir()
	require.NoError(t, os.Chmod(unreadable, 0))
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o700) })

	handler := &SettingsHandler{settingsService: settingsService, cfg: &config.Config{}}
	_, err = handler.UpdateSettings(ctx, &UpdateSettingsInput{EnvironmentID: "0", Body: settings.Update{ProjectsDirectory: new(unreadable)}})
	require.Error(t, err)

	var statusErr huma.StatusError
	require.ErrorAs(t, err, &statusErr)
	require.Equal(t, http.StatusBadRequest, statusErr.GetStatus())
	require.Contains(t, err.Error(), "cannot read projects directory")
	require.Equal(t, originalDir, settingsService.GetSettingsConfig().ProjectsDirectory.Value)
}

func TestSettingsHandlerRemoteWorkspaceSettingsVisibility(t *testing.T) {
	remoteSettings := []settings.PublicSetting{
		{Key: "dockerHost", Type: "string", Value: "unix:///var/run/docker.sock"},
		{Key: "baseServerUrl", Type: "string", Value: "https://manager.example"},
		{Key: "defaultShell", Type: "string", Value: "/bin/bash"},
		{Key: projectWorkspaceMaxFileSizeSettingKey, Type: "number", Value: "12"},
		{Key: volumeWorkspaceMaxFileSizeSettingKey, Type: "number", Value: "18"},
		{Key: "futureAdminSetting", Type: "string", Value: "hidden"},
	}
	proxy := func(_ context.Context, _, method, path string, _ []byte, output any) error {
		require.Equal(t, http.MethodGet, method)
		require.Equal(t, "/api/environments/0/settings", path)
		payload, err := json.Marshal(remoteSettings)
		require.NoError(t, err)
		return json.Unmarshal(payload, output)
	}

	settingsService, err := newSettingsServiceForTest(t, t.Context(), setupSettingsTestDB(t))
	require.NoError(t, err)
	handler := &SettingsHandler{settingsService: settingsService, proxyRemoteJSON: proxy}

	permissions := authz.NewPermissionSet()
	permissions.AddEnv("env-remote", authz.PermSettingsRead)
	ctx := context.WithValue(t.Context(), middleware.ContextKeyUserPermissions, permissions)
	output, err := handler.GetSettings(ctx, &GetSettingsInput{EnvironmentID: "env-remote"})
	require.NoError(t, err)
	require.Equal(t, []settings.PublicSetting{remoteSettings[0], remoteSettings[3], remoteSettings[4]}, output.Body)

	adminCtx := context.WithValue(t.Context(), middleware.ContextKeyUserPermissions, authz.SudoPermissionSet())
	output, err = handler.GetSettings(adminCtx, &GetSettingsInput{EnvironmentID: "env-remote"})
	require.NoError(t, err)
	require.Equal(t, remoteSettings, output.Body)
}

func runtimeSettingKeys(publicSettings []settings.PublicSetting) map[string]string {
	keys := make(map[string]string, len(publicSettings))
	for _, setting := range publicSettings {
		keys[setting.Key] = setting.Value
	}
	return keys
}
