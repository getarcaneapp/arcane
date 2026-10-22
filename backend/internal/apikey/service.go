package apikey

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/samber/hot"
	"go.getarcane.app/kit/normalization"
	"go.getarcane.app/kit/pkg"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/internal/role"
	"github.com/getarcaneapp/arcane/backend/v3/internal/user"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/dbutil"
)

const (
	apiKeyPrefix              = "arc_"
	apiKeyLength              = 32
	apiKeyPrefixLen           = 8
	apiKeyLastUsedWriteWindow = 5 * time.Minute

	managedByAdminBootstrap = "admin_account_default_api_key"
	defaultAdminUsername    = "arcane"
	defaultAdminAPIKeyName  = "Static Admin API Key"

	// ErrApiKeyPermissionEscalation is returned when a caller attempts to grant an
	// API key permissions they themselves do not hold.

	// ErrApiKeyPersonalNoGrants is returned when a caller attempts to attach
	// permission grants to a personal key, which has none of its own — it inherits
	// the owner's role permissions at authentication time.

)

var (
	ErrApiKeyNotFound  = errors.New("API key not found")
	ErrApiKeyExpired   = errors.New("API key has expired")
	ErrApiKeyInvalid   = errors.New("invalid API key")
	ErrApiKeyProtected = errors.New("API key is protected")

	ErrApiKeyPermissionEscalation = common.Classify(common.ErrForbidden, errors.New("cannot grant a permission you do not have"))

	ErrApiKeyPersonalNoGrants = errors.New("personal API keys inherit the owner's permissions and cannot carry grants")
)

var defaultAdminAPIKeyDescription = func() *string {
	return new("Environment-managed static API key for the built-in admin account")
}()

type ApiKeyService struct {
	db                *database.DB
	userService       *user.UserService
	roleService       *role.RoleService
	argon2Params      *user.Argon2Params
	validatedKeyCache *hot.HotCache[string, ApiKey]
	rejectedKeyCache  *hot.HotCache[string, struct{}]
	cacheKeyPepper    []byte
	cacheGen          atomic.Uint64
	cacheFillMu       sync.Mutex
	lastUsedMu        sync.Mutex
	lastUsedWrites    map[string]time.Time
}

func NewApiKeyService(db *database.DB, userService *user.UserService, roleService *role.RoleService) *ApiKeyService {
	pepper := make([]byte, 32)
	if _, err := rand.Read(pepper); err != nil {
		panic(fmt.Errorf("failed to generate API key cache pepper: %w", err))
	}
	return &ApiKeyService{
		cacheKeyPepper: pepper,
		db:             db,
		userService:    userService,
		roleService:    roleService,
		argon2Params:   user.DefaultArgon2Params(),
		validatedKeyCache: hot.NewHotCache[string, ApiKey](hot.LRU, 4096).
			WithTTL(15 * time.Second).
			WithJanitor().
			Build(),
		rejectedKeyCache: hot.NewHotCache[string, struct{}](hot.LRU, 4096).
			WithTTL(30 * time.Second).
			WithJanitor().
			Build(),
		lastUsedWrites: make(map[string]time.Time),
	}
}

// BackfillApiKeyPermissions ensures every ownerless (bootstrap) API key has
// its expected permission grants. Called once per boot.
//
// Per-key, not all-or-nothing: a single bootstrap key with zero grants is
// repaired even if other keys are already populated. This recovers env-
// bootstrap keys that pre-date the per-key permission feature, or that were
// created on a deployment where the original SetApiKeyPermissions call failed
// (e.g., the api_key_permissions table didn't exist yet).
//
// User-owned keys are deliberately skipped. A user-owned key with zero grants
// is an intentional "no access" state; rehydrating from the owner's effective
// permissions on every boot would clobber that. User keys are seeded at
// creation time by CreateApiKey instead.
func (s *ApiKeyService) BackfillApiKeyPermissions(ctx context.Context) error {
	// Bootstrap-class keys we'll repair if they have zero grants:
	//   - user_id IS NULL → env-bootstrap keys.
	//   - managed_by IS NOT NULL → admin-static (ADMIN_STATIC_API_KEY) keys,
	//     which DO have a user_id but are infrastructure-managed and must
	//     always carry full perms.
	// Regular user-created keys are deliberately excluded — empty grants on
	// those are an intentional "no access" state we must not overwrite.
	var keys []ApiKey
	if err := s.db.WithContext(ctx).Where("user_id IS NULL OR managed_by IS NOT NULL").Find(&keys).Error; err != nil {
		return fmt.Errorf("failed to list bootstrap api keys for backfill: %w", err)
	}
	if len(keys) == 0 {
		return nil
	}

	return dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		keyIDs := make([]string, 0, len(keys))
		for _, key := range keys {
			keyIDs = append(keyIDs, key.ID)
		}
		var grantedKeyIDs []string
		if err := tx.Model(&role.ApiKeyPermission{}).Where("api_key_id IN ?", keyIDs).Distinct().Pluck("api_key_id", &grantedKeyIDs).Error; err != nil {
			return fmt.Errorf("failed to count permissions for bootstrap api keys: %w", err)
		}
		for _, key := range keys {
			if slices.Contains(grantedKeyIDs, key.ID) {
				continue
			}
			perms, err := s.backfillPermsForKey(ctx, tx, key)
			if err != nil {
				return err
			}
			for _, p := range perms {
				if createKeyPermissionErr := tx.Create(&role.ApiKeyPermission{
					ApiKeyID:      key.ID,
					Permission:    p,
					EnvironmentID: key.EnvironmentID,
				}).Error; createKeyPermissionErr != nil {
					return fmt.Errorf("failed to seed api key permission: %w", createKeyPermissionErr)
				}
			}
			slog.InfoContext(ctx, "Backfilled missing permissions for bootstrap api key", "apiKeyId", key.ID, "permCount", len(perms), "envId", key.EnvironmentID)
		}
		return nil
	})
}

func (s *ApiKeyService) backfillPermsForKey(ctx context.Context, tx *gorm.DB, key ApiKey) ([]string, error) {
	// Static admin bootstrap keys (no owner, no env scope) get full access.
	if key.UserID == nil && key.EnvironmentID == nil {
		return authz.AllPermissions(), nil
	}
	// Environment bootstrap keys (key bound to a specific env, no owner) get
	// full access scoped to that environment via the auth bridge — replicate
	// that by granting all permissions scoped to that environment. Org-level
	// permissions land in PerEnv[envID] and are unreachable via org-level
	// checks (which always pass envID=""), so over-granting is harmless here.
	if key.UserID == nil && key.EnvironmentID != nil {
		return authz.AllPermissions(), nil
	}
	// Otherwise inherit the owner's current effective permissions.
	var owner user.User
	if err := tx.WithContext(ctx).Where("id = ?", *key.UserID).First(&owner).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to load api key owner: %w", err)
	}
	ps, err := s.roleService.ResolveUserPermissionsInDB(ctx, tx, owner.ID)
	if err != nil {
		return nil, err
	}
	// Flatten ps into a deduplicated list of permissions for this key's scope.
	seen := make(map[string]struct{}, len(ps.Global))
	for p := range ps.Global {
		seen[p] = struct{}{}
	}
	if key.EnvironmentID != nil {
		if env, ok := ps.PerEnv[*key.EnvironmentID]; ok {
			for p := range env {
				seen[p] = struct{}{}
			}
		}
	}
	out := slices.Collect(maps.Keys(seen))
	return out, nil
}

func (s *ApiKeyService) generateApiKey() (string, error) {
	bytes := make([]byte, apiKeyLength)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate API key: %w", err)
	}
	return apiKeyPrefix + hex.EncodeToString(bytes), nil
}

func parseAPIKeyPrefix(rawKey string) (string, error) {
	rawKey = strings.TrimSpace(rawKey)
	if !strings.HasPrefix(rawKey, apiKeyPrefix) {
		return "", ErrApiKeyInvalid
	}

	prefixLen := len(apiKeyPrefix) + apiKeyPrefixLen
	if len(rawKey) < prefixLen {
		return "", ErrApiKeyInvalid
	}

	return rawKey[:prefixLen], nil
}

func (s *ApiKeyService) markApiKeyUsed(ctx context.Context, keyID string) error {
	now := time.Now()
	cutoff := now.Add(-apiKeyLastUsedWriteWindow)
	if err := s.db.WithContext(ctx).
		Model(&ApiKey{}).
		Where("id = ? AND (last_used_at IS NULL OR last_used_at < ?)", keyID, cutoff).
		Update("last_used_at", now).Error; err != nil {
		return fmt.Errorf("failed to update API key last-used timestamp: %w", err)
	}
	return nil
}

// markApiKeyUsedDebounced skips the DB write entirely when this process
// already persisted last_used_at for the key within the write window, so the
// auth hot path stays read-only between windows.
func (s *ApiKeyService) markApiKeyUsedDebounced(ctx context.Context, keyID string) {
	now := time.Now()
	s.lastUsedMu.Lock()
	if last, ok := s.lastUsedWrites[keyID]; ok && now.Sub(last) < apiKeyLastUsedWriteWindow {
		s.lastUsedMu.Unlock()
		return
	}
	s.lastUsedWrites[keyID] = now
	s.lastUsedMu.Unlock()

	if err := s.markApiKeyUsed(ctx, keyID); err != nil {
		slog.WarnContext(ctx, "failed to persist API key usage", "apiKeyId", keyID, "error", err)
		// Release the reservation so the next use retries instead of silently
		// skipping writes for the rest of the window — but only if it is still
		// ours, to avoid clobbering a newer reservation made after an
		// invalidation reset the entry.
		s.lastUsedMu.Lock()
		if last, ok := s.lastUsedWrites[keyID]; ok && last.Equal(now) {
			delete(s.lastUsedWrites, keyID)
		}
		s.lastUsedMu.Unlock()
	}
}

// hashRawAPIKey derives the cache key for a raw API key. This is not
// credential storage — Argon2id in the DB is — so a fast keyed hash is
// appropriate for this hot path; the per-process pepper prevents offline
// brute-force of the digest should process memory leak.
func (s *ApiKeyService) hashRawAPIKey(rawKey string) string {
	mac := hmac.New(sha256.New, s.cacheKeyPepper)
	mac.Write([]byte(rawKey))
	return hex.EncodeToString(mac.Sum(nil))
}

// invalidateValidatedKeys evicts cached validations for the given key
// IDs (the cache is keyed by raw-key hash, so eviction scans for matching IDs)
// and resets their last_used_at debounce state.
//
// Callers must invoke this only AFTER the corresponding DB change has
// committed. Combined with the generation guard on fills, that makes the
// invariant: once this returns, no cached validation for these ids exists or
// can reappear. Requests whose cache read races the brief span between commit
// and eviction — like requests already past hash validation when the change
// commits — are the unavoidable in-flight window every auth system has; they
// are deliberately not defended against, as doing so would require locking
// every validation against every revocation.
func (s *ApiKeyService) invalidateValidatedKeys(ids ...string) {
	if s.validatedKeyCache == nil || len(ids) == 0 {
		return
	}
	idSet := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		idSet[id] = struct{}{}
	}

	// Bump the generation before evicting, under the fill mutex: any in-flight
	// validation that read the DB before this invalidation either publishes
	// before us (and is evicted below) or sees the moved generation and skips
	// its publish. Either way the stale entry cannot survive.
	s.cacheFillMu.Lock()
	s.cacheGen.Add(1)
	var cacheKeys []string
	s.validatedKeyCache.Range(func(key string, entry ApiKey) bool {
		if _, ok := idSet[entry.ID]; ok {
			cacheKeys = append(cacheKeys, key)
		}
		return true
	})
	s.validatedKeyCache.DeleteMany(cacheKeys)
	s.cacheFillMu.Unlock()

	s.lastUsedMu.Lock()
	for id := range idSet {
		delete(s.lastUsedWrites, id)
	}
	s.lastUsedMu.Unlock()
}

func (s *ApiKeyService) CreateApiKey(ctx context.Context, userID string, callerPerms *authz.PermissionSet, req types.ApiKey) (*types.ApiKeyCreatedDto, error) {
	if err := normalization.Normalize(&req); err != nil {
		return nil, err
	}
	if len(req.Permissions) == 0 {
		return nil, common.Classify(common.ErrValidation, errors.New("at least one permission is required"))
	}
	if err := validateGrantsAgainstPermissionSet(callerPerms, req.Permissions); err != nil {
		return nil, err
	}
	if err := s.validateGrantsAgainstOwner(ctx, userID, req.Permissions); err != nil {
		return nil, err
	}
	rawKey, err := s.generateApiKey()
	if err != nil {
		return nil, err
	}

	var created *types.ApiKeyCreatedDto
	if createTxErr := dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		var createErr error
		if created, createErr = s.createAPIKeyWithRawKey(ctx, tx, &userID, rawKey, req, nil, nil, ApiKeyKindScoped); createErr != nil {
			return createErr
		}
		if setApiKeyPermissionsErr := s.roleService.SetApiKeyPermissionsInDB(ctx, tx, created.ID, toApiKeyPermissionRows(created.ID, req.Permissions)); setApiKeyPermissionsErr != nil {
			return fmt.Errorf("failed to persist api key permissions: %w", setApiKeyPermissionsErr)
		}
		return nil
	}); createTxErr != nil {
		return nil, createTxErr
	}
	created.Permissions = s.loadKeyGrants(ctx, created.ID)
	return created, nil
}

// validateGrantsAgainstOwner caps grants at the key owner's role
// permissions, so no holder of apikeys:create/update — sudo included — can
// push a key above what its owner is allowed to do. Complements the caller-set
// check: the caller cap uses the credential's live context permissions, this
// one re-resolves the owner's roles from the DB. Skipped when role.RoleService is
// unavailable (bootstrap path only, where no human caller exists).
func (s *ApiKeyService) validateGrantsAgainstOwner(ctx context.Context, ownerID string, grants []types.ApiKeyPermissionGrant) error {
	if s.roleService == nil || len(grants) == 0 {
		return nil
	}
	localUser, err := s.userService.GetUserByID(ctx, ownerID)
	if err != nil {
		return fmt.Errorf("load owner for permission validation: %w", err)
	}
	ps, err := s.roleService.ResolvePermissions(ctx, localUser.ID)
	if err != nil {
		return fmt.Errorf("resolve owner permissions: %w", err)
	}
	if validateGrantsAgainstPermissionSetErr := validateGrantsAgainstPermissionSet(ps, grants); validateGrantsAgainstPermissionSetErr != nil {
		return fmt.Errorf("owner's roles do not allow this grant: %w", validateGrantsAgainstPermissionSetErr)
	}
	return nil
}

// validateGrantsAgainstPermissionSet refuses requests that would grant
// a key permissions the calling credential does not itself hold right now —
// session callers are capped by their role permissions, API-key callers by
// their own grants. Sudo callers always pass; a nil set denies everything.
func validateGrantsAgainstPermissionSet(callerPerms *authz.PermissionSet, grants []types.ApiKeyPermissionGrant) error {
	for _, g := range grants {
		envID := ""
		if g.EnvironmentID != nil {
			envID = *g.EnvironmentID
		}
		if !callerPerms.Allows(g.Permission, envID) {
			return fmt.Errorf("%s (env=%q): %w", g.Permission, envID, ErrApiKeyPermissionEscalation)
		}
	}
	return nil
}

func toApiKeyPermissionRows(apiKeyID string, grants []types.ApiKeyPermissionGrant) []role.ApiKeyPermission {
	out := make([]role.ApiKeyPermission, len(grants))
	for i, g := range grants {
		out[i] = role.ApiKeyPermission{
			ApiKeyID:      apiKeyID,
			Permission:    g.Permission,
			EnvironmentID: g.EnvironmentID,
		}
	}
	return out
}

// CreatePersonalApiKey creates a personal key for userID. Personal keys carry
// no permission grants of their own; the auth middleware resolves them to the
// owner's role permissions, so there is nothing to validate or escalate here.
func (s *ApiKeyService) CreatePersonalApiKey(ctx context.Context, userID string, req types.ApiKey) (*types.ApiKeyCreatedDto, error) {
	if err := normalization.Normalize(&req); err != nil {
		return nil, err
	}
	rawKey, err := s.generateApiKey()
	if err != nil {
		return nil, err
	}

	created, err := s.createAPIKeyWithRawKey(ctx, s.db.DB, &userID, rawKey, req, nil, nil, ApiKeyKindPersonal)
	if err != nil {
		return nil, err
	}
	created.Permissions = []types.ApiKeyPermissionGrant{}
	return created, nil
}

func (s *ApiKeyService) createAPIKeyWithRawKey(
	ctx context.Context,
	db *gorm.DB,
	userID *string,
	rawKey string,
	req types.ApiKey,
	managedBy *string,
	environmentID *string,
	kind string,
) (*types.ApiKeyCreatedDto, error) {
	if req.Name == "" {
		return nil, common.Classify(common.ErrValidation, errors.New("API key name is required"))
	}
	rawKey = strings.TrimSpace(rawKey)
	keyPrefix, err := parseAPIKeyPrefix(rawKey)
	if err != nil {
		return nil, err
	}

	keyHash, err := s.userService.HashPassword(ctx, rawKey)
	if err != nil {
		return nil, fmt.Errorf("failed to hash API key: %w", err)
	}

	ak := &ApiKey{
		Name:          req.Name,
		Description:   req.Description,
		KeyHash:       keyHash,
		KeyPrefix:     keyPrefix,
		ManagedBy:     managedBy,
		Kind:          kind,
		UserID:        userID,
		EnvironmentID: environmentID,
		ExpiresAt:     req.ExpiresAt,
	}
	if createAPIKeyErr := db.WithContext(ctx).Create(ak).Error; createAPIKeyErr != nil {
		return nil, fmt.Errorf("failed to create API key: %w", createAPIKeyErr)
	}
	s.rejectedKeyCache.Delete(s.hashRawAPIKey(rawKey))
	// A just-minted environment key is about to be linked as the
	// environment's pairing key, so present it as bootstrap already.
	return &types.ApiKeyCreatedDto{
		ApiKey: toAPIKeyDTO(ak, environmentID != nil), Key: rawKey,
	}, nil
}

func isStaticAPIKey(ak ApiKey) bool {
	return ak.ManagedBy != nil && *ak.ManagedBy == managedByAdminBootstrap
}

// isEnvironmentReferencedApiKey reports whether an environment
// currently references keyID as its pairing key (environments.api_key_id).
// Referenced keys are the live environment bootstrap credentials: editing or
// deleting one would silently break the paired edge agent the next time it
// authenticates, so they are protected. Keys orphaned by regeneration are no
// longer referenced and may be edited or deleted normally. Cascade-delete
// still applies when the environment row itself is removed.
func (s *ApiKeyService) isEnvironmentReferencedApiKey(ctx context.Context, keyID string) (bool, error) {
	var count int64
	if err := s.db.WithContext(ctx).Table("environments").
		Where("api_key_id = ?", keyID).Count(&count).Error; err != nil {
		return false, fmt.Errorf("failed to check environment references for API key: %w", err)
	}
	return count > 0, nil
}

// environmentReferencedApiKeyIDs returns the set of API key ids
// currently referenced by an environment, for tagging whole listings with a
// single query instead of one per row.
func (s *ApiKeyService) environmentReferencedApiKeyIDs(ctx context.Context) (map[string]struct{}, error) {
	var ids []string
	if err := s.db.WithContext(ctx).Table("environments").
		Where("api_key_id IS NOT NULL").Pluck("api_key_id", &ids).Error; err != nil {
		return nil, fmt.Errorf("failed to list environment-referenced API key ids: %w", err)
	}
	set := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	return set, nil
}

func toAPIKeyDTO(ak *ApiKey, isBootstrap bool) types.ApiKey {
	return types.ApiKey{
		ID:          ak.ID,
		Name:        ak.Name,
		Description: ak.Description,
		KeyPrefix:   ak.KeyPrefix,
		UserID:      ak.UserID,
		Kind:        ak.Kind,
		IsStatic:    isStaticAPIKey(*ak),
		IsBootstrap: isBootstrap,
		ExpiresAt:   ak.ExpiresAt,
		LastUsedAt:  ak.LastUsedAt,
		CreatedAt:   ak.CreatedAt,
		UpdatedAt:   ak.UpdatedAt,
	}
}

// toAPIKeyDTOWithPermissions is like toAPIKeyDTO but
// additionally attaches the key's persisted permission grants.
func toAPIKeyDTOWithPermissions(ak *ApiKey, isBootstrap bool) types.ApiKey {
	dto := toAPIKeyDTO(ak, isBootstrap)
	dto.Permissions = make([]types.ApiKeyPermissionGrant, len(ak.PermissionGrants))
	for i, grant := range ak.PermissionGrants {
		dto.Permissions[i] = types.ApiKeyPermissionGrant{
			Permission:    grant.Permission,
			EnvironmentID: grant.EnvironmentID,
		}
	}
	return dto
}

func (s *ApiKeyService) CreateDefaultAdminAPIKey(ctx context.Context, userID, rawKey string) (*types.ApiKeyCreatedDto, error) {
	return s.createAPIKeyWithRawKey(ctx, s.db.DB, &userID, rawKey, types.ApiKey{
		Name:        defaultAdminAPIKeyName,
		Description: defaultAdminAPIKeyDescription,
	}, new(managedByAdminBootstrap), nil, ApiKeyKindScoped)
}

func (s *ApiKeyService) getDefaultAdminUser(ctx context.Context) (*user.User, error) {
	adminUser, err := s.userService.GetUserByUsername(ctx, defaultAdminUsername)
	if err != nil {
		if errors.Is(err, common.ErrUserNotFound) {
			slog.WarnContext(ctx, "Default admin user not found, skipping default admin API key reconciliation", "username", defaultAdminUsername)
			return nil, nil
		}
		return nil, fmt.Errorf("failed to load default admin user: %w", err)
	}

	// The username is mutable and not proof of provenance — never mint the
	// managed full-permission key onto an account that isn't a global admin.
	if s.roleService != nil {
		perms, resolvePermissionsErr := s.roleService.ResolvePermissions(ctx, adminUser.ID)
		if resolvePermissionsErr != nil {
			return nil, fmt.Errorf("failed to resolve default admin permissions: %w", resolvePermissionsErr)
		}
		if !perms.IsGlobalAdmin() {
			slog.WarnContext(ctx, "User is not a global admin, skipping default admin API key reconciliation", "username", defaultAdminUsername)
			return nil, nil
		}
	}

	return adminUser, nil
}

func (s *ApiKeyService) listManagedAPIKeys(tx *gorm.DB, userID string) ([]ApiKey, error) {
	var managedKeys []ApiKey
	if err := tx.Where("user_id = ? AND managed_by = ?", userID, managedByAdminBootstrap).
		Order("created_at asc, id asc").
		Find(&managedKeys).Error; err != nil {
		return nil, fmt.Errorf("failed to load managed API keys: %w", err)
	}
	return managedKeys, nil
}

func (s *ApiKeyService) deleteManagedAPIKeysByIDs(tx *gorm.DB, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	if err := tx.Delete(&ApiKey{}, "id IN ?", ids).Error; err != nil {
		return fmt.Errorf("failed to delete managed API keys: %w", err)
	}
	return nil
}

func (s *ApiKeyService) findMatchingManagedAPIKey(ctx context.Context, rawKey string, managedKeys []ApiKey) (int, error) {
	for i, managedKey := range managedKeys {
		if err := s.userService.ValidatePassword(ctx, managedKey.KeyHash, rawKey); err == nil {
			return i, nil
		}
		if ctx.Err() != nil {
			return -1, ctx.Err()
		}
	}
	return -1, nil
}

func managedAPIKeyDeleteIDs(managedKeys []ApiKey, keepIndex int) []string {
	deleteIDs := make([]string, 0, len(managedKeys))
	for i, managedKey := range managedKeys {
		if i == keepIndex {
			continue
		}
		deleteIDs = append(deleteIDs, managedKey.ID)
	}
	return deleteIDs
}

func (s *ApiKeyService) updateMatchingManagedAPIKey(tx *gorm.DB, apiKeyID string) error {
	if err := tx.Model(&ApiKey{}).
		Where("id = ?", apiKeyID).
		Updates(map[string]any{
			"name":        defaultAdminAPIKeyName,
			"description": defaultAdminAPIKeyDescription,
			"managed_by":  managedByAdminBootstrap,
		}).Error; err != nil {
		return fmt.Errorf("failed to update managed API key metadata: %w", err)
	}
	return nil
}

func (s *ApiKeyService) createManagedDefaultAdminAPIKey(ctx context.Context, tx *gorm.DB, userID, rawKey string) error {
	keyPrefix, err := parseAPIKeyPrefix(rawKey)
	if err != nil {
		return err
	}

	keyHash, err := s.userService.HashPassword(ctx, rawKey)
	if err != nil {
		return fmt.Errorf("failed to hash API key: %w", err)
	}

	ak := &ApiKey{
		Name:        defaultAdminAPIKeyName,
		Description: defaultAdminAPIKeyDescription,
		KeyHash:     keyHash,
		KeyPrefix:   keyPrefix,
		ManagedBy:   new(managedByAdminBootstrap),
		UserID:      &userID,
	}
	if createManagedKeyErr := tx.Create(ak).Error; createManagedKeyErr != nil {
		return fmt.Errorf("failed to create managed API key: %w", createManagedKeyErr)
	}
	return nil
}

// reconcileManagedAPIKeys applies the desired managed-key state inside tx and
// returns the ids whose cached validations the caller must invalidate AFTER
// the transaction commits. Invalidating pre-commit is ineffective: a concurrent
// validation can still read the old committed row, snapshot the already-bumped
// generation, and republish the entry so it survives the commit.
func (s *ApiKeyService) reconcileManagedAPIKeys(ctx context.Context, tx *gorm.DB, userID, rawKey string) ([]string, error) {
	managedKeys, err := s.listManagedAPIKeys(tx, userID)
	if err != nil {
		return nil, err
	}

	if rawKey == "" {
		deleteIDs := managedAPIKeyDeleteIDs(managedKeys, -1)
		return deleteIDs, s.deleteManagedAPIKeysByIDs(tx, deleteIDs)
	}

	matchingIndex, err := s.findMatchingManagedAPIKey(ctx, rawKey, managedKeys)
	if err != nil {
		return nil, err
	}
	if matchingIndex >= 0 {
		if updateMatchingManagedAPIKeyErr := s.updateMatchingManagedAPIKey(tx, managedKeys[matchingIndex].ID); updateMatchingManagedAPIKeyErr != nil {
			return nil, updateMatchingManagedAPIKeyErr
		}
		deleteIDs := managedAPIKeyDeleteIDs(managedKeys, matchingIndex)
		if deleteManagedAPIKeysByIDsErr := s.deleteManagedAPIKeysByIDs(tx, deleteIDs); deleteManagedAPIKeysByIDsErr != nil {
			return nil, deleteManagedAPIKeysByIDsErr
		}
		// The kept key's metadata changed too; its cached copy is stale.
		return append(deleteIDs, managedKeys[matchingIndex].ID), nil
	}

	deleteIDs := managedAPIKeyDeleteIDs(managedKeys, -1)
	if deleteRemovedManagedKeysErr := s.deleteManagedAPIKeysByIDs(tx, deleteIDs); deleteRemovedManagedKeysErr != nil {
		return nil, deleteRemovedManagedKeysErr
	}

	return deleteIDs, s.createManagedDefaultAdminAPIKey(ctx, tx, userID, rawKey)
}

func (s *ApiKeyService) ReconcileDefaultAdminAPIKey(ctx context.Context, rawKey string) error {
	rawKey = strings.TrimSpace(rawKey)

	adminUser, err := s.getDefaultAdminUser(ctx)
	if err != nil || adminUser == nil {
		return err
	}

	var affectedIDs []string
	if transactionErr := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		affectedIDs, err = s.reconcileManagedAPIKeys(ctx, tx, adminUser.ID, rawKey)
		return err
	}); transactionErr != nil {
		// Rolled back: the DB is unchanged, so cached entries are still valid.
		return transactionErr
	}
	s.invalidateValidatedKeys(affectedIDs...)
	s.rejectedKeyCache.Delete(s.hashRawAPIKey(rawKey))
	return nil
}

func (s *ApiKeyService) CreateEnvironmentApiKey(ctx context.Context, environmentID string) (*types.ApiKeyCreatedDto, error) {
	rawKey, err := s.generateApiKey()
	if err != nil {
		return nil, err
	}

	envIDShort := environmentID
	if len(environmentID) > 8 {
		envIDShort = environmentID[:8]
	}
	name := "Environment Bootstrap Key - " + envIDShort
	created, err := s.createAPIKeyWithRawKey(ctx, s.db.DB, nil, rawKey, types.ApiKey{
		Name:        name,
		Description: new("Auto-generated key for environment pairing"),
	}, nil, &environmentID, ApiKeyKindScoped)
	if err != nil {
		return nil, err
	}
	// Env-bootstrap keys are infrastructure-level credentials used by the agent
	// pairing flow; they must always carry every permission scoped to their
	// environment. Without this seed, the per-key permission resolver returns
	// an empty set on any request authenticated by this key and every
	// downstream RequirePermission check fails with 403.
	if s.roleService != nil {
		all := authz.AllPermissions()
		grants := make([]role.ApiKeyPermission, len(all))
		for i, p := range all {
			grants[i] = role.ApiKeyPermission{
				ApiKeyID:      created.ID,
				Permission:    p,
				EnvironmentID: &environmentID,
			}
		}
		if setApiKeyPermissionsErr := s.roleService.SetApiKeyPermissions(ctx, created.ID, grants); setApiKeyPermissionsErr != nil {
			return nil, fmt.Errorf("failed to persist environment bootstrap key permissions: %w", setApiKeyPermissionsErr)
		}
		// Re-load grants into the response DTO so callers see the seeded
		// permissions immediately, not null.
		created.Permissions = s.loadKeyGrants(ctx, created.ID)
	}
	return created, nil
}

func (s *ApiKeyService) GetApiKey(ctx context.Context, id string) (*types.ApiKey, error) {
	var ak ApiKey
	query := s.db.WithContext(ctx)
	if s.roleService != nil {
		query = query.Preload("PermissionGrants")
	}
	if err := query.Where("id = ?", id).First(&ak).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrApiKeyNotFound
		}
		return nil, fmt.Errorf("failed to get API key: %w", err)
	}
	isBootstrap, err := s.isEnvironmentReferencedApiKey(ctx, ak.ID)
	if err != nil {
		return nil, err
	}
	return new(toAPIKeyDTOWithPermissions(&ak, isBootstrap)), nil
}

func (s *ApiKeyService) ListApiKeys(ctx context.Context, params pagination.QueryParams) ([]types.ApiKey, pagination.Response, error) {
	var apiKeys []ApiKey
	query := s.db.WithContext(ctx).Model(&ApiKey{})
	if s.roleService != nil {
		query = query.Preload("PermissionGrants")
	}

	if term := strings.TrimSpace(params.Search); term != "" {
		searchPattern := "%" + term + "%"
		query = query.Where(
			"name LIKE ? OR COALESCE(description, '') LIKE ?",
			searchPattern, searchPattern,
		)
	}

	paginationResp, err := pagination.PaginateAndSortDB(params, query, &apiKeys)
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to paginate API keys: %w", err)
	}

	referenced, err := s.environmentReferencedApiKeyIDs(ctx)
	if err != nil {
		return nil, pagination.Response{}, err
	}

	result := make([]types.ApiKey, len(apiKeys))
	for i := range apiKeys {
		// Include per-key permission grants so the edit form preloads with the
		// key's actual current grants. Without this, the form starts empty and
		// "Save" would wipe whatever grants the DB has.
		_, isBootstrap := referenced[apiKeys[i].ID]
		result[i] = toAPIKeyDTOWithPermissions(&apiKeys[i], isBootstrap)
	}

	return result, paginationResp, nil
}

// ListApiKeysByUser returns every non-static, non-bootstrap API key owned by
// userID. Used by the self-service personal-keys flow. Keys referenced by an
// environment are hidden even when they carry an owner (legacy bootstrap rows
// created before user_id became nullable) — self-service must never touch a
// live pairing credential.
func (s *ApiKeyService) ListApiKeysByUser(ctx context.Context, userID string) ([]types.ApiKey, error) {
	var apiKeys []ApiKey
	query := s.db.WithContext(ctx)
	if s.roleService != nil {
		query = query.Preload("PermissionGrants")
	}
	if err := query.Where("user_id = ?", userID).
		Order("created_at DESC").
		Find(&apiKeys).Error; err != nil {
		return nil, fmt.Errorf("failed to list user api keys: %w", err)
	}

	referenced, err := s.environmentReferencedApiKeyIDs(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]types.ApiKey, 0, len(apiKeys))
	for i := range apiKeys {
		if _, isBootstrap := referenced[apiKeys[i].ID]; isBootstrap || isStaticAPIKey(apiKeys[i]) {
			continue
		}
		result = append(result, toAPIKeyDTOWithPermissions(&apiKeys[i], false))
	}
	return result, nil
}

func (s *ApiKeyService) UpdateApiKey(ctx context.Context, callerPerms *authz.PermissionSet, id string, req types.ApiKey, submitted map[string]bool) (*types.ApiKey, error) {
	var ak ApiKey
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&ak).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrApiKeyNotFound
		}
		return nil, fmt.Errorf("failed to get API key: %w", err)
	}
	if isStaticAPIKey(ak) {
		return nil, ErrApiKeyProtected
	}
	if referenced, err := s.isEnvironmentReferencedApiKey(ctx, ak.ID); err != nil {
		return nil, err
	} else if referenced {
		return nil, ErrApiKeyProtected
	}

	req = types.ApiKey{
		Name:        kit.Ternary(submitted["name"], req.Name, ak.Name),
		Description: kit.Ternary(submitted["description"], req.Description, ak.Description),
		ExpiresAt:   kit.Ternary(submitted["expiresAt"], req.ExpiresAt, ak.ExpiresAt),
		Permissions: req.Permissions,
	}
	if err := normalization.Normalize(&req); err != nil {
		return nil, err
	}

	if submitted["permissions"] {
		if ak.Kind == ApiKeyKindPersonal {
			return nil, ErrApiKeyPersonalNoGrants
		}
		// Grants are capped twice: by the calling credential's effective
		// permissions (a holder of apikeys:update cannot grant anything the
		// credential itself cannot do right now) AND by the key owner's role
		// permissions (no caller can push another user's key above what that
		// user's own roles allow).
		if err := validateGrantsAgainstPermissionSet(callerPerms, req.Permissions); err != nil {
			return nil, err
		}
		if ak.UserID == nil {
			// No owner to cap against. Ownerless rows are (possibly stale)
			// environment bootstrap keys — name/description edits are harmless,
			// but refuse grant edits rather than fall back to a weaker
			// caller-only check.
			return nil, ErrApiKeyProtected
		}
		if err := s.validateGrantsAgainstOwner(ctx, *ak.UserID, req.Permissions); err != nil {
			return nil, err
		}
	}

	changes := ApiKey{
		BaseModel: database.BaseModel{
			UpdatedAt: new(time.Now()),
		},
		Name:        req.Name,
		Description: req.Description,
		ExpiresAt:   req.ExpiresAt,
	}

	permissionsUpdated := false
	if err := dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		if err := tx.Model(&ApiKey{}).
			Where("id = ?", ak.ID).
			Select("name", "description", "expires_at", "updated_at").
			Updates(&changes).Error; err != nil {
			return fmt.Errorf("failed to update API key: %w", err)
		}
		if submitted["permissions"] && s.roleService != nil {
			if err := s.roleService.SetApiKeyPermissionsInDB(ctx, tx, ak.ID, toApiKeyPermissionRows(ak.ID, req.Permissions)); err != nil {
				return fmt.Errorf("failed to update api key permissions: %w", err)
			}
			permissionsUpdated = true
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if permissionsUpdated {
		s.roleService.InvalidateApiKey(ak.ID)
	}
	s.invalidateValidatedKeys(ak.ID)

	return &types.ApiKey{
		ID:          ak.ID,
		Name:        changes.Name,
		Description: changes.Description,
		KeyPrefix:   ak.KeyPrefix,
		UserID:      ak.UserID,
		Kind:        ak.Kind,
		IsStatic:    isStaticAPIKey(ak),
		IsBootstrap: false, // an environment-referenced key can't reach this return
		ExpiresAt:   changes.ExpiresAt,
		LastUsedAt:  ak.LastUsedAt,
		CreatedAt:   ak.CreatedAt,
		UpdatedAt:   changes.UpdatedAt,
		Permissions: s.loadKeyGrants(ctx, ak.ID),
	}, nil
}

// loadKeyGrants returns the persisted permission grants for an API key.
// Always returns a non-nil slice (possibly empty) so the JSON-marshalled DTO
// renders as `"permissions": []` rather than `"permissions": null`, which the
// frontend treats differently (null hides the picker, [] shows it empty).
// DB errors are logged and yield an empty slice — the caller never sees nil.
func (s *ApiKeyService) loadKeyGrants(ctx context.Context, apiKeyID string) []types.ApiKeyPermissionGrant {
	empty := []types.ApiKeyPermissionGrant{}
	if s.roleService == nil {
		return empty
	}
	var rows []role.ApiKeyPermission
	if err := s.db.WithContext(ctx).Where("api_key_id = ?", apiKeyID).Find(&rows).Error; err != nil {
		slog.WarnContext(ctx, "failed to load api key permission grants", "apiKeyId", apiKeyID, "error", err)
		return empty
	}
	out := make([]types.ApiKeyPermissionGrant, len(rows))
	for i, r := range rows {
		out[i] = types.ApiKeyPermissionGrant{Permission: r.Permission, EnvironmentID: r.EnvironmentID}
	}
	return out
}

func (s *ApiKeyService) DeleteApiKey(ctx context.Context, id string) error {
	var apiKey ApiKey
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&apiKey).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrApiKeyNotFound
		}
		return fmt.Errorf("failed to load API key: %w", err)
	}
	if isStaticAPIKey(apiKey) {
		return ErrApiKeyProtected
	}
	if referenced, err := s.isEnvironmentReferencedApiKey(ctx, apiKey.ID); err != nil {
		return err
	} else if referenced {
		return ErrApiKeyProtected
	}

	result := s.db.WithContext(ctx).Delete(&ApiKey{}, "id = ?", id)
	if result.Error != nil {
		return fmt.Errorf("failed to delete API key: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrApiKeyNotFound
	}
	s.invalidateValidatedKeys(id)
	return nil
}

func (s *ApiKeyService) Stop() {
	s.validatedKeyCache.StopJanitor()
	s.rejectedKeyCache.StopJanitor()
}

func (s *ApiKeyService) ValidateApiKey(ctx context.Context, rawKey string) (*user.User, error) {
	localUser, _, err := s.ValidateApiKeyWithID(ctx, rawKey)
	return localUser, err
}

// ValidateApiKeyWithID is like ValidateApiKey but additionally returns the
// API key record so callers can resolve permissions according to its kind.
func (s *ApiKeyService) ValidateApiKeyWithID(ctx context.Context, rawKey string) (*user.User, *ApiKey, error) {
	apiKey, err := s.ValidateRawApiKey(ctx, rawKey)
	if err != nil {
		return nil, nil, err
	}

	if apiKey.UserID == nil {
		return nil, nil, ErrApiKeyInvalid
	}

	localUser, err := s.userService.GetUserByID(ctx, *apiKey.UserID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get user for API key: %w", err)
	}

	return localUser, apiKey, nil
}

func (s *ApiKeyService) GetEnvironmentByApiKey(ctx context.Context, rawKey string) (*string, error) {
	apiKey, err := s.ValidateRawApiKey(ctx, rawKey)
	if err != nil {
		return nil, err
	}
	return apiKey.EnvironmentID, nil
}

func (s *ApiKeyService) ValidateRawApiKey(ctx context.Context, rawKey string) (*ApiKey, error) {
	rawKey = strings.TrimSpace(rawKey)
	keyPrefix, err := parseAPIKeyPrefix(rawKey)
	if err != nil {
		return nil, err
	}

	cacheKey := s.hashRawAPIKey(rawKey)
	if cached, ok, _ := s.validatedKeyCache.Get(cacheKey); ok {
		if cached.ExpiresAt != nil && cached.ExpiresAt.Before(time.Now()) {
			s.validatedKeyCache.Delete(cacheKey)
			return nil, ErrApiKeyExpired
		}
		s.markApiKeyUsedDebounced(ctx, cached.ID)
		return &cached, nil
	}
	if _, rejected, _ := s.rejectedKeyCache.Get(cacheKey); rejected {
		return nil, ErrApiKeyInvalid
	}

	// Snapshot before the read: if a revocation lands between this read and the
	// publish below, the generation will have moved and the publish is dropped.
	gen := s.cacheGen.Load()
	var apiKeys []ApiKey
	if findAPIKeysErr := s.db.WithContext(ctx).Where("key_prefix = ?", keyPrefix).Find(&apiKeys).Error; findAPIKeysErr != nil {
		return nil, fmt.Errorf("failed to find API keys: %w", findAPIKeysErr)
	}

	for _, apiKey := range apiKeys {
		if validateErr := s.userService.ValidatePassword(ctx, apiKey.KeyHash, rawKey); validateErr == nil {
			if apiKey.ExpiresAt != nil && apiKey.ExpiresAt.Before(time.Now()) {
				return nil, ErrApiKeyExpired
			}

			s.storeValidatedKey(gen, cacheKey, apiKey)
			s.markApiKeyUsedDebounced(ctx, apiKey.ID)

			return &apiKey, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}

	s.rejectedKeyCache.Set(cacheKey, struct{}{})
	return nil, ErrApiKeyInvalid
}

// storeValidatedKey publishes a validated key into the cache unless an
// invalidation ran since the generation snapshot taken before the caller's DB
// read. The current request still proceeds — it validated against a row that
// existed at read time, indistinguishable from a request racing the revocation
// itself — but the result must not outlive the revocation in the cache.
func (s *ApiKeyService) storeValidatedKey(gen uint64, cacheKey string, apiKey ApiKey) {
	s.cacheFillMu.Lock()
	defer s.cacheFillMu.Unlock()
	if s.cacheGen.Load() == gen {
		s.validatedKeyCache.Set(cacheKey, apiKey)
	}
}
