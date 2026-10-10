package appimages

import (
	"bytes"
	"context"
	"fmt"
	"image/png"
	"testing"

	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/resources"
)

func newServiceForVersion(t *testing.T, version string) (*ApplicationImagesService, *settings.SettingsService) {
	t.Helper()
	original := config.Version
	config.Version = version
	t.Cleanup(func() { config.Version = original })

	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gdb.AutoMigrate(&settings.SettingVariable{}))
	settingsSvc, err := settings.NewSettingsService(t.Context(), &database.DB{DB: gdb})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, settingsSvc.Stop(context.WithoutCancel(t.Context()))) })

	return NewApplicationImagesService(resources.FS, settingsSvc), settingsSvc
}

func readEmbedded(t *testing.T, name string) []byte {
	t.Helper()
	data, err := resources.FS.ReadFile(name)
	require.NoError(t, err)
	return data
}

func TestNewApplicationImagesService_StableBuildUsesStandardBranding(t *testing.T) {
	svc, _ := newServiceForVersion(t, "v1.2.3")

	for _, name := range []string{"logo", "logo-animated"} {
		_, mimeType, err := svc.GetImageWithColor(name, "", false)
		require.NoError(t, err)
		assert.Equal(t, "image/svg+xml", mimeType, name)
	}

	data, _, err := svc.GetImageWithColor("favicon", "", false)
	require.NoError(t, err)
	assert.Equal(t, readEmbedded(t, "images/favicon.ico"), data)

	data, _, err = svc.GetImageWithColor("logo-email", "", false)
	require.NoError(t, err)
	assert.Equal(t, readEmbedded(t, "images/logo-email.png"), data)
}

func TestNewApplicationImagesService_NonStableBuildUsesDevelopmentBranding(t *testing.T) {
	svc, _ := newServiceForVersion(t, "1.2.3-next.1")
	mark := readEmbedded(t, "images/development/icon-512x512.png")

	for _, tc := range []struct {
		name  string
		color string
		loop  bool
	}{
		{"logo", "", false},
		{"logo", "#ff0000", false},
		{"logo-animated", "", false},
		{"logo-animated", "#ff0000", true},
	} {
		data, mimeType, err := svc.GetImageWithColor(tc.name, tc.color, tc.loop)
		require.NoError(t, err)
		assert.Equal(t, "image/png", mimeType, tc.name)
		assert.Equal(t, mark, data, tc.name)
	}

	for _, name := range []string{"logo-full", "logo-full-animated"} {
		data, mimeType, err := svc.GetImageWithColor(name, "#ff0000", false)
		require.NoError(t, err)
		assert.Equal(t, "image/svg+xml", mimeType, name)
		assert.Contains(t, string(data), "#ff0000", name)
	}

	data, mimeType, err := svc.GetImageWithColor("favicon", "", false)
	require.NoError(t, err)
	assert.Equal(t, readEmbedded(t, "images/development/favicon.ico"), data)
	assert.Equal(t, []byte{0, 0, 1, 0}, data[:4])
	assert.NotEmpty(t, mimeType)

	data, mimeType, err = svc.GetImageWithColor("logo-email", "", false)
	require.NoError(t, err)
	assert.Equal(t, "image/png", mimeType)
	assert.Equal(t, readEmbedded(t, "images/development/logo-email.png"), data)

	data, _, err = svc.GetImageWithColor("profile", "", false)
	require.NoError(t, err)
	assert.Equal(t, readEmbedded(t, "images/profile.webp"), data)

	for _, size := range []int{72, 96, 128, 144, 152, 192, 384, 512} {
		name := fmt.Sprintf("icon-%dx%d", size, size)
		icon, iconMimeType, iconErr := svc.GetImageWithColor(name, "", false)
		require.NoError(t, iconErr)
		assert.Equal(t, "image/png", iconMimeType, name)
		assert.Equal(t, readEmbedded(t, "images/development/"+name+".png"), icon, name)

		cfg, decodeErr := png.DecodeConfig(bytes.NewReader(icon))
		require.NoError(t, decodeErr)
		assert.Equal(t, size, cfg.Width, name)
		assert.Equal(t, size, cfg.Height, name)
	}
}

func TestGetImageWithColor_DevelopmentBrandingDisabledUsesStandardBranding(t *testing.T) {
	svc, settingsSvc := newServiceForVersion(t, "dev")
	require.NoError(t, settingsSvc.SetBoolSetting(t.Context(), "developmentBrandingEnabled", false))

	_, mimeType, err := svc.GetImageWithColor("logo", "", false)
	require.NoError(t, err)
	assert.Equal(t, "image/svg+xml", mimeType)

	for name, file := range map[string]string{
		"favicon":      "images/favicon.ico",
		"logo-email":   "images/logo-email.png",
		"icon-192x192": "images/icon-192x192.png",
	} {
		data, _, getErr := svc.GetImageWithColor(name, "", false)
		require.NoError(t, getErr)
		assert.Equal(t, readEmbedded(t, file), data, name)
	}
}
