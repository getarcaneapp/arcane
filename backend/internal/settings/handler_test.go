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
	"github.com/stretchr/testify/require"

	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/pkg/agent"
	"getarcane.dev/types/v3"
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
		mtls: agent.NewMTLS(agent.MTLSConfig{}),
	}

	publicRuntime := handler.runtimeSettings(false)
	require.Empty(t, publicRuntime.BackupVolumeName)
	require.False(t, publicRuntime.DepotConfigured)

	authenticatedRuntime := handler.runtimeSettings(true)
	require.Equal(t, "custom-backups", authenticatedRuntime.BackupVolumeName)
	require.False(t, authenticatedRuntime.AgentMTLSManagerCAAvailable)
}

func TestSettingsHandlerRuntimeSettingsDoesNotGenerateAgentMTLSCA(t *testing.T) {
	settingsService, err := newSettingsServiceForTest(t, t.Context(), setupSettingsTestDB(t))
	require.NoError(t, err)
	assetsDir := t.TempDir()
	handler := &SettingsHandler{
		settingsService: settingsService,
		cfg: &config.Config{
			AgentMTLSMode:      agent.MTLSModeRequired,
			AgentMTLSAssetsDir: assetsDir,
		},
		mtls: agent.NewMTLS(agent.MTLSConfig{
			Mode:      agent.MTLSModeRequired,
			AssetsDir: assetsDir,
		}),
	}

	require.False(t, handler.runtimeSettings(true).AgentMTLSManagerCAAvailable)
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
	_, err = handler.UpdateEnvironmentSettings(ctx, &UpdateSettingsInput{EnvironmentID: "0", Body: types.SettingsUpdate{"projectsDirectory": unreadable}})
	require.Error(t, err)

	var statusErr huma.StatusError
	require.ErrorAs(t, err, &statusErr)
	require.Equal(t, http.StatusBadRequest, statusErr.GetStatus())
	require.Contains(t, err.Error(), "cannot read projects directory")
	require.Equal(t, originalDir, settingsService.GetSettingsConfig().ProjectsDirectory.Value)
}

func TestSettingsHandlerRemoteEnvironmentSettingsPassThrough(t *testing.T) {
	remoteSnapshot := types.SettingsSnapshot{
		Settings: []types.SettingState{
			{Key: "dockerHost", Type: "text", Value: "unix:///var/run/docker.sock", Scope: "environment"},
			{Key: "defaultShell", Type: "text", Value: "/bin/bash", Scope: "environment"},
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

	output, err := handler.GetEnvironmentSettings(t.Context(), &types.EnvironmentInput{EnvironmentID: "env-remote"})
	require.NoError(t, err)
	require.Equal(t, remoteSnapshot, output.Body)
}
