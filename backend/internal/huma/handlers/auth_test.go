package handlers

import (
	"context"
	"testing"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	glsqlite "github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupAuthTestDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := gorm.Open(glsqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.SettingVariable{}, &models.User{}))
	return &database.DB{DB: db}
}

func TestGetAutoLoginConfig_Disabled(t *testing.T) {
	ctx := context.Background()
	db := setupAuthTestDB(t)

	cfg := &config.Config{
		AutoLoginEnable:   false,
		AutoLoginUsername: "testuser",
		AutoLoginPassword: "testpass",
		JWTSecret:         "test-jwt-secret-32-characters!!!!",
	}

	settingsSvc, err := services.NewSettingsService(ctx, db)
	require.NoError(t, err)

	userSvc := services.NewUserService(db)
	authSvc := services.NewAuthService(userSvc, settingsSvc, nil, cfg.JWTSecret, cfg)

	h := &AuthHandler{
		authService: authSvc,
	}

	result, err := h.GetAutoLoginConfig(ctx, &struct{}{})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Body.Success)
	assert.False(t, result.Body.Data.Enabled, "Expected auto-login to be disabled")
	assert.Empty(t, result.Body.Data.Username, "Expected empty username when disabled")
}

func TestGetAutoLoginConfig_Enabled(t *testing.T) {
	ctx := context.Background()
	db := setupAuthTestDB(t)

	cfg := &config.Config{
		AutoLoginEnable:   true,
		AutoLoginUsername: "autologinuser",
		AutoLoginPassword: "autologinpass",
		JWTSecret:         "test-jwt-secret-32-characters!!!!",
	}

	settingsSvc, err := services.NewSettingsService(ctx, db)
	require.NoError(t, err)
	require.NoError(t, settingsSvc.EnsureDefaultSettings(ctx))
	// Ensure local auth is enabled
	require.NoError(t, settingsSvc.SetBoolSetting(ctx, "authLocalEnabled", true))

	userSvc := services.NewUserService(db)
	authSvc := services.NewAuthService(userSvc, settingsSvc, nil, cfg.JWTSecret, cfg)

	h := &AuthHandler{
		authService: authSvc,
	}

	result, err := h.GetAutoLoginConfig(ctx, &struct{}{})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Body.Success)
	assert.True(t, result.Body.Data.Enabled, "Expected auto-login to be enabled")
	assert.Equal(t, "autologinuser", result.Body.Data.Username)
}

func TestAutoLogin_DisabledReturnsError(t *testing.T) {
	ctx := context.Background()
	db := setupAuthTestDB(t)

	cfg := &config.Config{
		AutoLoginEnable:   false,
		AutoLoginUsername: "testuser",
		AutoLoginPassword: "testpass",
		JWTSecret:         "test-jwt-secret-32-characters!!!!",
	}

	settingsSvc, err := services.NewSettingsService(ctx, db)
	require.NoError(t, err)

	userSvc := services.NewUserService(db)
	authSvc := services.NewAuthService(userSvc, settingsSvc, nil, cfg.JWTSecret, cfg)

	h := &AuthHandler{
		authService: authSvc,
	}

	_, err = h.AutoLogin(ctx, &struct{}{})
	require.Error(t, err, "Expected error when auto-login is disabled")
	assert.Contains(t, err.Error(), "auto-login is not enabled")
}

func TestAutoLogin_ServiceNilReturnsError(t *testing.T) {
	ctx := context.Background()

	h := &AuthHandler{
		authService: nil,
	}

	_, err := h.AutoLogin(ctx, &struct{}{})
	require.Error(t, err, "Expected error when service is nil")
	assert.Contains(t, err.Error(), "service not available")
}

func TestGetAutoLoginConfig_ServiceNilReturnsError(t *testing.T) {
	ctx := context.Background()

	h := &AuthHandler{
		authService: nil,
	}

	_, err := h.GetAutoLoginConfig(ctx, &struct{}{})
	require.Error(t, err, "Expected error when service is nil")
	assert.Contains(t, err.Error(), "service not available")
}
