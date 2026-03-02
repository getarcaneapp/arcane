package services

import (
	"context"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	dashboardtypes "github.com/getarcaneapp/arcane/types/dashboard"
	glsqlite "github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupDashboardServiceTestDB(t *testing.T) *database.DB {
	t.Helper()

	db, err := gorm.Open(glsqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.ApiKey{}))

	return &database.DB{DB: db}
}

func createDashboardTestAPIKey(t *testing.T, db *database.DB, key models.ApiKey) {
	t.Helper()
	require.NoError(t, db.WithContext(context.Background()).Create(&key).Error)
}

func TestDashboardService_GetActionItems_IncludesExpiringAPIKeys(t *testing.T) {
	db := setupDashboardServiceTestDB(t)
	svc := NewDashboardService(db, nil, nil)

	now := time.Now()
	expiringSoon := now.Add(24 * time.Hour)
	alreadyExpired := now.Add(-24 * time.Hour)
	farFuture := now.Add(45 * 24 * time.Hour)

	createDashboardTestAPIKey(t, db, models.ApiKey{
		Name:      "expiring-soon",
		KeyHash:   "hash-soon",
		KeyPrefix: "arc_test_s",
		UserID:    "user-1",
		ExpiresAt: &expiringSoon,
	})
	createDashboardTestAPIKey(t, db, models.ApiKey{
		Name:      "already-expired",
		KeyHash:   "hash-expired",
		KeyPrefix: "arc_test_e",
		UserID:    "user-1",
		ExpiresAt: &alreadyExpired,
	})
	createDashboardTestAPIKey(t, db, models.ApiKey{
		Name:      "future",
		KeyHash:   "hash-future",
		KeyPrefix: "arc_test_f",
		UserID:    "user-1",
		ExpiresAt: &farFuture,
	})
	createDashboardTestAPIKey(t, db, models.ApiKey{
		Name:      "never-expires",
		KeyHash:   "hash-never",
		KeyPrefix: "arc_test_n",
		UserID:    "user-1",
	})

	actionItems, err := svc.GetActionItems(context.Background(), DashboardActionItemsOptions{})
	require.NoError(t, err)
	require.NotNil(t, actionItems)
	require.Len(t, actionItems.Items, 1)

	item := actionItems.Items[0]
	require.Equal(t, dashboardtypes.ActionItemKindExpiringKeys, item.Kind)
	require.Equal(t, 2, item.Count)
	require.Equal(t, dashboardtypes.ActionItemSeverityWarning, item.Severity)
}

func TestDashboardService_GetActionItems_DebugAllGoodReturnsNoItems(t *testing.T) {
	db := setupDashboardServiceTestDB(t)
	svc := NewDashboardService(db, nil, nil)

	expiresAt := time.Now().Add(2 * time.Hour)
	createDashboardTestAPIKey(t, db, models.ApiKey{
		Name:      "expiring-soon",
		KeyHash:   "hash-soon",
		KeyPrefix: "arc_test_d",
		UserID:    "user-1",
		ExpiresAt: &expiresAt,
	})

	actionItems, err := svc.GetActionItems(context.Background(), DashboardActionItemsOptions{
		DebugAllGood: true,
	})
	require.NoError(t, err)
	require.NotNil(t, actionItems)
	require.Empty(t, actionItems.Items)
}
