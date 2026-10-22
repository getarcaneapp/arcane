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
	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/backend/v3/internal/config"
	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/edge"
)

func TestSettingsHandlerRuntimeSettings(t *testing.T) {
	settingsService, err := newSettingsServiceForTest(t, t.Context(), setupSettingsTestDB(t))
	require.NoError(t, err)
	handler := &SettingsHandler{
		settingsService: settingsService,
		cfg: &config.Config{
			UIConfigurationDisabled: true,
			BackupVolumeName:        "custom-backups",
		},
	}

	publicRuntime := handler.runtimeSettings(false)
	require.Empty(t, publicRuntime.BackupVolumeName)
	require.False(t, publicRuntime.DepotConfigured)

	authenticatedRuntime := handler.runtimeSettings(true)
	require.Equal(t, "custom-backups", authenticatedRuntime.BackupVolumeName)
	require.False(t, authenticatedRuntime.EdgeMTLSManagerCAAvailable)
}

func TestSettingsHandlerRuntimeSettingsDoesNotGenerateEdgeMTLSCA(t *testing.T) {
	settingsService, err := newSettingsServiceForTest(t, t.Context(), setupSettingsTestDB(t))
	require.NoError(t, err)
	assetsDir := t.TempDir()
	handler := &SettingsHandler{
		settingsService: settingsService,
		cfg: &config.Config{
			EdgeMTLSMode:      edge.EdgeMTLSModeRequired,
			EdgeMTLSAssetsDir: assetsDir,
		},
	}

	require.False(t, handler.runtimeSettings(true).EdgeMTLSManagerCAAvailable)
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
	_, err = handler.UpdateSettings(ctx, &UpdateSettingsInput{EnvironmentID: "0", Body: types.SettingsUpdate{"projectsDirectory": unreadable}})
	require.Error(t, err)

	var statusErr huma.StatusError
	require.ErrorAs(t, err, &statusErr)
	require.Equal(t, http.StatusBadRequest, statusErr.GetStatus())
	require.Contains(t, err.Error(), "cannot read projects directory")
	require.Equal(t, originalDir, settingsService.GetSettingsConfig().ProjectsDirectory.Value)
}

func TestSettingsHandlerRemoteWorkspaceSettingsVisibility(t *testing.T) {
	remoteSnapshot := types.SettingsSnapshot{
		Settings: []types.SettingState{
			{Key: "dockerHost", Type: "text", Value: "unix:///var/run/docker.sock"},
			{Key: "baseServerUrl", Type: "text", Value: "https://manager.example"},
			{Key: "defaultShell", Type: "text", Value: "/bin/bash"},
			{Key: "futureAdminSetting", Type: "text", Value: "hidden"},
		},
		Runtime: types.SettingsRuntime{
			BackupVolumeName: "remote-backups",
		},
	}
	proxy := func(_ context.Context, _, method, path string, _ []byte, output any) error {
		require.Equal(t, http.MethodGet, method)
		require.Equal(t, "/api/environments/0/settings", path)
		payload, err := json.Marshal(remoteSnapshot)
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
	require.Equal(t, []types.SettingState{remoteSnapshot.Settings[0]}, output.Body.Settings)
	require.Equal(t, remoteSnapshot.Runtime, output.Body.Runtime)

	adminCtx := context.WithValue(t.Context(), middleware.ContextKeyUserPermissions, authz.SudoPermissionSet())
	output, err = handler.GetSettings(adminCtx, &GetSettingsInput{EnvironmentID: "env-remote"})
	require.NoError(t, err)
	require.Equal(t, remoteSnapshot, output.Body)
}
