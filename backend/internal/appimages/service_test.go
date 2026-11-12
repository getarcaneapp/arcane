package appimages

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"slices"
	"testing"

	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/resources"
	"getarcane.dev/types/v3"
)

const testSVG = `<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1 1"><rect width="1" height="1"/></svg>`

func newServiceForVersion(t *testing.T, version string) (*ApplicationImagesService, *settings.SettingsService) {
	t.Helper()
	original := config.Version
	config.Version = version
	t.Cleanup(func() { config.Version = original })

	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gdb.AutoMigrate(&settings.SettingVariable{}, &CustomImage{}))
	db := &database.DB{DB: gdb}
	settingsSvc, err := settings.NewSettingsService(settings.SettingsServiceParams{
		Ctx:    t.Context(),
		DB:     db,
		Config: &config.Config{},
		Env:    settings.Env{},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, settingsSvc.Stop(context.WithoutCancel(t.Context()))) })

	return NewApplicationImagesService(resources.FS, settingsSvc, db), settingsSvc
}

func readEmbedded(t *testing.T, name string) []byte {
	t.Helper()
	data, err := resources.FS.ReadFile(name)
	require.NoError(t, err)
	return data
}

func encodePNG(t *testing.T, width, height int) []byte {
	t.Helper()
	var out bytes.Buffer
	require.NoError(t, png.Encode(&out, image.NewNRGBA(image.Rect(0, 0, width, height))))
	return out.Bytes()
}

func publicSettingValue(settingsSvc *settings.SettingsService, key string) (string, bool) {
	snapshot := settingsSvc.SettingsSnapshot(settings.SettingVisibilityPublic)
	idx := slices.IndexFunc(snapshot.Settings, func(state types.SettingState) bool {
		return state.Key == key
	})
	if idx < 0 {
		return "", false
	}
	return snapshot.Settings[idx].Value, true
}

func TestNewApplicationImagesService_StableBuildUsesStandardBranding(t *testing.T) {
	svc, _ := newServiceForVersion(t, "v1.2.3")

	for _, name := range []string{"logo", "logo-animated"} {
		_, mimeType, err := svc.GetImageWithColor(t.Context(), name, "", false)
		require.NoError(t, err)
		assert.Equal(t, "image/svg+xml", mimeType, name)
	}

	data, _, err := svc.GetImageWithColor(t.Context(), "favicon", "", false)
	require.NoError(t, err)
	assert.Equal(t, readEmbedded(t, "images/favicon.ico"), data)
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
		data, mimeType, err := svc.GetImageWithColor(t.Context(), tc.name, tc.color, tc.loop)
		require.NoError(t, err)
		assert.Equal(t, "image/png", mimeType, tc.name)
		assert.Equal(t, mark, data, tc.name)
	}

	for _, name := range []string{"logo-full", "logo-full-animated"} {
		data, mimeType, err := svc.GetImageWithColor(t.Context(), name, "#ff0000", false)
		require.NoError(t, err)
		assert.Equal(t, "image/svg+xml", mimeType, name)
		assert.Contains(t, string(data), "#ff0000", name)
	}

	data, mimeType, err := svc.GetImageWithColor(t.Context(), "favicon", "", false)
	require.NoError(t, err)
	assert.Equal(t, readEmbedded(t, "images/development/favicon.ico"), data)
	assert.Equal(t, []byte{0, 0, 1, 0}, data[:4])
	assert.NotEmpty(t, mimeType)

	data, _, err = svc.GetImageWithColor(t.Context(), "profile", "", false)
	require.NoError(t, err)
	assert.Equal(t, readEmbedded(t, "images/profile.webp"), data)

	for _, size := range []int{72, 96, 128, 144, 152, 192, 384, 512} {
		name := fmt.Sprintf("icon-%dx%d", size, size)
		icon, iconMimeType, iconErr := svc.GetImageWithColor(t.Context(), name, "", false)
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

	_, mimeType, err := svc.GetImageWithColor(t.Context(), "logo", "", false)
	require.NoError(t, err)
	assert.Equal(t, "image/svg+xml", mimeType)

	for name, file := range map[string]string{
		"favicon":      "images/favicon.ico",
		"icon-192x192": "images/icon-192x192.png",
	} {
		data, _, getErr := svc.GetImageWithColor(t.Context(), name, "", false)
		require.NoError(t, getErr)
		assert.Equal(t, readEmbedded(t, file), data, name)
	}
}

func TestUploadImage_LogoTakesPrecedenceUntilDeleted(t *testing.T) {
	svc, settingsSvc := newServiceForVersion(t, "1.2.3-next.1")
	ctx := t.Context()
	markPNG := encodePNG(t, 4, 4)

	require.NoError(t, svc.UploadImage(ctx, ImageSlotMark, bytes.NewReader(markPNG)))
	firstHash, ok := publicSettingValue(settingsSvc, "customLogoHash")
	require.True(t, ok)
	assert.NotEmpty(t, firstHash)

	for _, tc := range []struct {
		name  string
		color string
		loop  bool
	}{
		{"logo", "", false},
		{"logo", "#ff0000", false},
		{"logo-animated", "#ff0000", true},
	} {
		data, mimeType, err := svc.GetImageWithColor(ctx, tc.name, tc.color, tc.loop)
		require.NoError(t, err)
		assert.Equal(t, "image/png", mimeType, tc.name)
		assert.Equal(t, markPNG, data, tc.name)
	}

	_, fullMimeType, err := svc.GetImageWithColor(ctx, "logo-full", "", false)
	require.NoError(t, err)
	assert.Equal(t, "image/svg+xml", fullMimeType)
	fullHash, _ := publicSettingValue(settingsSvc, "customLogoFullHash")
	assert.Empty(t, fullHash)

	require.NoError(t, svc.UploadImage(ctx, ImageSlotMark, bytes.NewReader([]byte(testSVG))))
	data, mimeType, err := svc.GetImageWithColor(ctx, "logo-animated", "#ff0000", false)
	require.NoError(t, err)
	assert.Equal(t, "image/svg+xml", mimeType)
	assert.Equal(t, testSVG, string(data))
	replacedHash, _ := publicSettingValue(settingsSvc, "customLogoHash")
	assert.NotEqual(t, firstHash, replacedHash)

	require.NoError(t, svc.UploadImage(ctx, ImageSlotFull, bytes.NewReader([]byte(testSVG))))
	data, _, err = svc.GetImageWithColor(ctx, "logo-full-animated", "", true)
	require.NoError(t, err)
	assert.Equal(t, testSVG, string(data))

	require.NoError(t, svc.DeleteImage(ctx, ImageSlotMark))
	data, mimeType, err = svc.GetImageWithColor(ctx, "logo", "", false)
	require.NoError(t, err)
	assert.Equal(t, "image/png", mimeType)
	assert.Equal(t, readEmbedded(t, "images/development/icon-512x512.png"), data)
	clearedHash, _ := publicSettingValue(settingsSvc, "customLogoHash")
	assert.Empty(t, clearedHash)

	data, _, err = svc.GetImageWithColor(ctx, "logo-full", "", false)
	require.NoError(t, err)
	assert.Equal(t, testSVG, string(data))
}

func TestUploadImage_FaviconAcceptsIcoPngAndSvg(t *testing.T) {
	svc, settingsSvc := newServiceForVersion(t, "1.2.3-next.1")
	ctx := t.Context()

	for _, tc := range []struct {
		mimeType string
		data     []byte
	}{
		{mimeType: "image/x-icon", data: readEmbedded(t, "images/favicon.ico")},
		{mimeType: "image/png", data: encodePNG(t, 32, 32)},
		{mimeType: "image/svg+xml", data: []byte(testSVG)},
	} {
		require.NoError(t, svc.UploadImage(ctx, ImageSlotFavicon, bytes.NewReader(tc.data)), tc.mimeType)
		data, mimeType, err := svc.GetImageWithColor(ctx, "favicon", "", false)
		require.NoError(t, err)
		assert.Equal(t, tc.mimeType, mimeType)
		assert.Equal(t, tc.data, data, tc.mimeType)
		hash, _ := publicSettingValue(settingsSvc, "customFaviconHash")
		assert.NotEmpty(t, hash, tc.mimeType)
	}

	require.NoError(t, svc.DeleteImage(ctx, ImageSlotFavicon))
	data, _, err := svc.GetImageWithColor(ctx, "favicon", "", false)
	require.NoError(t, err)
	assert.Equal(t, readEmbedded(t, "images/development/favicon.ico"), data)
}

func TestUploadImage_PWAIconIsResizedToEverySize(t *testing.T) {
	svc, settingsSvc := newServiceForVersion(t, "v1.2.3")
	ctx := t.Context()

	require.NoError(t, svc.UploadImage(ctx, ImageSlotPWA, bytes.NewReader(encodePNG(t, 600, 600))))
	hash, _ := publicSettingValue(settingsSvc, "customPwaIconHash")
	assert.NotEmpty(t, hash)

	for _, size := range []int{72, 96, 128, 144, 152, 192, 384, 512} {
		name := fmt.Sprintf("icon-%dx%d", size, size)
		icon, mimeType, err := svc.GetImageWithColor(ctx, name, "", false)
		require.NoError(t, err)
		assert.Equal(t, "image/png", mimeType, name)
		assert.NotEqual(t, readEmbedded(t, "images/"+name+".png"), icon, name)

		cfg, decodeErr := png.DecodeConfig(bytes.NewReader(icon))
		require.NoError(t, decodeErr)
		assert.Equal(t, size, cfg.Width, name)
		assert.Equal(t, size, cfg.Height, name)
	}

	require.NoError(t, svc.DeleteImage(ctx, ImageSlotPWA))
	icon, _, err := svc.GetImageWithColor(ctx, "icon-192x192", "", false)
	require.NoError(t, err)
	assert.Equal(t, readEmbedded(t, "images/icon-192x192.png"), icon)
	clearedHash, _ := publicSettingValue(settingsSvc, "customPwaIconHash")
	assert.Empty(t, clearedHash)
}

func TestUploadImage_RejectsInvalidUploads(t *testing.T) {
	svc, settingsSvc := newServiceForVersion(t, "v1.2.3")
	ctx := t.Context()

	for _, tc := range []struct {
		name string
		slot ImageSlot
		data []byte
	}{
		{name: "unknown slot", slot: "banner", data: encodePNG(t, 4, 4)},
		{name: "too large", slot: ImageSlotMark, data: append(encodePNG(t, 4, 4), make([]byte, ImageMaxBytes)...)},
		{name: "gif logo", slot: ImageSlotMark, data: append([]byte("GIF89a"), make([]byte, 16)...)},
		{name: "plain text logo", slot: ImageSlotFull, data: []byte("hello")},
		{name: "html logo", slot: ImageSlotFull, data: []byte("<html><body>hi</body></html>")},
		{name: "empty favicon", slot: ImageSlotFavicon, data: nil},
		{name: "jpeg favicon", slot: ImageSlotFavicon, data: append([]byte("\xff\xd8\xff"), make([]byte, 16)...)},
		{name: "svg pwa icon", slot: ImageSlotPWA, data: []byte(testSVG)},
		{name: "non-square pwa icon", slot: ImageSlotPWA, data: encodePNG(t, 600, 512)},
		{name: "small pwa icon", slot: ImageSlotPWA, data: encodePNG(t, 256, 256)},
		{name: "oversized pwa icon", slot: ImageSlotPWA, data: encodePNG(t, 4097, 4097)},
	} {
		err := svc.UploadImage(ctx, tc.slot, bytes.NewReader(tc.data))
		require.ErrorIs(t, err, common.ErrValidation, tc.name)
	}

	require.ErrorIs(t, svc.DeleteImage(ctx, "banner"), common.ErrValidation)

	for _, key := range []string{"customLogoHash", "customLogoFullHash", "customFaviconHash", "customPwaIconHash"} {
		value, ok := publicSettingValue(settingsSvc, key)
		assert.True(t, ok, key)
		assert.Empty(t, value, key)
	}
}

func TestCustomImageHashes_AreNotWritableThroughSettingsUpdate(t *testing.T) {
	_, settingsSvc := newServiceForVersion(t, "v1.2.3")

	err := settingsSvc.UpdateSettings(t.Context(), types.SettingsUpdate{"customLogoHash": "forged"})
	require.ErrorIs(t, err, common.ErrValidation)
	value, _ := publicSettingValue(settingsSvc, "customLogoHash")
	assert.Empty(t, value)
}
