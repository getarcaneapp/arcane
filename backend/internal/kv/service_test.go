package kv

import (
	"context"
	"testing"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupKVServiceInternal(t *testing.T) *KVService {
	t.Helper()

	db := setupSettingsTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.KVEntry{}))

	return NewKVService(db)
}

func TestKVService_Get_MissingKey(t *testing.T) {
	ctx := context.Background()
	svc := setupKVServiceInternal(t)

	value, ok, err := svc.Get(ctx, "missing")
	require.NoError(t, err)
	require.False(t, ok)
	require.Empty(t, value)
}

func TestKVService_Set_UpsertsValue(t *testing.T) {
	ctx := context.Background()
	svc := setupKVServiceInternal(t)

	require.NoError(t, svc.Set(ctx, "analytics.heartbeat.last_attempt_at", "2026-03-10T00:00:00Z"))

	value, ok, err := svc.Get(ctx, "analytics.heartbeat.last_attempt_at")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "2026-03-10T00:00:00Z", value)

	require.NoError(t, svc.Set(ctx, "analytics.heartbeat.last_attempt_at", "2026-03-11T00:00:00Z"))

	updatedValue, updatedOK, err := svc.Get(ctx, "analytics.heartbeat.last_attempt_at")
	require.NoError(t, err)
	require.True(t, updatedOK)
	require.Equal(t, "2026-03-11T00:00:00Z", updatedValue)
}

func setupSettingsTestDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.SettingVariable{}))
	return &database.DB{DB: db}
}
