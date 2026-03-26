package services

import (
	"context"
	"testing"

	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnvContentChangedInternal(t *testing.T) {
	t.Run("ignores formatting-only changes", func(t *testing.T) {
		oldEnv := "B=2\nA=1\n# comment\n"
		newEnv := "A=1\nB=2\n"

		assert.False(t, envContentChangedInternal(oldEnv, newEnv))
	})

	t.Run("detects semantic changes", func(t *testing.T) {
		oldEnv := "A=1\nB=2\n"
		newEnv := "A=1\nB=3\n"

		assert.True(t, envContentChangedInternal(oldEnv, newEnv))
	})
}

func TestGitOpsSyncService_GetEnvironmentSyncLimits(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	settingsSvc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	require.NoError(t, settingsSvc.SetIntSetting(ctx, "gitSyncMaxFiles", 123))
	require.NoError(t, settingsSvc.SetIntSetting(ctx, "gitSyncMaxTotalSizeMb", 64))
	require.NoError(t, settingsSvc.SetIntSetting(ctx, "gitSyncMaxBinarySizeMb", 12))

	svc := &GitOpsSyncService{settingsService: settingsSvc}

	maxFiles, maxTotalSize, maxBinarySize := svc.getEnvironmentSyncLimits(ctx)

	require.Equal(t, 123, maxFiles)
	require.Equal(t, int64(64*1024*1024), maxTotalSize)
	require.Equal(t, int64(12*1024*1024), maxBinarySize)
}

func TestGitOpsSyncService_GetEffectiveSyncLimits(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	settingsSvc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	require.NoError(t, settingsSvc.SetIntSetting(ctx, "gitSyncMaxFiles", 200))
	require.NoError(t, settingsSvc.SetIntSetting(ctx, "gitSyncMaxTotalSizeMb", 30))
	require.NoError(t, settingsSvc.SetIntSetting(ctx, "gitSyncMaxBinarySizeMb", 5))

	svc := &GitOpsSyncService{settingsService: settingsSvc}

	t.Run("uses environment caps when sync values are looser", func(t *testing.T) {
		sync := &models.GitOpsSync{
			MaxSyncFiles:      500,
			MaxSyncTotalSize:  50 * 1024 * 1024,
			MaxSyncBinarySize: 10 * 1024 * 1024,
		}

		maxFiles, maxTotalSize, maxBinarySize := svc.getEffectiveSyncLimits(ctx, sync)

		require.Equal(t, 200, maxFiles)
		require.Equal(t, int64(30*1024*1024), maxTotalSize)
		require.Equal(t, int64(5*1024*1024), maxBinarySize)
	})

	t.Run("preserves tighter sync-specific limits", func(t *testing.T) {
		sync := &models.GitOpsSync{
			MaxSyncFiles:      75,
			MaxSyncTotalSize:  8 * 1024 * 1024,
			MaxSyncBinarySize: 2 * 1024 * 1024,
		}

		maxFiles, maxTotalSize, maxBinarySize := svc.getEffectiveSyncLimits(ctx, sync)

		require.Equal(t, 75, maxFiles)
		require.Equal(t, int64(8*1024*1024), maxTotalSize)
		require.Equal(t, int64(2*1024*1024), maxBinarySize)
	})

	t.Run("treats zero as unlimited", func(t *testing.T) {
		sync := &models.GitOpsSync{
			MaxSyncFiles:      0,
			MaxSyncTotalSize:  0,
			MaxSyncBinarySize: 0,
		}

		maxFiles, maxTotalSize, maxBinarySize := svc.getEffectiveSyncLimits(ctx, sync)

		require.Equal(t, 200, maxFiles)
		require.Equal(t, int64(30*1024*1024), maxTotalSize)
		require.Equal(t, int64(5*1024*1024), maxBinarySize)
	})
}
