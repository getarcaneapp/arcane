//go:build buildables

package services

import (
	"context"
	"testing"

	"github.com/getarcaneapp/arcane/backend/buildables"
	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/stretchr/testify/require"
)

func enableAutoLoginFeature(t *testing.T) {
	t.Helper()
	original := buildables.EnabledFeatures
	buildables.EnabledFeatures = "autologin"
	t.Cleanup(func() {
		buildables.EnabledFeatures = original
	})
}

func TestGetAutoLoginConfig_Enabled(t *testing.T) {
	enableAutoLoginFeature(t)
	ctx := context.Background()
	db := setupAuthServiceTestDB(t)

	settingsSvc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsSvc.EnsureDefaultSettings(ctx))
	require.NoError(t, settingsSvc.SetBoolSetting(ctx, "authLocalEnabled", true))

	s := newTestAuthService("")
	s.config = &config.Config{
		BuildablesConfig: config.BuildablesConfig{
			AutoLoginUsername: "autologinuser",
			AutoLoginPassword: "autologinpass",
		},
	}
	s.settingsService = settingsSvc

	result, err := s.GetAutoLoginConfig(ctx)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Enabled, "Expected auto-login to be enabled")
	require.Equal(t, "autologinuser", result.Username)
}

func TestGetAutoLoginConfig_DisabledWhenLocalAuthDisabled(t *testing.T) {
	enableAutoLoginFeature(t)
	ctx := context.Background()
	db := setupAuthServiceTestDB(t)

	settingsSvc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsSvc.EnsureDefaultSettings(ctx))
	require.NoError(t, settingsSvc.SetBoolSetting(ctx, "authLocalEnabled", false))

	s := newTestAuthService("")
	s.config = &config.Config{
		BuildablesConfig: config.BuildablesConfig{
			AutoLoginUsername: "autologinuser",
			AutoLoginPassword: "autologinpass",
		},
	}
	s.settingsService = settingsSvc

	result, err := s.GetAutoLoginConfig(ctx)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.Enabled, "Expected auto-login to be disabled when local auth is disabled")
	require.Empty(t, result.Username, "Expected empty username when disabled")
}

func TestGetAutoLoginPassword(t *testing.T) {
	enableAutoLoginFeature(t)
	s := newTestAuthService("")
	s.config = &config.Config{
		BuildablesConfig: config.BuildablesConfig{
			AutoLoginPassword: "my-secret-password",
		},
	}

	password := s.GetAutoLoginPassword()
	require.Equal(t, "my-secret-password", password)
}

func TestGetAutoLoginPassword_EmptyWhenNotConfigured(t *testing.T) {
	enableAutoLoginFeature(t)
	s := newTestAuthService("")
	s.config = &config.Config{
		BuildablesConfig: config.BuildablesConfig{
			AutoLoginPassword: "",
		},
	}

	password := s.GetAutoLoginPassword()
	require.Empty(t, password)
}
