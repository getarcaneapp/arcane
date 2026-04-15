package scheduler

import (
	"context"
	"testing"

	glsqlite "github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/types/system"
)

func setupScheduledPruneSettingsServiceInternal(t *testing.T) *services.SettingsService {
	t.Helper()

	db, err := gorm.Open(glsqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.SettingVariable{}))

	svc, err := services.NewSettingsService(context.Background(), &database.DB{DB: db})
	require.NoError(t, err)

	return svc
}

func TestBuildScheduledPruneRequestInternal(t *testing.T) {
	ctx := context.Background()
	settingsService := setupScheduledPruneSettingsServiceInternal(t)

	require.NoError(t, settingsService.UpdateSetting(ctx, "pruneContainerMode", "olderThan"))
	require.NoError(t, settingsService.UpdateSetting(ctx, "pruneContainerUntil", "24h"))
	require.NoError(t, settingsService.UpdateSetting(ctx, "pruneImageMode", "all"))
	require.NoError(t, settingsService.UpdateSetting(ctx, "pruneVolumeMode", "anonymous"))
	require.NoError(t, settingsService.UpdateSetting(ctx, "pruneNetworkMode", "none"))
	require.NoError(t, settingsService.UpdateSetting(ctx, "pruneBuildCacheMode", "olderThan"))
	require.NoError(t, settingsService.UpdateSetting(ctx, "pruneBuildCacheUntil", "30m"))

	req := buildScheduledPruneRequestInternal(ctx, settingsService)

	require.Equal(t, &system.PruneContainersOptions{
		Mode:  system.PruneContainerModeOlderThan,
		Until: "24h",
	}, req.Containers)
	require.Equal(t, &system.PruneImagesOptions{
		Mode: system.PruneImageModeAll,
	}, req.Images)
	require.Equal(t, &system.PruneVolumesOptions{
		Mode: system.PruneVolumeModeAnonymous,
	}, req.Volumes)
	require.Nil(t, req.Networks)
	require.Equal(t, &system.PruneBuildCacheOptions{
		Mode:  system.PruneBuildCacheModeOlderThan,
		Until: "30m",
	}, req.BuildCache)
	require.True(t, hasScheduledPruneTargetsInternal(req))
}

func TestBuildScheduledPruneRequestInternal_SkipsWhenAllModesAreNone(t *testing.T) {
	ctx := context.Background()
	settingsService := setupScheduledPruneSettingsServiceInternal(t)

	require.NoError(t, settingsService.UpdateSetting(ctx, "pruneContainerMode", "none"))
	require.NoError(t, settingsService.UpdateSetting(ctx, "pruneImageMode", "none"))
	require.NoError(t, settingsService.UpdateSetting(ctx, "pruneVolumeMode", "none"))
	require.NoError(t, settingsService.UpdateSetting(ctx, "pruneNetworkMode", "none"))
	require.NoError(t, settingsService.UpdateSetting(ctx, "pruneBuildCacheMode", "none"))

	req := buildScheduledPruneRequestInternal(ctx, settingsService)

	require.Nil(t, req.Containers)
	require.Nil(t, req.Images)
	require.Nil(t, req.Volumes)
	require.Nil(t, req.Networks)
	require.Nil(t, req.BuildCache)
	require.False(t, hasScheduledPruneTargetsInternal(req))
}
