package apikey

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/session"

	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"

	"context"
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/role"
	"github.com/getarcaneapp/arcane/backend/v2/internal/user"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/types/v2/apikey"
)

func setupAPIKeyServiceTestDB(t *testing.T) *database.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.NewReplacer("/", "_", " ", "_").Replace(t.Name()))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&settings.SettingVariable{},
		&common.User{},
		&session.UserSession{},
		&testEnvironmentRow{},
		&role.Role{},
		&role.UserRoleAssignment{},
		&ApiKey{},
		&role.ApiKeyPermission{},
		&role.OidcRoleMapping{},
	))

	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	return &database.DB{DB: db}
}

func setupAPIKeyService(t *testing.T) (*ApiKeyService, *database.DB, *user.UserService) {
	t.Helper()

	db := setupAPIKeyServiceTestDB(t)
	userService := user.NewUserService(db)
	return NewApiKeyService(db, userService), db, userService
}

func createTestAPIKeyUser(t *testing.T, ctx context.Context, userService *user.UserService, id string, usernames ...string) *common.User {
	t.Helper()
	username := fmt.Sprintf("user-%s", id)
	if len(usernames) > 0 {
		username = usernames[0]
	}

	user := &common.User{
		BaseModel: database.BaseModel{ID: id},
		Username:  username,
	}

	created, err := userService.CreateUser(ctx, user)
	require.NoError(t, err)
	return created
}

func fetchAPIKey(t *testing.T, db *database.DB, keyID string) ApiKey {
	t.Helper()

	var apiKey ApiKey
	err := db.WithContext(context.Background()).Where("id = ?", keyID).First(&apiKey).Error
	require.NoError(t, err)
	return apiKey
}

func listAPIKeysForUser(t *testing.T, db *database.DB, userID string) []ApiKey {
	t.Helper()

	var apiKeys []ApiKey
	err := db.WithContext(context.Background()).Where("user_id = ?", userID).Order("created_at asc").Find(&apiKeys).Error
	require.NoError(t, err)
	return apiKeys
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

func createDefaultAdminUser(t *testing.T, ctx context.Context, userService *user.UserService) *common.User {
	t.Helper()

	user := &common.User{
		BaseModel: database.BaseModel{ID: "default-admin-user"},
		Username:  defaultAdminUsername,
	}

	created, err := userService.CreateUser(ctx, user)
	require.NoError(t, err)
	return created
}

func TestListApiKeysPermissionQueryCountIsConstant(t *testing.T) {
	queryCounts := make(map[int]int64, 2)
	userQueryCounts := make(map[int]int64, 2)

	for _, keyCount := range []int{1, 5} {
		t.Run(fmt.Sprintf("%d_keys", keyCount), func(t *testing.T) {
			db := setupAPIKeyServiceTestDB(t)
			service := NewApiKeyService(db, user.NewUserService(db)).WithRoleService(role.NewRoleService(db))
			userID := "query-count-user"

			apiKeys := make([]ApiKey, keyCount)
			permissions := make([]role.ApiKeyPermission, keyCount)
			for i := range keyCount {
				keyID := fmt.Sprintf("key-%d", i)
				apiKeys[i] = ApiKey{
					BaseModel: database.BaseModel{ID: keyID},
					Name:      keyID,
					KeyHash:   "hash",
					KeyPrefix: fmt.Sprintf("arc_%04d", i),
					Kind:      ApiKeyKindScoped,
					UserID:    &userID,
				}
				permissions[i] = role.ApiKeyPermission{
					BaseModel:  database.BaseModel{ID: fmt.Sprintf("permission-%d", i)},
					ApiKeyID:   keyID,
					Permission: authz.PermContainersList,
				}
			}
			require.NoError(t, db.Create(&apiKeys).Error)
			require.NoError(t, db.Create(&permissions).Error)

			var queryCount atomic.Int64
			require.NoError(t, db.Callback().Query().Before("gorm:query").Register("count_api_key_list_queries", func(*gorm.DB) {
				queryCount.Add(1)
			}))

			result, _, err := service.ListApiKeys(context.Background(), pagination.QueryParams{
				Params: pagination.Params{Limit: 100},
			})
			require.NoError(t, err)
			require.Len(t, result, keyCount)
			for _, apiKey := range result {
				require.Len(t, apiKey.Permissions, 1)
			}
			queryCounts[keyCount] = queryCount.Load()

			queryCount.Store(0)
			result, err = service.ListApiKeysByUser(context.Background(), userID)
			require.NoError(t, err)
			require.Len(t, result, keyCount)
			for _, apiKey := range result {
				require.Len(t, apiKey.Permissions, 1)
			}
			userQueryCounts[keyCount] = queryCount.Load()
		})
	}

	// Each list call spends one extra constant query on the set of
	// environment-referenced key ids (bootstrap-protection flag).
	require.Equal(t, int64(4), queryCounts[1])
	require.Equal(t, int64(4), queryCounts[5])
	require.Equal(t, int64(3), userQueryCounts[1])
	require.Equal(t, int64(3), userQueryCounts[5])
}

func TestCreateDefaultAdminAPIKeyUsesProvidedRawKey(t *testing.T) {
	ctx := context.Background()
	service, db, userService := setupAPIKeyService(t)
	user := createTestAPIKeyUser(t, ctx, userService, "user-default-admin")

	rawKey := "arc_bootstrapprovidedkey1234567890"
	created, err := service.CreateDefaultAdminAPIKey(ctx, user.ID, rawKey)
	require.NoError(t, err)
	require.Equal(t, rawKey, created.Key)
	require.Equal(t, defaultAdminAPIKeyName, created.Name)
	require.True(t, created.IsStatic)

	stored := fetchAPIKey(t, db, created.ID)
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

	err = service.DeleteApiKey(ctx, created.ID)
	require.ErrorIs(t, err, ErrApiKeyProtected)

	apiKeys := listAPIKeysForUser(t, db, adminUser.ID)
	require.Len(t, apiKeys, 1)
	require.Equal(t, created.ID, apiKeys[0].ID)
}

func TestUpdateApiKeyRejectsStaticKey(t *testing.T) {
	ctx := context.Background()
	service, db, userService := setupAPIKeyService(t)
	adminUser := createDefaultAdminUser(t, ctx, userService)

	created, err := service.CreateDefaultAdminAPIKey(ctx, adminUser.ID, "arc_bootstrapupdateprotected1234567890")
	require.NoError(t, err)

	updated, err := service.UpdateApiKey(ctx, authz.SudoPermissionSet(), created.ID, apikey.UpdateApiKey{
		Name:        new("renamed"),
		Description: new("updated description"),
	})
	require.Nil(t, updated)
	require.ErrorIs(t, err, ErrApiKeyProtected)

	apiKeys := listAPIKeysForUser(t, db, adminUser.ID)
	require.Len(t, apiKeys, 1)
	require.Equal(t, defaultAdminAPIKeyName, apiKeys[0].Name)
	require.NotNil(t, apiKeys[0].Description)
	require.Equal(t, *defaultAdminAPIKeyDescription, *apiKeys[0].Description)
}

func TestUpdateApiKeyRollsBackMetadataWhenPermissionUpdateFails(t *testing.T) {
	ctx := context.Background()
	db := setupAPIKeyServiceTestDB(t)
	require.NoError(t, db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_akp_uniq ON api_key_permissions(api_key_id, permission, COALESCE(environment_id, ''))").Error)

	roleSvc := role.NewRoleService(db)
	require.NoError(t, roleSvc.EnsureBuiltInRoles(ctx))
	userSvc := user.NewUserService(db).WithRoleService(roleSvc)
	service := NewApiKeyService(db, userSvc).WithRoleService(roleSvc)
	admin := createTestAPIKeyUser(t, ctx, userSvc, "admin-update-rollback", "admin-update-rollback")
	require.NoError(t, roleSvc.SetUserAssignments(ctx, admin.ID, []role.UserRoleAssignment{
		{RoleID: authz.BuiltInRoleAdmin, EnvironmentID: nil},
	}))

	created, err := service.CreateApiKey(ctx, admin.ID, authz.SudoPermissionSet(), apikey.CreateApiKey{Name: "original"})
	require.NoError(t, err)

	updated, err := service.UpdateApiKey(ctx, authz.SudoPermissionSet(), created.ID, apikey.UpdateApiKey{
		Name: new("renamed"),
		Permissions: []apikey.PermissionGrant{
			{Permission: authz.PermContainersList},
			{Permission: authz.PermContainersList},
		},
	})
	require.Nil(t, updated)
	require.Error(t, err)

	stored := fetchAPIKey(t, db, created.ID)
	require.Equal(t, "original", stored.Name)
}

func TestCreateApiKeyRejectsGrantsBeyondCallerPermissions(t *testing.T) {
	ctx := context.Background()
	service, _, userService := setupAPIKeyService(t)
	user := createTestAPIKeyUser(t, ctx, userService, "user-escalation")

	callerPerms := authz.NewPermissionSet()
	callerPerms.AddGlobal(authz.PermContainersList)

	// A grant the caller does not hold must be rejected.
	_, err := service.CreateApiKey(ctx, user.ID, callerPerms, apikey.CreateApiKey{
		Name:        "escalated",
		Permissions: []apikey.PermissionGrant{{Permission: authz.PermApiKeysCreate}},
	})
	require.ErrorIs(t, err, ErrApiKeyPermissionEscalation)

	// A grant within the caller's set succeeds.
	created, err := service.CreateApiKey(ctx, user.ID, callerPerms, apikey.CreateApiKey{
		Name:        "allowed",
		Permissions: []apikey.PermissionGrant{{Permission: authz.PermContainersList}},
	})
	require.NoError(t, err)
	require.Equal(t, ApiKeyKindScoped, created.Kind)
}

func TestApiKeyGrantsAreCappedByOwnerRoles(t *testing.T) {
	ctx := context.Background()
	db := setupAPIKeyServiceTestDB(t)

	roleSvc := role.NewRoleService(db)
	require.NoError(t, roleSvc.EnsureBuiltInRoles(ctx))
	userSvc := user.NewUserService(db).WithRoleService(roleSvc)
	service := NewApiKeyService(db, userSvc).WithRoleService(roleSvc)
	// Owner has no roles at all — their permission ceiling is empty.
	owner := createTestAPIKeyUser(t, ctx, userSvc, "roleless-owner", "roleless-owner")

	// Create path: even a sudo caller cannot mint a key above the owner's roles.
	_, err := service.CreateApiKey(ctx, owner.ID, authz.SudoPermissionSet(), apikey.CreateApiKey{
		Name:        "above-owner",
		Permissions: []apikey.PermissionGrant{{Permission: authz.PermContainersList}},
	})
	require.ErrorIs(t, err, ErrApiKeyPermissionEscalation)

	// Update path: a grantless key cannot gain permissions the owner lacks.
	created, err := service.CreateApiKey(ctx, owner.ID, authz.SudoPermissionSet(), apikey.CreateApiKey{Name: "grantless"})
	require.NoError(t, err)
	_, err = service.UpdateApiKey(ctx, authz.SudoPermissionSet(), created.ID, apikey.UpdateApiKey{
		Permissions: []apikey.PermissionGrant{{Permission: authz.PermContainersList}},
	})
	require.ErrorIs(t, err, ErrApiKeyPermissionEscalation)

	// Ownerless rows (not env-bootstrap; no production path creates these)
	// have no owner ceiling to validate against — grant edits are refused.
	require.NoError(t, db.WithContext(ctx).Create(&ApiKey{
		Name:      "orphaned",
		KeyHash:   "hash",
		KeyPrefix: "arc_orph",
	}).Error)
	var orphan ApiKey
	require.NoError(t, db.WithContext(ctx).Where("key_prefix = ?", "arc_orph").First(&orphan).Error)
	_, err = service.UpdateApiKey(ctx, authz.SudoPermissionSet(), orphan.ID, apikey.UpdateApiKey{
		Permissions: []apikey.PermissionGrant{{Permission: authz.PermContainersList}},
	})
	require.ErrorIs(t, err, ErrApiKeyProtected)
}

func TestCreatePersonalApiKeyHasNoGrantsAndCannotGainAny(t *testing.T) {
	ctx := context.Background()
	service, db, userService := setupAPIKeyService(t)
	user := createTestAPIKeyUser(t, ctx, userService, "user-personal")

	created, err := service.CreatePersonalApiKey(ctx, user.ID, apikey.CreateUserApiKey{Name: "personal"})
	require.NoError(t, err)
	require.Equal(t, ApiKeyKindPersonal, created.Kind)
	require.Empty(t, created.Permissions)
	require.Equal(t, ApiKeyKindPersonal, fetchAPIKey(t, db, created.ID).Kind)

	// Attaching grants to a personal key is rejected even for sudo callers.
	_, err = service.UpdateApiKey(ctx, authz.SudoPermissionSet(), created.ID, apikey.UpdateApiKey{
		Permissions: []apikey.PermissionGrant{{Permission: authz.PermContainersList}},
	})
	require.ErrorIs(t, err, ErrApiKeyPersonalNoGrants)
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

func TestReconcileDefaultAdminAPIKeyRotationEvictsRacingCacheFill(t *testing.T) {
	ctx := context.Background()
	service, db, userService := setupAPIKeyService(t)
	adminUser := createDefaultAdminUser(t, ctx, userService)

	oldKey := "arc_bootstraprotateold1234567890"
	newKey := "arc_bootstraprotatenew1234567890"
	require.NoError(t, service.ReconcileDefaultAdminAPIKey(ctx, oldKey))
	oldRow := listAPIKeysForUser(t, db, adminUser.ID)[0]

	// Mid-transaction — after the old key's delete, before commit — simulate a
	// concurrent validation that read the old committed row and publishes it
	// with a fresh generation snapshot; the hook fires on the new managed
	// key's insert. With pre-commit invalidation this entry survived the
	// commit and kept authenticating the deleted key.
	published := false
	require.NoError(t, db.Callback().Create().After("gorm:create").Register("simulate_racing_validation", func(*gorm.DB) {
		if published {
			return
		}
		published = true
		service.storeValidatedKeyInternal(service.cacheGen.Load(), service.hashRawAPIKeyInternal(oldKey), oldRow)
	}))

	require.NoError(t, service.ReconcileDefaultAdminAPIKey(ctx, newKey))
	require.True(t, published)

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

	userCreated, err := service.CreateApiKey(ctx, adminUser.ID, authz.SudoPermissionSet(), apikey.CreateApiKey{Name: "manual-key"})
	require.NoError(t, err)

	require.NoError(t, service.ReconcileDefaultAdminAPIKey(ctx, "arc_bootstrapmanualsafe1234567890"))

	apiKeys := listAPIKeysForUser(t, db, adminUser.ID)
	require.Len(t, apiKeys, 2)

	foundUserKey := false
	foundManagedKey := false
	for _, apiKey := range apiKeys {
		if apiKey.ID == userCreated.ID {
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
	require.Equal(t, first.ID, apiKeys[0].ID)
}

func TestReconcileDefaultAdminAPIKeySkipsWhenDefaultAdminMissing(t *testing.T) {
	ctx := context.Background()
	service, db, _ := setupAPIKeyService(t)

	err := service.ReconcileDefaultAdminAPIKey(ctx, "arc_bootstrapmissing1234567890")
	require.NoError(t, err)

	var count int64
	err = db.WithContext(ctx).Model(&ApiKey{}).Count(&count).Error
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

	created, err := service.CreateApiKey(ctx, user.ID, authz.SudoPermissionSet(), apikey.CreateApiKey{Name: "validate-key"})
	require.NoError(t, err)
	require.Nil(t, fetchAPIKey(t, db, created.ID).LastUsedAt)

	validatedUser, err := service.ValidateApiKey(ctx, created.Key)
	require.NoError(t, err)
	require.Equal(t, user.ID, validatedUser.ID)

	apiKey := fetchAPIKey(t, db, created.ID)
	require.NotNil(t, apiKey.LastUsedAt)
}

func TestValidateAPIKeyCacheSkipsHashValidationAndInvalidatesOnRevoke(t *testing.T) {
	ctx := context.Background()
	service, db, userService := setupAPIKeyService(t)
	user := createTestAPIKeyUser(t, ctx, userService, "user-cached")

	created, err := service.CreateApiKey(ctx, user.ID, authz.SudoPermissionSet(), apikey.CreateApiKey{Name: "cached-key"})
	require.NoError(t, err)

	_, err = service.ValidateApiKey(ctx, created.Key)
	require.NoError(t, err)

	// Corrupt the stored hash: a second validation can only succeed by hitting
	// the validated-key cache and skipping the Argon2id check.
	require.NoError(t, db.Model(&ApiKey{}).Where("id = ?", created.ID).Update("key_hash", "corrupted").Error)

	validatedUser, err := service.ValidateApiKey(ctx, created.Key)
	require.NoError(t, err)
	require.Equal(t, user.ID, validatedUser.ID)

	// Revocation must reject immediately despite the cache.
	require.NoError(t, service.DeleteApiKey(ctx, created.ID))
	_, err = service.ValidateApiKey(ctx, created.Key)
	require.ErrorIs(t, err, ErrApiKeyInvalid)
}

func TestValidateAPIKeyCacheFillDroppedWhenRevocationRacesValidation(t *testing.T) {
	ctx := context.Background()
	service, db, userService := setupAPIKeyService(t)
	user := createTestAPIKeyUser(t, ctx, userService, "user-fill-race")

	created, err := service.CreateApiKey(ctx, user.ID, authz.SudoPermissionSet(), apikey.CreateApiKey{Name: "fill-race-key"})
	require.NoError(t, err)

	// Simulate a validation whose DB read happened before the revocation:
	// snapshot the generation and the row, revoke, then attempt the publish
	// with the stale snapshot. The publish must be dropped, not accepted.
	gen := service.cacheGen.Load()
	stored := fetchAPIKey(t, db, created.ID)
	require.NoError(t, service.DeleteApiKey(ctx, created.ID))
	service.storeValidatedKeyInternal(gen, service.hashRawAPIKeyInternal(created.Key), stored)

	_, err = service.ValidateApiKey(ctx, created.Key)
	require.ErrorIs(t, err, ErrApiKeyInvalid)
}

func TestValidateAPIKeyDebouncesLastUsedWrites(t *testing.T) {
	ctx := context.Background()
	service, db, userService := setupAPIKeyService(t)
	user := createTestAPIKeyUser(t, ctx, userService, "user-debounce")

	created, err := service.CreateApiKey(ctx, user.ID, authz.SudoPermissionSet(), apikey.CreateApiKey{Name: "debounce-key"})
	require.NoError(t, err)

	_, err = service.ValidateApiKey(ctx, created.Key)
	require.NoError(t, err)
	require.NotNil(t, fetchAPIKey(t, db, created.ID).LastUsedAt)

	// Clear the column directly; a validation inside the debounce window must
	// not issue another write.
	require.NoError(t, db.Model(&ApiKey{}).Where("id = ?", created.ID).Update("last_used_at", nil).Error)

	_, err = service.ValidateApiKey(ctx, created.Key)
	require.NoError(t, err)
	require.Nil(t, fetchAPIKey(t, db, created.ID).LastUsedAt)
}

func TestValidateAPIKeyDebounceReleasedOnWriteFailure(t *testing.T) {
	ctx := context.Background()
	service, db, userService := setupAPIKeyService(t)
	user := createTestAPIKeyUser(t, ctx, userService, "user-debounce-retry")

	created, err := service.CreateApiKey(ctx, user.ID, authz.SudoPermissionSet(), apikey.CreateApiKey{Name: "debounce-retry-key"})
	require.NoError(t, err)

	canceledCtx, cancel := context.WithCancel(ctx)
	cancel()
	service.markApiKeyUsedDebouncedInternal(canceledCtx, created.ID)
	require.Nil(t, fetchAPIKey(t, db, created.ID).LastUsedAt)

	// The failed write must not leave the key debounced: the next use inside
	// the window must retry and persist.
	service.markApiKeyUsedDebouncedInternal(ctx, created.ID)
	require.NotNil(t, fetchAPIKey(t, db, created.ID).LastUsedAt)
}

func TestValidateAPIKeyLastUsedUpdateIsRequestScoped(t *testing.T) {
	ctx := context.Background()
	service, db, userService := setupAPIKeyService(t)
	user := createTestAPIKeyUser(t, ctx, userService, "user-request-scoped")

	created, err := service.CreateApiKey(ctx, user.ID, authz.SudoPermissionSet(), apikey.CreateApiKey{Name: "request-scoped-key"})
	require.NoError(t, err)

	updateStarted := make(chan struct{})
	releaseUpdate := make(chan struct{})
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("block_api_key_last_used_update", func(*gorm.DB) {
		close(updateStarted)
		<-releaseUpdate
	}))

	validationDone := make(chan error, 1)
	go func() {
		_, err := service.ValidateApiKey(ctx, created.Key)
		validationDone <- err
	}()

	select {
	case <-updateStarted:
	case <-time.After(time.Second):
		close(releaseUpdate)
		require.FailNow(t, "last-used update did not start")
	}

	select {
	case err := <-validationDone:
		close(releaseUpdate)
		require.NoError(t, err)
		require.FailNow(t, "validation returned before its last-used update completed")
	case <-time.After(50 * time.Millisecond):
	}

	close(releaseUpdate)
	require.NoError(t, <-validationDone)
}

func TestMarkAPIKeyUsedHonorsRequestCancellation(t *testing.T) {
	ctx := context.Background()
	service, db, userService := setupAPIKeyService(t)
	user := createTestAPIKeyUser(t, ctx, userService, "user-canceled-usage")

	created, err := service.CreateApiKey(ctx, user.ID, authz.SudoPermissionSet(), apikey.CreateApiKey{Name: "canceled-usage-key"})
	require.NoError(t, err)

	canceledCtx, cancel := context.WithCancel(ctx)
	cancel()
	err = service.markApiKeyUsedInternal(canceledCtx, created.ID)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, fetchAPIKey(t, db, created.ID).LastUsedAt)
}

func TestMarkAPIKeyUsedReturnsDatabaseErrors(t *testing.T) {
	ctx := context.Background()
	service, db, userService := setupAPIKeyService(t)
	user := createTestAPIKeyUser(t, ctx, userService, "user-usage-error")

	created, err := service.CreateApiKey(ctx, user.ID, authz.SudoPermissionSet(), apikey.CreateApiKey{Name: "usage-error-key"})
	require.NoError(t, err)

	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	err = service.markApiKeyUsedInternal(ctx, created.ID)
	require.Error(t, err)
	require.ErrorContains(t, err, "failed to update API key last-used timestamp")
}

func TestValidateAPIKeyRepeatedAuthenticationDoesNotGrowGoroutines(t *testing.T) {
	ctx := context.Background()
	service, _, userService := setupAPIKeyService(t)
	user := createTestAPIKeyUser(t, ctx, userService, "user-repeated-auth")

	created, err := service.CreateApiKey(ctx, user.ID, authz.SudoPermissionSet(), apikey.CreateApiKey{Name: "repeated-auth-key"})
	require.NoError(t, err)

	before := runtime.NumGoroutine()
	for range 20 {
		validatedUser, err := service.ValidateApiKey(ctx, created.Key)
		require.NoError(t, err)
		require.Equal(t, user.ID, validatedUser.ID)
	}
	runtime.Gosched()
	after := runtime.NumGoroutine()
	t.Logf("goroutines before repeated authentication: %d; after: %d", before, after)
	require.LessOrEqual(t, after, before+1)
}

func TestGetEnvironmentByAPIKeyUpdatesLastUsedAt(t *testing.T) {
	ctx := context.Background()
	service, db, _ := setupAPIKeyService(t)

	created, err := service.CreateEnvironmentApiKey(ctx, "env-123")
	require.NoError(t, err)
	require.Nil(t, fetchAPIKey(t, db, created.ID).LastUsedAt)

	environmentID, err := service.GetEnvironmentByApiKey(ctx, created.Key)
	require.NoError(t, err)
	require.NotNil(t, environmentID)
	require.Equal(t, "env-123", *environmentID)

	apiKey := fetchAPIKey(t, db, created.ID)
	require.NotNil(t, apiKey.LastUsedAt)
}

func TestApiKeyProtectedWhileEnvironmentReferenced(t *testing.T) {
	ctx := context.Background()
	service, db, _ := setupAPIKeyService(t)

	created, err := service.CreateEnvironmentApiKey(ctx, "env-referenced")
	require.NoError(t, err)

	env := &testEnvironmentRow{
		BaseModel: database.BaseModel{ID: "env-referenced"},
		Name:      "referenced",
		ApiUrl:    "http://localhost:2375",
		ApiKeyID:  &created.ID,
	}
	require.NoError(t, db.WithContext(ctx).Create(env).Error)

	// Referenced by the environment: protected from update and delete.
	require.ErrorIs(t, service.DeleteApiKey(ctx, created.ID), ErrApiKeyProtected)
	_, err = service.UpdateApiKey(ctx, authz.SudoPermissionSet(), created.ID, apikey.UpdateApiKey{Name: new("renamed")})
	require.ErrorIs(t, err, ErrApiKeyProtected)

	// Legacy pre-046 bootstrap rows carry an owner; the reference alone must
	// still protect them.
	legacyUserID := "legacy-owner"
	legacy := &ApiKey{
		BaseModel:     database.BaseModel{ID: "legacy-key"},
		Name:          "Environment Bootstrap Key - legacy",
		KeyHash:       "hash",
		KeyPrefix:     "arc_lgcy",
		Kind:          ApiKeyKindScoped,
		UserID:        &legacyUserID,
		EnvironmentID: new("env-referenced"),
	}
	require.NoError(t, db.WithContext(ctx).Create(legacy).Error)
	require.NoError(t, db.WithContext(ctx).Model(&testEnvironmentRow{}).
		Where("id = ?", env.ID).Update("api_key_id", legacy.ID).Error)
	require.ErrorIs(t, service.DeleteApiKey(ctx, legacy.ID), ErrApiKeyProtected)

	// The first key is no longer referenced (simulates regeneration): stale
	// keys are deletable.
	require.NoError(t, service.DeleteApiKey(ctx, created.ID))
}

func TestValidateAPIKeyInvalidDoesNotUpdateLastUsedAt(t *testing.T) {
	ctx := context.Background()
	service, db, userService := setupAPIKeyService(t)
	user := createTestAPIKeyUser(t, ctx, userService, "user-invalid")

	created, err := service.CreateApiKey(ctx, user.ID, authz.SudoPermissionSet(), apikey.CreateApiKey{Name: "invalid-key"})
	require.NoError(t, err)

	_, err = service.ValidateApiKey(ctx, invalidateAPIKey(created.Key))
	require.ErrorIs(t, err, ErrApiKeyInvalid)

	apiKey := fetchAPIKey(t, db, created.ID)
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
	service, db, _ := setupAPIKeyService(t)

	created, err := service.CreateEnvironmentApiKey(ctx, "env-expired")
	require.NoError(t, err)

	expiredAt := time.Now().Add(-time.Minute)
	err = db.WithContext(ctx).Model(&ApiKey{}).Where("id = ?", created.ID).Update("expires_at", expiredAt).Error
	require.NoError(t, err)

	_, err = service.GetEnvironmentByApiKey(ctx, created.Key)
	require.ErrorIs(t, err, ErrApiKeyExpired)

	apiKey := fetchAPIKey(t, db, created.ID)
	require.Nil(t, apiKey.LastUsedAt)
}

func TestCreateEnvironmentApiKeySeedsAllPermissionsScopedToEnv(t *testing.T) {
	ctx := context.Background()
	db := setupAPIKeyServiceTestDB(t)
	require.NoError(t, db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_akp_uniq ON api_key_permissions(api_key_id, permission, COALESCE(environment_id, ''))").Error)

	roleSvc := role.NewRoleService(db)
	require.NoError(t, roleSvc.EnsureBuiltInRoles(ctx))
	userSvc := user.NewUserService(db).WithRoleService(roleSvc)
	service := NewApiKeyService(db, userSvc).WithRoleService(roleSvc)
	admin := createTestAPIKeyUser(t, ctx, userSvc, "admin-env-bootstrap", "admin-env-bootstrap")
	require.NoError(t, roleSvc.SetUserAssignments(ctx, admin.ID, []role.UserRoleAssignment{
		{RoleID: authz.BuiltInRoleAdmin, EnvironmentID: nil},
	}))

	envID := "env-bootstrap-test"
	created, err := service.CreateEnvironmentApiKey(ctx, envID)
	require.NoError(t, err)

	// Resolve the per-key permission set and confirm every permission is
	// present, scoped to the bootstrap env (not global).
	ps, err := roleSvc.ResolveApiKeyPermissions(ctx, created.ID)
	require.NoError(t, err)
	require.Empty(t, ps.Global, "bootstrap key permissions must land in PerEnv, not Global")
	envPerms, ok := ps.PerEnv[envID]
	require.True(t, ok)
	for _, p := range authz.AllPermissions() {
		_, has := envPerms[p]
		require.True(t, has, "missing permission %s on bootstrap key", p)
	}
}

func TestBackfillApiKeyPermissionsRepairsExistingBootstrapKey(t *testing.T) {
	ctx := context.Background()
	db := setupAPIKeyServiceTestDB(t)
	require.NoError(t, db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_akp_uniq ON api_key_permissions(api_key_id, permission, COALESCE(environment_id, ''))").Error)

	roleSvc := role.NewRoleService(db)
	require.NoError(t, roleSvc.EnsureBuiltInRoles(ctx))
	userSvc := user.NewUserService(db).WithRoleService(roleSvc)
	service := NewApiKeyService(db, userSvc).WithRoleService(roleSvc)

	// Simulate a pre-existing env-bootstrap key with NO permission grants
	// (e.g., created on a deployment where the per-key seed step failed).
	envID := "env-broken-bootstrap"
	require.NoError(t, db.WithContext(ctx).Create(&ApiKey{
		Name:          "Environment Bootstrap Key - broken",
		KeyHash:       "hash",
		KeyPrefix:     "arc_brkn",
		EnvironmentID: &envID,
	}).Error)

	// A user-owned key with a single deliberate grant must NOT be rehydrated
	// from the owner's effective permissions by the backfill.
	owner := createTestAPIKeyUser(t, ctx, userSvc, "backfill-scoped-owner")
	scopedKey := ApiKey{
		Name:      "Custom scoped key",
		KeyHash:   "scoped-hash",
		KeyPrefix: "arc_scpd",
		Kind:      ApiKeyKindScoped,
		UserID:    &owner.ID,
	}
	require.NoError(t, db.WithContext(ctx).Create(&scopedKey).Error)
	require.NoError(t, db.WithContext(ctx).Create(&role.ApiKeyPermission{
		ApiKeyID:   scopedKey.ID,
		Permission: authz.PermTemplatesRead,
	}).Error)

	require.NoError(t, service.BackfillApiKeyPermissions(ctx))

	// The backfill should have populated the env-scoped perms retroactively.
	var keys []ApiKey
	require.NoError(t, db.WithContext(ctx).Where("environment_id = ?", envID).Find(&keys).Error)
	require.Len(t, keys, 1)
	ps, err := roleSvc.ResolveApiKeyPermissions(ctx, keys[0].ID)
	require.NoError(t, err)
	envPerms, ok := ps.PerEnv[envID]
	require.True(t, ok)
	require.Len(t, envPerms, len(authz.AllPermissions()))

	var scopedPerms []role.ApiKeyPermission
	require.NoError(t, db.WithContext(ctx).Where("api_key_id = ?", scopedKey.ID).Find(&scopedPerms).Error)
	require.Len(t, scopedPerms, 1)
	require.Equal(t, authz.PermTemplatesRead, scopedPerms[0].Permission)
}

func TestBackfillPermsForKeyDeduplicatesGlobalAndEnvironmentPermissions(t *testing.T) {
	ctx := context.Background()
	db := setupAPIKeyServiceTestDB(t)

	roleSvc := role.NewRoleService(db)
	require.NoError(t, roleSvc.EnsureBuiltInRoles(ctx))
	userSvc := user.NewUserService(db).WithRoleService(roleSvc)
	service := NewApiKeyService(db, userSvc).WithRoleService(roleSvc)

	admin := createTestAPIKeyUser(t, ctx, userSvc, "admin", "admin")
	require.NoError(t, roleSvc.SetUserAssignments(ctx, admin.ID, []role.UserRoleAssignment{
		{RoleID: authz.BuiltInRoleAdmin},
	}))
	owner := createTestAPIKeyUser(t, ctx, userSvc, "api-key-owner")
	envID := "env-1"
	now := time.Now()
	require.NoError(t, db.WithContext(ctx).Create(&testEnvironmentRow{
		BaseModel: database.BaseModel{ID: envID, CreatedAt: now, UpdatedAt: &now},
		Name:      "env-" + envID,
		ApiUrl:    "http://localhost:3552",
		Status:    "online",
		Enabled:   true,
	}).Error)

	require.NoError(t, roleSvc.SetUserAssignments(ctx, owner.ID, []role.UserRoleAssignment{
		{RoleID: authz.BuiltInRoleViewer, EnvironmentID: nil},
		{RoleID: authz.BuiltInRoleEditor, EnvironmentID: &envID},
	}))

	perms, err := service.backfillPermsForKeyInternal(ctx, db.WithContext(ctx), ApiKey{
		UserID:        &owner.ID,
		EnvironmentID: &envID,
	})
	require.NoError(t, err)
	require.Contains(t, perms, authz.PermContainersList)
	count := 0
	for _, p := range perms {
		if p == authz.PermContainersList {
			count++
		}
	}
	require.Equal(t, 1, count)
}

func TestGetEnvironmentByAPIKeyRecentLastUsedAtDoesNotRewriteImmediately(t *testing.T) {
	ctx := context.Background()
	service, db, _ := setupAPIKeyService(t)

	created, err := service.CreateEnvironmentApiKey(ctx, "env-456")
	require.NoError(t, err)

	recent := time.Now().Add(-time.Minute)
	err = db.WithContext(ctx).Model(&ApiKey{}).Where("id = ?", created.ID).Update("last_used_at", recent).Error
	require.NoError(t, err)

	before := fetchAPIKey(t, db, created.ID)
	require.NotNil(t, before.LastUsedAt)

	environmentID, err := service.GetEnvironmentByApiKey(ctx, created.Key)
	require.NoError(t, err)
	require.NotNil(t, environmentID)
	require.Equal(t, "env-456", *environmentID)

	after := fetchAPIKey(t, db, created.ID)
	require.NotNil(t, after.LastUsedAt)
	require.Equal(t, before.LastUsedAt.UTC().Unix(), after.LastUsedAt.UTC().Unix())
}

// testEnvironmentRow is a minimal stand-in for environment.Environment: the
// environment package imports apikey, so this in-package test cannot import it.
type testEnvironmentRow struct {
	database.BaseModel
	Name        string
	ApiUrl      string `gorm:"column:api_url"`
	Status      string
	Enabled     bool
	AccessToken *string `gorm:"column:access_token"`
	ApiKeyID    *string `gorm:"column:api_key_id"`
}

func (testEnvironmentRow) TableName() string { return "environments" }
