package apikey

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.getarcane.app/kit/normalization"

	"emperror.dev/errors"

	"github.com/samber/hot"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/role"
	"github.com/getarcaneapp/arcane/backend/v2/internal/user"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/dbutil"
	"github.com/getarcaneapp/arcane/types/v2/apikey"
	"gorm.io/gorm"
)

const (
	ErrApiKeyNotFound  = errors.Sentinel("API key not found")
	ErrApiKeyExpired   = errors.Sentinel("API key has expired")
	ErrApiKeyInvalid   = errors.Sentinel("invalid API key")
	ErrApiKeyProtected = errors.Sentinel("API key is protected")
)

const (
	apiKeyPrefix              = "arc_"
	apiKeyLength              = 32
	apiKeyPrefixLen           = 8
	apiKeyLastUsedWriteWindow = 5 * time.Minute

	managedByAdminBootstrap = "admin_account_default_api_key"
	defaultAdminUsername    = "arcane"
	defaultAdminAPIKeyName  = "Static Admin API Key"
)

var defaultAdminAPIKeyDescription = func() *string {
	return new("Environment-managed static API key for the built-in admin account")
}()

type ApiKeyService struct {
	db           *database.DB
	userService  *user.UserService
	roleService  *role.RoleService
	argon2Params *user.Argon2Params
	// validatedKeyCache is a per-process cache of Argon2id-validated API keys,
	// keyed by HMAC-SHA256 of the raw key under cacheKeyPepper. Entries are only
	// written after a full hash validation, so a hit safely skips the Argon2id
	// derivation. Expiry is re-checked on every hit and entries are invalidated
	// on key update/delete.
	validatedKeyCache *hot.HotCache[string, ApiKey]
	// cacheKeyPepper is a random per-process HMAC key for deriving cache keys.
	// Generated keys carry 256 bits of entropy, but the static admin key is
	// operator-provided and may be weak; the pepper keeps its in-memory digest
	// useless for offline brute-force (e.g. from a memory dump). Argon2id
	// remains the sole persistent hash.
	cacheKeyPepper []byte
	// cacheGen is bumped by every invalidation; cache fills are guarded by
	// cacheFillMu and dropped when the generation moved after the fill's DB
	// read. Without this, a validation that read the key row just before a
	// revocation would republish the deleted key after eviction already ran,
	// letting it authenticate for a full cache TTL.
	cacheGen    atomic.Uint64
	cacheFillMu sync.Mutex
	// lastUsedMu guards lastUsedWrites, which debounces last_used_at writes so
	// each key opens at most one write transaction per apiKeyLastUsedWriteWindow.
	lastUsedMu     sync.Mutex
	lastUsedWrites map[string]time.Time
}

func NewApiKeyService(db *database.DB, userService *user.UserService) *ApiKeyService {
	pepper := make([]byte, 32)
	if _, err := rand.Read(pepper); err != nil {
		panic(errors.WrapIf(err, "failed to generate API key cache pepper"))
	}
	return &ApiKeyService{
		cacheKeyPepper: pepper,
		db:             db,
		userService:    userService,
		argon2Params:   user.DefaultArgon2Params(),
		validatedKeyCache: hot.NewHotCache[string, ApiKey](hot.LRU, 4096).
			WithTTL(15 * time.Second).
			WithJanitor().
			Build(),
		lastUsedWrites: make(map[string]time.Time),
	}
}

// WithRoleService wires the role.RoleService dependency. Separated from the
// constructor to break the bootstrap-ordering cycle between ApiKeyService and
// role.RoleService (role.RoleService.BackfillApiKeyPermissions needs ApiKeyService to
// exist when it runs, while permission-validated CreateApiKey needs the
// role.RoleService).
func (s *ApiKeyService) WithRoleService(roleService *role.RoleService) *ApiKeyService {
	s.roleService = roleService
	return s
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
		return errors.WrapIf(err, "failed to list bootstrap api keys for backfill")
	}
	if len(keys) == 0 {
		return nil
	}

	return dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		for _, key := range keys {
			var existing int64
			if err := tx.Model(&role.ApiKeyPermission{}).Where("api_key_id = ?", key.ID).Count(&existing).Error; err != nil {
				return errors.WrapIff(err, "failed to count permissions for api key %s", key.ID)
			}
			if existing > 0 {
				continue
			}
			perms, err := s.backfillPermsForKeyInternal(ctx, tx, key)
			if err != nil {
				return err
			}
			for _, p := range perms {
				if err := tx.Create(&role.ApiKeyPermission{
					ApiKeyID:      key.ID,
					Permission:    p,
					EnvironmentID: key.EnvironmentID,
				}).Error; err != nil {
					return errors.WrapIf(err, "failed to seed api key permission")
				}
			}
			slog.InfoContext(ctx, "Backfilled missing permissions for bootstrap api key", "api_key_id", key.ID, "perm_count", len(perms), "env_id", key.EnvironmentID)
		}
		return nil
	})
}

func (s *ApiKeyService) backfillPermsForKeyInternal(ctx context.Context, tx *gorm.DB, key ApiKey) ([]string, error) {
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
	var owner common.User
	if err := tx.WithContext(ctx).Where("id = ?", *key.UserID).First(&owner).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, errors.WrapIf(err, "failed to load api key owner")
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
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	return out, nil
}

func (s *ApiKeyService) generateApiKey() (string, error) {
	bytes := make([]byte, apiKeyLength)
	if _, err := rand.Read(bytes); err != nil {
		return "", errors.WrapIf(err, "failed to generate API key")
	}
	return apiKeyPrefix + hex.EncodeToString(bytes), nil
}

func (s *ApiKeyService) hashApiKey(key string) (string, error) {
	return s.userService.HashPassword(key)
}

func (s *ApiKeyService) validateApiKeyHash(hash, key string) error {
	return s.userService.ValidatePassword(hash, key)
}

func parseAPIKeyPrefixInternal(rawKey string) (string, error) {
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

func (s *ApiKeyService) markApiKeyUsedInternal(ctx context.Context, keyID string) error {
	now := time.Now()
	cutoff := now.Add(-apiKeyLastUsedWriteWindow)
	if err := s.db.WithContext(ctx).
		Model(&ApiKey{}).
		Where("id = ? AND (last_used_at IS NULL OR last_used_at < ?)", keyID, cutoff).
		Update("last_used_at", now).Error; err != nil {
		return errors.WrapIf(err, "failed to update API key last-used timestamp")
	}
	return nil
}

// markApiKeyUsedDebouncedInternal skips the DB write entirely when this process
// already persisted last_used_at for the key within the write window, so the
// auth hot path stays read-only between windows.
func (s *ApiKeyService) markApiKeyUsedDebouncedInternal(ctx context.Context, keyID string) {
	now := time.Now()
	s.lastUsedMu.Lock()
	if last, ok := s.lastUsedWrites[keyID]; ok && now.Sub(last) < apiKeyLastUsedWriteWindow {
		s.lastUsedMu.Unlock()
		return
	}
	s.lastUsedWrites[keyID] = now
	s.lastUsedMu.Unlock()

	if err := s.markApiKeyUsedInternal(ctx, keyID); err != nil {
		slog.WarnContext(ctx, "failed to persist API key usage", "api_key_id", keyID, "error", err)
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

// hashRawAPIKeyInternal derives the cache key for a raw API key. This is not
// credential storage — Argon2id in the DB is — so a fast keyed hash is
// appropriate for this hot path; the per-process pepper prevents offline
// brute-force of the digest should process memory leak.
func (s *ApiKeyService) hashRawAPIKeyInternal(rawKey string) string {
	mac := hmac.New(sha256.New, s.cacheKeyPepper)
	mac.Write([]byte(rawKey))
	return hex.EncodeToString(mac.Sum(nil))
}

// invalidateValidatedKeysInternal evicts cached validations for the given key
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
func (s *ApiKeyService) invalidateValidatedKeysInternal(ids ...string) {
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

func (s *ApiKeyService) CreateApiKey(ctx context.Context, userID string, callerPerms *authz.PermissionSet, req apikey.CreateApiKey) (*apikey.ApiKeyCreatedDto, error) {
	if err := normalization.Normalize(&req); err != nil {
		return nil, err
	}
	if err := validateGrantsAgainstPermissionSetInternal(callerPerms, req.Permissions); err != nil {
		return nil, err
	}
	if err := s.validateGrantsAgainstOwnerInternal(ctx, userID, req.Permissions); err != nil {
		return nil, err
	}
	rawKey, err := s.generateApiKey()
	if err != nil {
		return nil, err
	}

	created, err := s.createAPIKeyWithRawKey(ctx, &userID, rawKey, req, nil, nil, ApiKeyKindScoped)
	if err != nil {
		return nil, err
	}
	if s.roleService != nil {
		grants := toApiKeyPermissionRowsInternal(created.ID, req.Permissions)
		if err := s.roleService.SetApiKeyPermissions(ctx, created.ID, grants); err != nil {
			return nil, errors.WrapIf(err, "failed to persist api key permissions")
		}
		// Re-load the just-persisted grants into the response DTO so the
		// frontend doesn't see `"permissions": null` on a successful create.
		created.Permissions = s.loadKeyGrantsInternal(ctx, created.ID)
	}
	return created, nil
}

// ErrApiKeyPermissionEscalation is returned when a caller attempts to grant an
// API key permissions they themselves do not hold.
const ErrApiKeyPermissionEscalation = errors.Sentinel("cannot grant a permission you do not have")

// ErrApiKeyPersonalNoGrants is returned when a caller attempts to attach
// permission grants to a personal key, which has none of its own — it inherits
// the owner's role permissions at authentication time.
const ErrApiKeyPersonalNoGrants = errors.Sentinel("personal API keys inherit the owner's permissions and cannot carry grants")

// validateGrantsAgainstOwnerInternal caps grants at the key owner's role
// permissions, so no holder of apikeys:create/update — sudo included — can
// push a key above what its owner is allowed to do. Complements the caller-set
// check: the caller cap uses the credential's live context permissions, this
// one re-resolves the owner's roles from the DB. Skipped when role.RoleService is
// unavailable (bootstrap path only, where no human caller exists).
func (s *ApiKeyService) validateGrantsAgainstOwnerInternal(ctx context.Context, ownerID string, grants []apikey.PermissionGrant) error {
	if s.roleService == nil || len(grants) == 0 {
		return nil
	}
	user, err := s.userService.GetUserByID(ctx, ownerID)
	if err != nil {
		return errors.WrapIf(err, "load owner for permission validation")
	}
	ps, err := s.roleService.ResolvePermissions(ctx, user)
	if err != nil {
		return errors.WrapIf(err, "resolve owner permissions")
	}
	if err := validateGrantsAgainstPermissionSetInternal(ps, grants); err != nil {
		return errors.WrapIf(err, "owner's roles do not allow this grant")
	}
	return nil
}

// validateGrantsAgainstPermissionSetInternal refuses requests that would grant
// a key permissions the calling credential does not itself hold right now —
// session callers are capped by their role permissions, API-key callers by
// their own grants. Sudo callers always pass; a nil set denies everything.
func validateGrantsAgainstPermissionSetInternal(callerPerms *authz.PermissionSet, grants []apikey.PermissionGrant) error {
	for _, g := range grants {
		envID := ""
		if g.EnvironmentID != nil {
			envID = *g.EnvironmentID
		}
		if !callerPerms.Allows(g.Permission, envID) {
			return errors.WrapIff(ErrApiKeyPermissionEscalation, "%s (env=%q)", g.Permission, envID)
		}
	}
	return nil
}

func toApiKeyPermissionRowsInternal(apiKeyID string, grants []apikey.PermissionGrant) []role.ApiKeyPermission {
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
func (s *ApiKeyService) CreatePersonalApiKey(ctx context.Context, userID string, req apikey.CreateUserApiKey) (*apikey.ApiKeyCreatedDto, error) {
	if err := normalization.Normalize(&req); err != nil {
		return nil, err
	}
	rawKey, err := s.generateApiKey()
	if err != nil {
		return nil, err
	}

	created, err := s.createAPIKeyWithRawKey(ctx, &userID, rawKey, apikey.CreateApiKey{
		Name:        req.Name,
		Description: req.Description,
		ExpiresAt:   req.ExpiresAt,
	}, nil, nil, ApiKeyKindPersonal)
	if err != nil {
		return nil, err
	}
	created.Permissions = []apikey.PermissionGrant{}
	return created, nil
}

func (s *ApiKeyService) createAPIKeyWithRawKey(
	ctx context.Context,
	userID *string,
	rawKey string,
	req apikey.CreateApiKey,
	managedBy *string,
	environmentID *string,
	kind string,
) (*apikey.ApiKeyCreatedDto, error) {
	rawKey = strings.TrimSpace(rawKey)
	keyPrefix, err := parseAPIKeyPrefixInternal(rawKey)
	if err != nil {
		return nil, err
	}

	keyHash, err := s.hashApiKey(rawKey)
	if err != nil {
		return nil, errors.WrapIf(err, "failed to hash API key")
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

	if err := s.db.WithContext(ctx).Create(ak).Error; err != nil {
		return nil, errors.WrapIf(err, "failed to create API key")
	}

	return &apikey.ApiKeyCreatedDto{
		// A just-minted environment key is about to be linked as the
		// environment's pairing key, so present it as bootstrap already.
		ApiKey: toAPIKeyDTOInternal(ak, environmentID != nil),
		Key:    rawKey,
	}, nil
}

func isStaticAPIKeyInternal(ak ApiKey) bool {
	return ak.ManagedBy != nil && *ak.ManagedBy == managedByAdminBootstrap
}

// isEnvironmentReferencedApiKeyInternal reports whether an environment
// currently references keyID as its pairing key (environments.api_key_id).
// Referenced keys are the live environment bootstrap credentials: editing or
// deleting one would silently break the paired edge agent the next time it
// authenticates, so they are protected. Keys orphaned by regeneration are no
// longer referenced and may be edited or deleted normally. Cascade-delete
// still applies when the environment row itself is removed.
func (s *ApiKeyService) isEnvironmentReferencedApiKeyInternal(ctx context.Context, keyID string) (bool, error) {
	var count int64
	if err := s.db.WithContext(ctx).Table("environments").
		Where("api_key_id = ?", keyID).Count(&count).Error; err != nil {
		return false, errors.WrapIf(err, "failed to check environment references for API key")
	}
	return count > 0, nil
}

// environmentReferencedApiKeyIDsInternal returns the set of API key ids
// currently referenced by an environment, for tagging whole listings with a
// single query instead of one per row.
func (s *ApiKeyService) environmentReferencedApiKeyIDsInternal(ctx context.Context) (map[string]struct{}, error) {
	var ids []string
	if err := s.db.WithContext(ctx).Table("environments").
		Where("api_key_id IS NOT NULL").Pluck("api_key_id", &ids).Error; err != nil {
		return nil, errors.WrapIf(err, "failed to list environment-referenced API key ids")
	}
	set := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	return set, nil
}

func toAPIKeyDTOInternal(ak *ApiKey, isBootstrap bool) apikey.ApiKey {
	return apikey.ApiKey{
		ID:          ak.ID,
		Name:        ak.Name,
		Description: ak.Description,
		KeyPrefix:   ak.KeyPrefix,
		UserID:      ak.UserID,
		Kind:        ak.Kind,
		IsStatic:    isStaticAPIKeyInternal(*ak),
		IsBootstrap: isBootstrap,
		ExpiresAt:   ak.ExpiresAt,
		LastUsedAt:  ak.LastUsedAt,
		CreatedAt:   ak.CreatedAt,
		UpdatedAt:   ak.UpdatedAt,
	}
}

// toAPIKeyDTOWithPermissionsInternal is like toAPIKeyDTOInternal but
// additionally attaches the key's persisted permission grants.
func toAPIKeyDTOWithPermissionsInternal(ak *ApiKey, isBootstrap bool) apikey.ApiKey {
	dto := toAPIKeyDTOInternal(ak, isBootstrap)
	dto.Permissions = make([]apikey.PermissionGrant, len(ak.PermissionGrants))
	for i, grant := range ak.PermissionGrants {
		dto.Permissions[i] = apikey.PermissionGrant{
			Permission:    grant.Permission,
			EnvironmentID: grant.EnvironmentID,
		}
	}
	return dto
}

func (s *ApiKeyService) CreateDefaultAdminAPIKey(ctx context.Context, userID, rawKey string) (*apikey.ApiKeyCreatedDto, error) {
	return s.createAPIKeyWithRawKey(ctx, &userID, rawKey, apikey.CreateApiKey{
		Name:        defaultAdminAPIKeyName,
		Description: defaultAdminAPIKeyDescription,
	}, new(managedByAdminBootstrap), nil, ApiKeyKindScoped)
}

func (s *ApiKeyService) getDefaultAdminUser(ctx context.Context) (*common.User, error) {
	adminUser, err := s.userService.GetUserByUsername(ctx, defaultAdminUsername)
	if err != nil {
		if errors.Is(err, common.ErrUserNotFound) {
			slog.WarnContext(ctx, "Default admin user not found, skipping default admin API key reconciliation", "username", defaultAdminUsername)
			return nil, nil
		}
		return nil, errors.WrapIf(err, "failed to load default admin user")
	}

	// The username is mutable and not proof of provenance — never mint the
	// managed full-permission key onto an account that isn't a global admin.
	if s.roleService != nil {
		perms, err := s.roleService.ResolvePermissions(ctx, adminUser)
		if err != nil {
			return nil, errors.WrapIf(err, "failed to resolve default admin permissions")
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
		return nil, errors.WrapIf(err, "failed to load managed API keys")
	}

	return managedKeys, nil
}

func (s *ApiKeyService) deleteManagedAPIKeysByIDs(tx *gorm.DB, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	if err := tx.Delete(&ApiKey{}, "id IN ?", ids).Error; err != nil {
		return errors.WrapIf(err, "failed to delete managed API keys")
	}
	return nil
}

func (s *ApiKeyService) findMatchingManagedAPIKey(rawKey string, managedKeys []ApiKey) int {
	for i, managedKey := range managedKeys {
		if err := s.validateApiKeyHash(managedKey.KeyHash, rawKey); err == nil {
			return i
		}
	}
	return -1
}

func managedAPIKeyDeleteIDsInternal(managedKeys []ApiKey, keepIndex int) []string {
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
		return errors.WrapIf(err, "failed to update managed API key metadata")
	}
	return nil
}

func (s *ApiKeyService) createManagedDefaultAdminAPIKey(tx *gorm.DB, userID, rawKey string) error {
	keyPrefix, err := parseAPIKeyPrefixInternal(rawKey)
	if err != nil {
		return err
	}

	keyHash, err := s.hashApiKey(rawKey)
	if err != nil {
		return errors.WrapIf(err, "failed to hash API key")
	}

	ak := &ApiKey{
		Name:        defaultAdminAPIKeyName,
		Description: defaultAdminAPIKeyDescription,
		KeyHash:     keyHash,
		KeyPrefix:   keyPrefix,
		ManagedBy:   new(managedByAdminBootstrap),
		UserID:      &userID,
	}

	if err := tx.Create(ak).Error; err != nil {
		return errors.WrapIf(err, "failed to create managed API key")
	}
	return nil
}

// reconcileManagedAPIKeys applies the desired managed-key state inside tx and
// returns the ids whose cached validations the caller must invalidate AFTER
// the transaction commits. Invalidating pre-commit is ineffective: a concurrent
// validation can still read the old committed row, snapshot the already-bumped
// generation, and republish the entry so it survives the commit.
func (s *ApiKeyService) reconcileManagedAPIKeys(tx *gorm.DB, userID string, rawKey string) ([]string, error) {
	managedKeys, err := s.listManagedAPIKeys(tx, userID)
	if err != nil {
		return nil, err
	}

	if rawKey == "" {
		deleteIDs := managedAPIKeyDeleteIDsInternal(managedKeys, -1)
		return deleteIDs, s.deleteManagedAPIKeysByIDs(tx, deleteIDs)
	}

	matchingIndex := s.findMatchingManagedAPIKey(rawKey, managedKeys)
	if matchingIndex >= 0 {
		if err := s.updateMatchingManagedAPIKey(tx, managedKeys[matchingIndex].ID); err != nil {
			return nil, err
		}
		deleteIDs := managedAPIKeyDeleteIDsInternal(managedKeys, matchingIndex)
		if err := s.deleteManagedAPIKeysByIDs(tx, deleteIDs); err != nil {
			return nil, err
		}
		// The kept key's metadata changed too; its cached copy is stale.
		return append(deleteIDs, managedKeys[matchingIndex].ID), nil
	}

	deleteIDs := managedAPIKeyDeleteIDsInternal(managedKeys, -1)
	if err := s.deleteManagedAPIKeysByIDs(tx, deleteIDs); err != nil {
		return nil, err
	}

	return deleteIDs, s.createManagedDefaultAdminAPIKey(tx, userID, rawKey)
}

func (s *ApiKeyService) ReconcileDefaultAdminAPIKey(ctx context.Context, rawKey string) error {
	rawKey = strings.TrimSpace(rawKey)

	adminUser, err := s.getDefaultAdminUser(ctx)
	if err != nil || adminUser == nil {
		return err
	}

	var affectedIDs []string
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		affectedIDs, err = s.reconcileManagedAPIKeys(tx, adminUser.ID, rawKey)
		return err
	}); err != nil {
		// Rolled back: the DB is unchanged, so cached entries are still valid.
		return err
	}
	s.invalidateValidatedKeysInternal(affectedIDs...)
	return nil
}

func (s *ApiKeyService) CreateEnvironmentApiKey(ctx context.Context, environmentID string) (*apikey.ApiKeyCreatedDto, error) {
	rawKey, err := s.generateApiKey()
	if err != nil {
		return nil, err
	}

	envIDShort := environmentID
	if len(environmentID) > 8 {
		envIDShort = environmentID[:8]
	}
	name := "Environment Bootstrap Key - " + envIDShort
	created, err := s.createAPIKeyWithRawKey(ctx, nil, rawKey, apikey.CreateApiKey{
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
		if err := s.roleService.SetApiKeyPermissions(ctx, created.ID, grants); err != nil {
			return nil, errors.WrapIf(err, "failed to persist environment bootstrap key permissions")
		}
		// Re-load grants into the response DTO so callers see the seeded
		// permissions immediately, not null.
		created.Permissions = s.loadKeyGrantsInternal(ctx, created.ID)
	}
	return created, nil
}

func (s *ApiKeyService) GetApiKey(ctx context.Context, id string) (*apikey.ApiKey, error) {
	var ak ApiKey
	query := s.db.WithContext(ctx)
	if s.roleService != nil {
		query = query.Preload("PermissionGrants")
	}
	if err := query.Where("id = ?", id).First(&ak).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrApiKeyNotFound
		}
		return nil, errors.WrapIf(err, "failed to get API key")
	}
	isBootstrap, err := s.isEnvironmentReferencedApiKeyInternal(ctx, ak.ID)
	if err != nil {
		return nil, err
	}
	return new(toAPIKeyDTOWithPermissionsInternal(&ak, isBootstrap)), nil
}

func (s *ApiKeyService) ListApiKeys(ctx context.Context, params pagination.QueryParams) ([]apikey.ApiKey, pagination.Response, error) {
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
		return nil, pagination.Response{}, errors.WrapIf(err, "failed to paginate API keys")
	}

	referenced, err := s.environmentReferencedApiKeyIDsInternal(ctx)
	if err != nil {
		return nil, pagination.Response{}, err
	}

	result := make([]apikey.ApiKey, len(apiKeys))
	for i := range apiKeys {
		// Include per-key permission grants so the edit form preloads with the
		// key's actual current grants. Without this, the form starts empty and
		// "Save" would wipe whatever grants the DB has.
		_, isBootstrap := referenced[apiKeys[i].ID]
		result[i] = toAPIKeyDTOWithPermissionsInternal(&apiKeys[i], isBootstrap)
	}

	return result, paginationResp, nil
}

// ListApiKeysByUser returns every non-static, non-bootstrap API key owned by
// userID. Used by the self-service personal-keys flow. Keys referenced by an
// environment are hidden even when they carry an owner (legacy bootstrap rows
// created before user_id became nullable) — self-service must never touch a
// live pairing credential.
func (s *ApiKeyService) ListApiKeysByUser(ctx context.Context, userID string) ([]apikey.ApiKey, error) {
	var apiKeys []ApiKey
	query := s.db.WithContext(ctx)
	if s.roleService != nil {
		query = query.Preload("PermissionGrants")
	}
	if err := query.
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Find(&apiKeys).Error; err != nil {
		return nil, errors.WrapIf(err, "failed to list user api keys")
	}

	referenced, err := s.environmentReferencedApiKeyIDsInternal(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]apikey.ApiKey, 0, len(apiKeys))
	for i := range apiKeys {
		if _, isBootstrap := referenced[apiKeys[i].ID]; isBootstrap || isStaticAPIKeyInternal(apiKeys[i]) {
			continue
		}
		result = append(result, toAPIKeyDTOWithPermissionsInternal(&apiKeys[i], false))
	}
	return result, nil
}

func (s *ApiKeyService) UpdateApiKey(ctx context.Context, callerPerms *authz.PermissionSet, id string, req apikey.UpdateApiKey) (*apikey.ApiKey, error) {
	if err := normalization.Normalize(&req); err != nil {
		return nil, err
	}
	var ak ApiKey
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&ak).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrApiKeyNotFound
		}
		return nil, errors.WrapIf(err, "failed to get API key")
	}
	if isStaticAPIKeyInternal(ak) {
		return nil, ErrApiKeyProtected
	}
	if referenced, err := s.isEnvironmentReferencedApiKeyInternal(ctx, ak.ID); err != nil {
		return nil, err
	} else if referenced {
		return nil, ErrApiKeyProtected
	}

	if req.Permissions != nil {
		if ak.Kind == ApiKeyKindPersonal {
			return nil, ErrApiKeyPersonalNoGrants
		}
		// Grants are capped twice: by the calling credential's effective
		// permissions (a holder of apikeys:update cannot grant anything the
		// credential itself cannot do right now) AND by the key owner's role
		// permissions (no caller can push another user's key above what that
		// user's own roles allow).
		if err := validateGrantsAgainstPermissionSetInternal(callerPerms, req.Permissions); err != nil {
			return nil, err
		}
		if ak.UserID == nil {
			// No owner to cap against. Ownerless rows are (possibly stale)
			// environment bootstrap keys — name/description edits are harmless,
			// but refuse grant edits rather than fall back to a weaker
			// caller-only check.
			return nil, ErrApiKeyProtected
		}
		if err := s.validateGrantsAgainstOwnerInternal(ctx, *ak.UserID, req.Permissions); err != nil {
			return nil, err
		}
	}

	if req.Name != nil {
		ak.Name = *req.Name
	}
	if req.Description != nil {
		ak.Description = req.Description
	}
	if req.ExpiresAt != nil {
		ak.ExpiresAt = req.ExpiresAt
	}

	permissionsUpdated := false
	if err := dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		if err := tx.Save(&ak).Error; err != nil {
			return errors.WrapIf(err, "failed to update API key")
		}
		if req.Permissions != nil && s.roleService != nil {
			if err := s.roleService.SetApiKeyPermissionsInDB(ctx, tx, ak.ID, toApiKeyPermissionRowsInternal(ak.ID, req.Permissions)); err != nil {
				return errors.WrapIf(err, "failed to update api key permissions")
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
	s.invalidateValidatedKeysInternal(ak.ID)

	return &apikey.ApiKey{
		ID:          ak.ID,
		Name:        ak.Name,
		Description: ak.Description,
		KeyPrefix:   ak.KeyPrefix,
		UserID:      ak.UserID,
		Kind:        ak.Kind,
		IsStatic:    isStaticAPIKeyInternal(ak),
		IsBootstrap: false, // an environment-referenced key can't reach this return
		ExpiresAt:   ak.ExpiresAt,
		LastUsedAt:  ak.LastUsedAt,
		CreatedAt:   ak.CreatedAt,
		UpdatedAt:   ak.UpdatedAt,
		Permissions: s.loadKeyGrantsInternal(ctx, ak.ID),
	}, nil
}

// loadKeyGrantsInternal returns the persisted permission grants for an API key.
// Always returns a non-nil slice (possibly empty) so the JSON-marshalled DTO
// renders as `"permissions": []` rather than `"permissions": null`, which the
// frontend treats differently (null hides the picker, [] shows it empty).
// DB errors are logged and yield an empty slice — the caller never sees nil.
func (s *ApiKeyService) loadKeyGrantsInternal(ctx context.Context, apiKeyID string) []apikey.PermissionGrant {
	empty := []apikey.PermissionGrant{}
	if s.roleService == nil {
		return empty
	}
	var rows []role.ApiKeyPermission
	if err := s.db.WithContext(ctx).Where("api_key_id = ?", apiKeyID).Find(&rows).Error; err != nil {
		slog.WarnContext(ctx, "failed to load api key permission grants", "api_key_id", apiKeyID, "error", err)
		return empty
	}
	out := make([]apikey.PermissionGrant, len(rows))
	for i, r := range rows {
		out[i] = apikey.PermissionGrant{Permission: r.Permission, EnvironmentID: r.EnvironmentID}
	}
	return out
}

func (s *ApiKeyService) DeleteApiKey(ctx context.Context, id string) error {
	var apiKey ApiKey
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&apiKey).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrApiKeyNotFound
		}
		return errors.WrapIf(err, "failed to load API key")
	}
	if isStaticAPIKeyInternal(apiKey) {
		return ErrApiKeyProtected
	}
	if referenced, err := s.isEnvironmentReferencedApiKeyInternal(ctx, apiKey.ID); err != nil {
		return err
	} else if referenced {
		return ErrApiKeyProtected
	}

	result := s.db.WithContext(ctx).Delete(&ApiKey{}, "id = ?", id)
	if result.Error != nil {
		return errors.WrapIf(result.Error, "failed to delete API key")
	}
	if result.RowsAffected == 0 {
		return ErrApiKeyNotFound
	}
	s.invalidateValidatedKeysInternal(id)
	return nil
}

func (s *ApiKeyService) ValidateApiKey(ctx context.Context, rawKey string) (*common.User, error) {
	user, _, err := s.ValidateApiKeyWithID(ctx, rawKey)
	return user, err
}

// ValidateApiKeyWithID is like ValidateApiKey but additionally returns the
// API key record so callers can resolve permissions according to its kind.
func (s *ApiKeyService) ValidateApiKeyWithID(ctx context.Context, rawKey string) (*common.User, *ApiKey, error) {
	apiKey, err := s.validateRawAPIKeyInternal(ctx, rawKey)
	if err != nil {
		return nil, nil, err
	}

	if apiKey.UserID == nil {
		return nil, nil, ErrApiKeyInvalid
	}

	user, err := s.userService.GetUserByID(ctx, *apiKey.UserID)
	if err != nil {
		return nil, nil, errors.WrapIf(err, "failed to get user for API key")
	}

	return user, apiKey, nil
}

func (s *ApiKeyService) GetEnvironmentByApiKey(ctx context.Context, rawKey string) (*string, error) {
	apiKey, err := s.validateRawAPIKeyInternal(ctx, rawKey)
	if err != nil {
		return nil, err
	}
	return apiKey.EnvironmentID, nil
}

// validateRawAPIKeyInternal resolves a raw key to its validated record. A cache
// hit skips the per-candidate Argon2id derivation; expiry is always re-checked
// so a key expiring mid-TTL is rejected immediately.
func (s *ApiKeyService) validateRawAPIKeyInternal(ctx context.Context, rawKey string) (*ApiKey, error) {
	rawKey = strings.TrimSpace(rawKey)
	keyPrefix, err := parseAPIKeyPrefixInternal(rawKey)
	if err != nil {
		return nil, err
	}

	cacheKey := s.hashRawAPIKeyInternal(rawKey)
	if cached, ok, _ := s.validatedKeyCache.Get(cacheKey); ok {
		if cached.ExpiresAt != nil && cached.ExpiresAt.Before(time.Now()) {
			s.validatedKeyCache.Delete(cacheKey)
			return nil, ErrApiKeyExpired
		}
		s.markApiKeyUsedDebouncedInternal(ctx, cached.ID)
		return &cached, nil
	}

	// Snapshot before the read: if a revocation lands between this read and the
	// publish below, the generation will have moved and the publish is dropped.
	gen := s.cacheGen.Load()
	var apiKeys []ApiKey
	if err := s.db.WithContext(ctx).Where("key_prefix = ?", keyPrefix).Find(&apiKeys).Error; err != nil {
		return nil, errors.WrapIf(err, "failed to find API keys")
	}

	for _, apiKey := range apiKeys {
		if err := s.validateApiKeyHash(apiKey.KeyHash, rawKey); err == nil {
			if apiKey.ExpiresAt != nil && apiKey.ExpiresAt.Before(time.Now()) {
				return nil, ErrApiKeyExpired
			}

			s.storeValidatedKeyInternal(gen, cacheKey, apiKey)
			s.markApiKeyUsedDebouncedInternal(ctx, apiKey.ID)

			return &apiKey, nil
		}
	}

	return nil, ErrApiKeyInvalid
}

// storeValidatedKeyInternal publishes a validated key into the cache unless an
// invalidation ran since the generation snapshot taken before the caller's DB
// read. The current request still proceeds — it validated against a row that
// existed at read time, indistinguishable from a request racing the revocation
// itself — but the result must not outlive the revocation in the cache.
func (s *ApiKeyService) storeValidatedKeyInternal(gen uint64, cacheKey string, apiKey ApiKey) {
	s.cacheFillMu.Lock()
	defer s.cacheFillMu.Unlock()
	if s.cacheGen.Load() == gen {
		s.validatedKeyCache.Set(cacheKey, apiKey)
	}
}
