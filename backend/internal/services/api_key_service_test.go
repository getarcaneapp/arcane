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

func listAPIKeysForUser(t *testing.T, db *database.DB, userID string) []models.ApiKey {
	t.Helper()

	var apiKeys []models.ApiKey
	err := db.WithContext(context.Background()).Where("user_id = ?", userID).Order("created_at asc").Find(&apiKeys).Error
	require.NoError(t, err)
	return apiKeys
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

func createDefaultAdminUser(t *testing.T, ctx context.Context, userService *UserService) *models.User {
	t.Helper()

	user := &models.User{
		BaseModel: models.BaseModel{ID: "default-admin-user"},
		Username:  defaultAdminUsername,
		Roles:     models.StringSlice{"admin"},
	}

	created, err := userService.CreateUser(ctx, user)
	require.NoError(t, err)
	return created
}

func TestCreateDefaultAdminAPIKeyUsesProvidedRawKey(t *testing.T) {
	ctx := context.Background()
	service, db, userService := setupAPIKeyService(t)
	user := createTestAPIKeyUser(t, ctx, userService, "user-default-admin")

	rawKey := "arc_bootstrapprovidedkey1234567890"
	created, err := service.CreateDefaultAdminAPIKey(ctx, user.ID, rawKey)
	require.NoError(t, err)
	require.Equal(t, rawKey, created.Key)
	require.Equal(t, defaultAdminAPIKeyName, created.ApiKey.Name)
	require.True(t, created.ApiKey.IsStatic)

	stored := fetchAPIKey(t, db, created.ApiKey.ID)
	require.NotEqual(t, rawKey, stored.KeyHash)
	require.Equal(t, rawKey[:len(apiKeyPrefix)+apiKeyPrefixLen], stored.KeyPrefix)
	require.NotNil(t, stored.ManagedBy)
	require.Equal(t, managedByAdminBootstrap, *stored.ManagedBy)
}

func TestReconcileDefaultAdminAPIKeyCreatesManagedKey(t *testing.T) {
	ctx := context.Background()
	service, db, userService := setupAPIKeyService(t)
	adminUser := createDefaultAdminUser(t, ctx, userService)

	rawKey := "arc_bootstrapcreate1234567890"
	err := service.ReconcileDefaultAdminAPIKey(ctx, rawKey)
	require.NoError(t, err)

	apiKeys := listAPIKeysForUser(t, db, adminUser.ID)
	require.Len(t, apiKeys, 1)
	require.Equal(t, defaultAdminAPIKeyName, apiKeys[0].Name)
	require.NotNil(t, apiKeys[0].Description)
	require.Equal(t, *defaultAdminAPIKeyDescription, *apiKeys[0].Description)
	require.NotNil(t, apiKeys[0].ManagedBy)
	require.Equal(t, managedByAdminBootstrap, *apiKeys[0].ManagedBy)

	validatedUser, err := service.ValidateApiKey(ctx, rawKey)
	require.NoError(t, err)
	require.Equal(t, adminUser.ID, validatedUser.ID)
}

func TestDeleteApiKeyRejectsStaticKey(t *testing.T) {
	ctx := context.Background()
	service, db, userService := setupAPIKeyService(t)
	adminUser := createDefaultAdminUser(t, ctx, userService)

	created, err := service.CreateDefaultAdminAPIKey(ctx, adminUser.ID, "arc_bootstrapprotected1234567890")
	require.NoError(t, err)

	err = service.DeleteApiKey(ctx, created.ApiKey.ID)
	require.ErrorIs(t, err, ErrApiKeyProtected)

	apiKeys := listAPIKeysForUser(t, db, adminUser.ID)
	require.Len(t, apiKeys, 1)
	require.Equal(t, created.ApiKey.ID, apiKeys[0].ID)
}

func TestUpdateApiKeyRejectsStaticKey(t *testing.T) {
	ctx := context.Background()
	service, db, userService := setupAPIKeyService(t)
	adminUser := createDefaultAdminUser(t, ctx, userService)

	created, err := service.CreateDefaultAdminAPIKey(ctx, adminUser.ID, "arc_bootstrapupdateprotected1234567890")
	require.NoError(t, err)

	name := "renamed"
	description := "updated description"
	updated, err := service.UpdateApiKey(ctx, created.ApiKey.ID, apikey.UpdateApiKey{
		Name:        &name,
		Description: &description,
	})
	require.Nil(t, updated)
	require.ErrorIs(t, err, ErrApiKeyProtected)

	apiKeys := listAPIKeysForUser(t, db, adminUser.ID)
	require.Len(t, apiKeys, 1)
	require.Equal(t, defaultAdminAPIKeyName, apiKeys[0].Name)
	require.NotNil(t, apiKeys[0].Description)
	require.Equal(t, *defaultAdminAPIKeyDescription, *apiKeys[0].Description)
}

func TestReconcileDefaultAdminAPIKeyNoOpWhenUnchanged(t *testing.T) {
	ctx := context.Background()
	service, db, userService := setupAPIKeyService(t)
	adminUser := createDefaultAdminUser(t, ctx, userService)

	rawKey := "arc_bootstrapstable1234567890"
	require.NoError(t, service.ReconcileDefaultAdminAPIKey(ctx, rawKey))
	first := listAPIKeysForUser(t, db, adminUser.ID)
	require.Len(t, first, 1)

	require.NoError(t, service.ReconcileDefaultAdminAPIKey(ctx, rawKey))
	second := listAPIKeysForUser(t, db, adminUser.ID)
	require.Len(t, second, 1)
	require.Equal(t, first[0].ID, second[0].ID)
}

func TestReconcileDefaultAdminAPIKeyReplacesManagedKeyOnRotation(t *testing.T) {
	ctx := context.Background()
	service, db, userService := setupAPIKeyService(t)
	adminUser := createDefaultAdminUser(t, ctx, userService)

	oldKey := "arc_bootstrapoldvalue1234567890"
	newKey := "arc_bootstrapnewvalue1234567890"
	require.NoError(t, service.ReconcileDefaultAdminAPIKey(ctx, oldKey))
	first := listAPIKeysForUser(t, db, adminUser.ID)
	require.Len(t, first, 1)

	require.NoError(t, service.ReconcileDefaultAdminAPIKey(ctx, newKey))
	second := listAPIKeysForUser(t, db, adminUser.ID)
	require.Len(t, second, 1)
	require.NotEqual(t, first[0].ID, second[0].ID)

	_, err := service.ValidateApiKey(ctx, oldKey)
	require.ErrorIs(t, err, ErrApiKeyInvalid)

	validatedUser, err := service.ValidateApiKey(ctx, newKey)
	require.NoError(t, err)
	require.Equal(t, adminUser.ID, validatedUser.ID)
}

func TestReconcileDefaultAdminAPIKeyDeletesManagedKeyWhenUnset(t *testing.T) {
	ctx := context.Background()
	service, db, userService := setupAPIKeyService(t)
	adminUser := createDefaultAdminUser(t, ctx, userService)

	require.NoError(t, service.ReconcileDefaultAdminAPIKey(ctx, "arc_bootstrapdelete1234567890"))
	require.Len(t, listAPIKeysForUser(t, db, adminUser.ID), 1)

	require.NoError(t, service.ReconcileDefaultAdminAPIKey(ctx, ""))
	require.Empty(t, listAPIKeysForUser(t, db, adminUser.ID))
}

func TestReconcileDefaultAdminAPIKeyPreservesUserManagedKeys(t *testing.T) {
	ctx := context.Background()
	service, db, userService := setupAPIKeyService(t)
	adminUser := createDefaultAdminUser(t, ctx, userService)

	userCreated, err := service.CreateApiKey(ctx, adminUser.ID, apikey.CreateApiKey{Name: "manual-key"})
	require.NoError(t, err)

	require.NoError(t, service.ReconcileDefaultAdminAPIKey(ctx, "arc_bootstrapmanualsafe1234567890"))

	apiKeys := listAPIKeysForUser(t, db, adminUser.ID)
	require.Len(t, apiKeys, 2)

	foundUserKey := false
	foundManagedKey := false
	for _, apiKey := range apiKeys {
		if apiKey.ID == userCreated.ApiKey.ID {
			foundUserKey = true
			require.Nil(t, apiKey.ManagedBy)
			require.Equal(t, "manual-key", apiKey.Name)
		}
		if apiKey.ManagedBy != nil && *apiKey.ManagedBy == managedByAdminBootstrap {
			foundManagedKey = true
		}
	}

	require.True(t, foundUserKey)
	require.True(t, foundManagedKey)
}

func TestReconcileDefaultAdminAPIKeyDeletesDuplicateManagedKeys(t *testing.T) {
	ctx := context.Background()
	service, db, userService := setupAPIKeyService(t)
	adminUser := createDefaultAdminUser(t, ctx, userService)

	rawKey := "arc_bootstrapduplicate1234567890"
	first, err := service.CreateDefaultAdminAPIKey(ctx, adminUser.ID, rawKey)
	require.NoError(t, err)
	_, err = service.CreateDefaultAdminAPIKey(ctx, adminUser.ID, rawKey)
	require.NoError(t, err)

	require.NoError(t, service.ReconcileDefaultAdminAPIKey(ctx, rawKey))

	apiKeys := listAPIKeysForUser(t, db, adminUser.ID)
	require.Len(t, apiKeys, 1)
	require.Equal(t, first.ApiKey.ID, apiKeys[0].ID)
}

func TestReconcileDefaultAdminAPIKeySkipsWhenDefaultAdminMissing(t *testing.T) {
	ctx := context.Background()
	service, db, _ := setupAPIKeyService(t)

	err := service.ReconcileDefaultAdminAPIKey(ctx, "arc_bootstrapmissing1234567890")
	require.NoError(t, err)

	var count int64
	err = db.WithContext(ctx).Model(&models.ApiKey{}).Count(&count).Error
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestReconcileDefaultAdminAPIKeyRejectsInvalidKey(t *testing.T) {
	ctx := context.Background()
	service, db, userService := setupAPIKeyService(t)
	adminUser := createDefaultAdminUser(t, ctx, userService)

	err := service.ReconcileDefaultAdminAPIKey(ctx, "invalid-key")
	require.ErrorIs(t, err, ErrApiKeyInvalid)
	require.Empty(t, listAPIKeysForUser(t, db, adminUser.ID))
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

func TestValidateAPIKeyRejectsShortPrefixedInput(t *testing.T) {
	ctx := context.Background()
	service, _, _ := setupAPIKeyService(t)

	_, err := service.ValidateApiKey(ctx, "arc_123")
	require.ErrorIs(t, err, ErrApiKeyInvalid)
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
