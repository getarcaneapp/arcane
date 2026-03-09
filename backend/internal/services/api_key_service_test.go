package services

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	glsqlite "github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/types/apikey"
)

func setupAPIKeyServiceTestDB(t *testing.T) *database.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.NewReplacer("/", "_", " ", "_").Replace(t.Name()))
	db, err := gorm.Open(glsqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.User{}, &models.ApiKey{}))

	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	return &database.DB{DB: db}
}

func setupAPIKeyService(t *testing.T) (*ApiKeyService, *database.DB, *UserService) {
	t.Helper()

	db := setupAPIKeyServiceTestDB(t)
	userService := NewUserService(db)
	return NewApiKeyService(db, userService), db, userService
}

func createTestAPIKeyUser(t *testing.T, ctx context.Context, userService *UserService, id string) *models.User {
	t.Helper()

	user := &models.User{
		BaseModel: models.BaseModel{ID: id},
		Username:  fmt.Sprintf("user-%s", id),
		Roles:     models.StringSlice{"admin"},
	}

	created, err := userService.CreateUser(ctx, user)
	require.NoError(t, err)
	return created
}

func fetchAPIKey(t *testing.T, db *database.DB, keyID string) models.ApiKey {
	t.Helper()

	var apiKey models.ApiKey
	err := db.WithContext(context.Background()).Where("id = ?", keyID).First(&apiKey).Error
	require.NoError(t, err)
	return apiKey
}

func requireAPIKeyLastUsedEventually(t *testing.T, db *database.DB, keyID string) models.ApiKey {
	t.Helper()

	var apiKey models.ApiKey
	require.Eventually(t, func() bool {
		err := db.WithContext(context.Background()).Where("id = ?", keyID).First(&apiKey).Error
		return err == nil && apiKey.LastUsedAt != nil
	}, time.Second, 10*time.Millisecond)

	return apiKey
}

func assertAPIKeyLastUsedStable(t *testing.T, db *database.DB, keyID string, expected *time.Time, duration time.Duration) {
	t.Helper()

	assert.Never(t, func() bool {
		apiKey := fetchAPIKey(t, db, keyID)
		if expected == nil {
			return apiKey.LastUsedAt != nil
		}
		if apiKey.LastUsedAt == nil {
			return true
		}
		return apiKey.LastUsedAt.UTC().UnixNano() != expected.UTC().UnixNano()
	}, duration, 10*time.Millisecond)
}

func invalidateAPIKey(rawKey string) string {
	if rawKey == "" {
		return rawKey
	}

	if strings.HasSuffix(rawKey, "0") {
		return rawKey[:len(rawKey)-1] + "1"
	}

	return rawKey[:len(rawKey)-1] + "0"
}

func TestValidateAPIKeyUpdatesLastUsedAt(t *testing.T) {
	ctx := context.Background()
	service, db, userService := setupAPIKeyService(t)
	user := createTestAPIKeyUser(t, ctx, userService, "user-validate")

	created, err := service.CreateApiKey(ctx, user.ID, apikey.CreateApiKey{Name: "validate-key"})
	require.NoError(t, err)
	require.Nil(t, fetchAPIKey(t, db, created.ApiKey.ID).LastUsedAt)

	validatedUser, err := service.ValidateApiKey(ctx, created.Key)
	require.NoError(t, err)
	require.Equal(t, user.ID, validatedUser.ID)

	apiKey := requireAPIKeyLastUsedEventually(t, db, created.ApiKey.ID)
	require.NotNil(t, apiKey.LastUsedAt)
}

func TestGetEnvironmentByAPIKeyUpdatesLastUsedAt(t *testing.T) {
	ctx := context.Background()
	service, db, userService := setupAPIKeyService(t)
	user := createTestAPIKeyUser(t, ctx, userService, "user-environment")

	created, err := service.CreateEnvironmentApiKey(ctx, "env-123", user.ID)
	require.NoError(t, err)
	require.Nil(t, fetchAPIKey(t, db, created.ApiKey.ID).LastUsedAt)

	environmentID, err := service.GetEnvironmentByApiKey(ctx, created.Key)
	require.NoError(t, err)
	require.NotNil(t, environmentID)
	require.Equal(t, "env-123", *environmentID)

	apiKey := requireAPIKeyLastUsedEventually(t, db, created.ApiKey.ID)
	require.NotNil(t, apiKey.LastUsedAt)
}

func TestValidateAPIKeyInvalidDoesNotUpdateLastUsedAt(t *testing.T) {
	ctx := context.Background()
	service, db, userService := setupAPIKeyService(t)
	user := createTestAPIKeyUser(t, ctx, userService, "user-invalid")

	created, err := service.CreateApiKey(ctx, user.ID, apikey.CreateApiKey{Name: "invalid-key"})
	require.NoError(t, err)

	_, err = service.ValidateApiKey(ctx, invalidateAPIKey(created.Key))
	require.ErrorIs(t, err, ErrApiKeyInvalid)

	assertAPIKeyLastUsedStable(t, db, created.ApiKey.ID, nil, 500*time.Millisecond)
	apiKey := fetchAPIKey(t, db, created.ApiKey.ID)
	require.Nil(t, apiKey.LastUsedAt)
}

func TestGetEnvironmentByAPIKeyExpiredDoesNotUpdateLastUsedAt(t *testing.T) {
	ctx := context.Background()
	service, db, userService := setupAPIKeyService(t)
	user := createTestAPIKeyUser(t, ctx, userService, "user-expired")

	created, err := service.CreateEnvironmentApiKey(ctx, "env-expired", user.ID)
	require.NoError(t, err)

	expiredAt := time.Now().Add(-time.Minute)
	err = db.WithContext(ctx).Model(&models.ApiKey{}).Where("id = ?", created.ApiKey.ID).Update("expires_at", expiredAt).Error
	require.NoError(t, err)

	_, err = service.GetEnvironmentByApiKey(ctx, created.Key)
	require.ErrorIs(t, err, ErrApiKeyExpired)

	assertAPIKeyLastUsedStable(t, db, created.ApiKey.ID, nil, 500*time.Millisecond)
	apiKey := fetchAPIKey(t, db, created.ApiKey.ID)
	require.Nil(t, apiKey.LastUsedAt)
}

func TestGetEnvironmentByAPIKeyRecentLastUsedAtDoesNotRewriteImmediately(t *testing.T) {
	ctx := context.Background()
	service, db, userService := setupAPIKeyService(t)
	user := createTestAPIKeyUser(t, ctx, userService, "user-environment-recent")

	created, err := service.CreateEnvironmentApiKey(ctx, "env-456", user.ID)
	require.NoError(t, err)

	recent := time.Now().Add(-time.Minute)
	err = db.WithContext(ctx).Model(&models.ApiKey{}).Where("id = ?", created.ApiKey.ID).Update("last_used_at", recent).Error
	require.NoError(t, err)

	before := fetchAPIKey(t, db, created.ApiKey.ID)
	require.NotNil(t, before.LastUsedAt)

	environmentID, err := service.GetEnvironmentByApiKey(ctx, created.Key)
	require.NoError(t, err)
	require.NotNil(t, environmentID)
	require.Equal(t, "env-456", *environmentID)

	assertAPIKeyLastUsedStable(t, db, created.ApiKey.ID, before.LastUsedAt, 2*time.Second)
	after := fetchAPIKey(t, db, created.ApiKey.ID)
	require.NotNil(t, after.LastUsedAt)
	require.Equal(t, before.LastUsedAt.UTC().Unix(), after.LastUsedAt.UTC().Unix())
}
