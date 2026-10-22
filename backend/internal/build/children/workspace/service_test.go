package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v3/internal/config"
	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/internal/settings"
)

func TestServiceDeleteFileRejectsCurrentDirectoryInternal(t *testing.T) {
	ctx := t.Context()
	db := setupSettingsTestDB(t)
	settingsService, err := newSettingsServiceForTestInternal(t, ctx, db)
	require.NoError(t, err)

	root := t.TempDir()
	require.NoError(t, settingsService.UpdateSetting(ctx, "buildsDirectory", root))
	sentinel := filepath.Join(root, "keep.txt")
	require.NoError(t, os.WriteFile(sentinel, []byte("keep"), 0o600))

	err = NewService(func() string {
		return settingsService.GetSettingsConfig().BuildsDirectory.Value
	}).DeleteFile(ctx, ".")
	require.ErrorContains(t, err, "cannot delete root directory")
	require.FileExists(t, sentinel)
}

// Test fixtures shared by this package's tests.

func setupSettingsTestDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&settings.SettingVariable{}))
	return &database.DB{DB: db}
}

func newSettingsServiceForTestInternal(t testing.TB, ctx context.Context, db *database.DB) (*settings.SettingsService, error) {
	t.Helper()
	svc, err := settings.NewSettingsService(ctx, db, &config.Config{}, settings.Env{})
	if err == nil {
		t.Cleanup(func() { require.NoError(t, svc.Stop(context.WithoutCancel(t.Context()))) })
	}
	return svc, err
}
