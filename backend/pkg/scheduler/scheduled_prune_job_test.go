package scheduler

import (
	"testing"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/internal/settings"
)

func setupScheduledPruneSettingsService(t *testing.T) *settings.SettingsService {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&settings.SettingVariable{}))

	svc, err := newSettingsServiceForTest(t, t.Context(), &database.DB{DB: db})
	require.NoError(t, err)

	return svc
}

func TestBuildScheduledPruneRequest(t *testing.T) {
	ctx := t.Context()
	settingsService := setupScheduledPruneSettingsService(t)

	require.NoError(t, settingsService.UpdateSetting(ctx, "pruneContainerMode", "olderThan"))
	require.NoError(t, settingsService.UpdateSetting(ctx, "pruneContainerUntil", "24h"))
	require.NoError(t, settingsService.UpdateSetting(ctx, "pruneImageMode", "all"))
	require.NoError(t, settingsService.UpdateSetting(ctx, "pruneVolumeMode", "anonymous"))
	require.NoError(t, settingsService.UpdateSetting(ctx, "pruneNetworkMode", "none"))
	require.NoError(t, settingsService.UpdateSetting(ctx, "pruneBuildCacheMode", "olderThan"))
	require.NoError(t, settingsService.UpdateSetting(ctx, "pruneBuildCacheUntil", "30m"))

	req := buildScheduledPruneRequest(ctx, settingsService)

	require.Equal(t, &types.PruneContainersOptions{
		Mode:  types.PruneContainerModeOlderThan,
		Until: "24h",
	}, req.Containers)
	require.Equal(t, &types.PruneImagesOptions{
		Mode: types.PruneImageModeAll,
	}, req.Images)
	require.Equal(t, &types.PruneVolumesOptions{
		Mode: types.PruneVolumeModeAnonymous,
	}, req.Volumes)
	require.Nil(t, req.Networks)
	require.Equal(t, &types.PruneBuildCacheOptions{
		Mode:  types.PruneBuildCacheModeOlderThan,
		Until: "30m",
	}, req.BuildCache)
	require.True(t, hasScheduledPruneTargets(req))
}

func TestBuildScheduledPruneRequestInternal_SkipsWhenAllModesAreNone(t *testing.T) {
	ctx := t.Context()
	settingsService := setupScheduledPruneSettingsService(t)

	require.NoError(t, settingsService.UpdateSetting(ctx, "pruneContainerMode", "none"))
	require.NoError(t, settingsService.UpdateSetting(ctx, "pruneImageMode", "none"))
	require.NoError(t, settingsService.UpdateSetting(ctx, "pruneVolumeMode", "none"))
	require.NoError(t, settingsService.UpdateSetting(ctx, "pruneNetworkMode", "none"))
	require.NoError(t, settingsService.UpdateSetting(ctx, "pruneBuildCacheMode", "none"))

	req := buildScheduledPruneRequest(ctx, settingsService)

	require.Nil(t, req.Containers)
	require.Nil(t, req.Images)
	require.Nil(t, req.Volumes)
	require.Nil(t, req.Networks)
	require.Nil(t, req.BuildCache)
	require.False(t, hasScheduledPruneTargets(req))
}
