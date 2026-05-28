package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/pkg/utils/dbutil"
	"github.com/getarcaneapp/arcane/types/apikey"
	"gorm.io/gorm"
)

var (
	ErrApiKeyNotFound  = errors.New("API key not found")
	ErrApiKeyExpired   = errors.New("API key has expired")
	ErrApiKeyInvalid   = errors.New("invalid API key")
	ErrApiKeyProtected = errors.New("API key is protected")
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
	userService  *UserService
	roleService  *RoleService
	argon2Params *Argon2Params
}

func NewApiKeyService(db *database.DB, userService *UserService) *ApiKeyService {
	return &ApiKeyService{
		db:           db,
		userService:  userService,
		argon2Params: DefaultArgon2Params(),
	}
}

// WithRoleService wires the RoleService dependency. Separated from the
// constructor to break the bootstrap-ordering cycle between ApiKeyService and
// RoleService (RoleService.BackfillApiKeyPermissions needs ApiKeyService to
// exist when it runs, while permission-validated CreateApiKey needs the
// RoleService).
func (s *ApiKeyService) WithRoleService(roleService *RoleService) *ApiKeyService {
	s.roleService = roleService
	return s
}

func (s *ApiKeyService) generateApiKey() (string, error) {
	bytes := make([]byte, apiKeyLength)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate API key: %w", err)
	}
	return apiKeyPrefix + hex.EncodeToString(bytes), nil
}

func (s *ApiKeyService) hashApiKey(key string) (string, error) {
	return s.userService.HashPassword(key)
}

func (s *ApiKeyService) validateApiKeyHash(hash, key string) error {
	return s.userService.ValidatePassword(hash, key)
}

func normalizeAPIKeyInputInternal(rawKey string) string {
	return strings.TrimSpace(rawKey)
}

func parseAPIKeyPrefixInternal(rawKey string) (string, error) {
	rawKey = normalizeAPIKeyInputInternal(rawKey)
	if !strings.HasPrefix(rawKey, apiKeyPrefix) {
		return "", ErrApiKeyInvalid
	}

	prefixLen := len(apiKeyPrefix) + apiKeyPrefixLen
	if len(rawKey) < prefixLen {
		return "", ErrApiKeyInvalid
	}

	return rawKey[:prefixLen], nil
}

func (s *ApiKeyService) markApiKeyUsedAsync(ctx context.Context, keyID string) {
	go func(keyID string) {
		bgCtx := context.WithoutCancel(ctx)
		now := time.Now()
		cutoff := now.Add(-apiKeyLastUsedWriteWindow)
		s.db.WithContext(bgCtx).
			Model(&models.ApiKey{}).
			Where("id = ? AND (last_used_at IS NULL OR last_used_at < ?)", keyID, cutoff).
			Update("last_used_at", now)
	}(keyID)
}

func (s *ApiKeyService) CreateApiKey(ctx context.Context, userID string, req apikey.CreateApiKey) (*apikey.ApiKeyCreatedDto, error) {
	if err := s.validateGrantsAgainstUserInternal(ctx, userID, req.Permissions); err != nil {
		return nil, err
	}
	rawKey, err := s.generateApiKey()
	if err != nil {
		return nil, err
	}

	created, err := s.createAPIKeyWithRawKey(ctx, &userID, rawKey, req, nil, nil)
	if err != nil {
		return nil, err
	}
	if s.roleService != nil {
		grants := toApiKeyPermissionRowsInternal(created.ID, req.Permissions)
		if err := s.roleService.SetApiKeyPermissions(ctx, created.ID, grants); err != nil {
			return nil, fmt.Errorf("failed to persist api key permissions: %w", err)
		}
		// Re-load the just-persisted grants into the response DTO so the
		// frontend doesn't see `"permissions": null` on a successful create.
		created.Permissions = s.loadKeyGrantsInternal(ctx, created.ID)
	}
	return created, nil
}

// ErrApiKeyPermissionEscalation is returned when a caller attempts to grant an
// API key permissions they themselves do not hold.
var ErrApiKeyPermissionEscalation = errors.New("cannot grant a permission you do not have")

// validateGrantsAgainstUserInternal refuses requests that would grant the new key
// permissions that the requesting user doesn't hold. Sudo callers bypass this
// (they always pass). If RoleService is unavailable validation is skipped (used
// only in the boot-bootstrap path where no human caller exists).
func (s *ApiKeyService) validateGrantsAgainstUserInternal(ctx context.Context, userID string, grants []apikey.PermissionGrant) error {
	if s.roleService == nil || len(grants) == 0 {
		return nil
	}
	user, err := s.userService.GetUserByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("load user for permission validation: %w", err)
	}
	ps, err := s.roleService.ResolvePermissions(ctx, user)
	if err != nil {
		return fmt.Errorf("resolve user permissions: %w", err)
	}
	for _, g := range grants {
		envID := ""
		if g.EnvironmentID != nil {
			envID = *g.EnvironmentID
		}
		if !ps.Allows(g.Permission, envID) {
			return fmt.Errorf("%w: %s (env=%q)", ErrApiKeyPermissionEscalation, g.Permission, envID)
		}
	}
	return nil
}

func toApiKeyPermissionRowsInternal(apiKeyID string, grants []apikey.PermissionGrant) []models.ApiKeyPermission {
	out := make([]models.ApiKeyPermission, len(grants))
	for i, g := range grants {
		out[i] = models.ApiKeyPermission{
			ApiKeyID:      apiKeyID,
			Permission:    g.Permission,
			EnvironmentID: g.EnvironmentID,
		}
	}
	return out
}

func (s *ApiKeyService) createAPIKeyWithRawKey(
	ctx context.Context,
	userID *string,
	rawKey string,
	req apikey.CreateApiKey,
	managedBy *string,
	environmentID *string,
) (*apikey.ApiKeyCreatedDto, error) {
	rawKey = normalizeAPIKeyInputInternal(rawKey)
	keyPrefix, err := parseAPIKeyPrefixInternal(rawKey)
	if err != nil {
		return nil, err
	}

	keyHash, err := s.hashApiKey(rawKey)
	if err != nil {
		return nil, fmt.Errorf("failed to hash API key: %w", err)
	}

	ak := &models.ApiKey{
		Name:          req.Name,
		Description:   req.Description,
		KeyHash:       keyHash,
		KeyPrefix:     keyPrefix,
		ManagedBy:     managedBy,
		UserID:        userID,
		EnvironmentID: environmentID,
		ExpiresAt:     req.ExpiresAt,
	}

	if err := s.db.WithContext(ctx).Create(ak).Error; err != nil {
		return nil, fmt.Errorf("failed to create API key: %w", err)
	}

	return &apikey.ApiKeyCreatedDto{
		ApiKey: toAPIKeyDTOInternal(ak),
		Key:    rawKey,
	}, nil
}

func isStaticAPIKeyInternal(ak models.ApiKey) bool {
	return ak.ManagedBy != nil && *ak.ManagedBy == managedByAdminBootstrap
}

// isEnvironmentBootstrapKeyInternal identifies the auto-generated key minted by
// CreateEnvironmentApiKey for environment pairing. Those keys have no owner
// (UserID == nil) and are scoped to a single environment. They carry full
// env-scoped permissions and must never be hand-edited or deleted via the API
// — manually clearing the grants would silently break the paired edge agent
// the next time it tries to authenticate. Cascade-delete still applies when
// the environment row itself is removed.
func isEnvironmentBootstrapKeyInternal(ak models.ApiKey) bool {
	return ak.UserID == nil && ak.EnvironmentID != nil
}

func toAPIKeyDTOInternal(ak *models.ApiKey) apikey.ApiKey {
	return apikey.ApiKey{
		ID:          ak.ID,
		Name:        ak.Name,
		Description: ak.Description,
		KeyPrefix:   ak.KeyPrefix,
		UserID:      ak.UserID,
		IsStatic:    isStaticAPIKeyInternal(*ak),
		IsBootstrap: isEnvironmentBootstrapKeyInternal(*ak),
		ExpiresAt:   ak.ExpiresAt,
		LastUsedAt:  ak.LastUsedAt,
		CreatedAt:   ak.CreatedAt,
		UpdatedAt:   ak.UpdatedAt,
	}
}

// toAPIKeyDTOWithPermissionsInternal is like toAPIKeyDTOInternal but
// additionally attaches the key's persisted permission grants.
func (s *ApiKeyService) toAPIKeyDTOWithPermissionsInternal(ctx context.Context, ak *models.ApiKey) apikey.ApiKey {
	dto := toAPIKeyDTOInternal(ak)
	dto.Permissions = s.loadKeyGrantsInternal(ctx, ak.ID)
	return dto
}

func (s *ApiKeyService) CreateDefaultAdminAPIKey(ctx context.Context, userID, rawKey string) (*apikey.ApiKeyCreatedDto, error) {
	return s.createAPIKeyWithRawKey(ctx, &userID, rawKey, apikey.CreateApiKey{
		Name:        defaultAdminAPIKeyName,
		Description: defaultAdminAPIKeyDescription,
	}, new(managedByAdminBootstrap), nil)
}

func (s *ApiKeyService) getDefaultAdminUser(ctx context.Context) (*models.User, error) {
	adminUser, err := s.userService.GetUserByUsername(ctx, defaultAdminUsername)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			slog.WarnContext(ctx, "Default admin user not found, skipping default admin API key reconciliation", "username", defaultAdminUsername)
			return nil, nil
		}
		return nil, fmt.Errorf("failed to load default admin user: %w", err)
	}

	return adminUser, nil
}

func (s *ApiKeyService) listManagedAPIKeys(tx *gorm.DB, userID string) ([]models.ApiKey, error) {
	var managedKeys []models.ApiKey
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
	if err := tx.Delete(&models.ApiKey{}, "id IN ?", ids).Error; err != nil {
		return fmt.Errorf("failed to delete managed API keys: %w", err)
	}
	return nil
}

func (s *ApiKeyService) findMatchingManagedAPIKey(rawKey string, managedKeys []models.ApiKey) int {
	for i, managedKey := range managedKeys {
		if err := s.validateApiKeyHash(managedKey.KeyHash, rawKey); err == nil {
			return i
		}
	}
	return -1
}

func managedAPIKeyDeleteIDsInternal(managedKeys []models.ApiKey, keepIndex int) []string {
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
	if err := tx.Model(&models.ApiKey{}).
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

func (s *ApiKeyService) createManagedDefaultAdminAPIKey(tx *gorm.DB, userID, rawKey string) error {
	keyPrefix, err := parseAPIKeyPrefixInternal(rawKey)
	if err != nil {
		return err
	}

	keyHash, err := s.hashApiKey(rawKey)
	if err != nil {
		return fmt.Errorf("failed to hash API key: %w", err)
	}

	ak := &models.ApiKey{
		Name:        defaultAdminAPIKeyName,
		Description: defaultAdminAPIKeyDescription,
		KeyHash:     keyHash,
		KeyPrefix:   keyPrefix,
		ManagedBy:   new(managedByAdminBootstrap),
		UserID:      &userID,
	}

	if err := tx.Create(ak).Error; err != nil {
		return fmt.Errorf("failed to create managed API key: %w", err)
	}
	return nil
}

func (s *ApiKeyService) reconcileManagedAPIKeys(tx *gorm.DB, userID string, rawKey string) error {
	managedKeys, err := s.listManagedAPIKeys(tx, userID)
	if err != nil {
		return err
	}

	if rawKey == "" {
		return s.deleteManagedAPIKeysByIDs(tx, managedAPIKeyDeleteIDsInternal(managedKeys, -1))
	}

	matchingIndex := s.findMatchingManagedAPIKey(rawKey, managedKeys)
	if matchingIndex >= 0 {
		if err := s.updateMatchingManagedAPIKey(tx, managedKeys[matchingIndex].ID); err != nil {
			return err
		}
		return s.deleteManagedAPIKeysByIDs(tx, managedAPIKeyDeleteIDsInternal(managedKeys, matchingIndex))
	}

	if err := s.deleteManagedAPIKeysByIDs(tx, managedAPIKeyDeleteIDsInternal(managedKeys, -1)); err != nil {
		return err
	}

	return s.createManagedDefaultAdminAPIKey(tx, userID, rawKey)
}

func (s *ApiKeyService) ReconcileDefaultAdminAPIKey(ctx context.Context, rawKey string) error {
	rawKey = normalizeAPIKeyInputInternal(rawKey)

	adminUser, err := s.getDefaultAdminUser(ctx)
	if err != nil || adminUser == nil {
		return err
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return s.reconcileManagedAPIKeys(tx, adminUser.ID, rawKey)
	})
}

func (s *ApiKeyService) CreateEnvironmentApiKey(ctx context.Context, environmentID string, userID string) (*apikey.ApiKeyCreatedDto, error) {
	rawKey, err := s.generateApiKey()
	if err != nil {
		return nil, err
	}

	envIDShort := environmentID
	if len(environmentID) > 8 {
		envIDShort = environmentID[:8]
	}
	name := fmt.Sprintf("Environment Bootstrap Key - %s", envIDShort)
	created, err := s.createAPIKeyWithRawKey(ctx, nil, rawKey, apikey.CreateApiKey{
		Name:        name,
		Description: new("Auto-generated key for environment pairing"),
	}, nil, &environmentID)
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
		grants := make([]models.ApiKeyPermission, len(all))
		for i, p := range all {
			grants[i] = models.ApiKeyPermission{
				ApiKeyID:      created.ID,
				Permission:    p,
				EnvironmentID: &environmentID,
			}
		}
		if err := s.roleService.SetApiKeyPermissions(ctx, created.ID, grants); err != nil {
			return nil, fmt.Errorf("failed to persist environment bootstrap key permissions: %w", err)
		}
		// Re-load grants into the response DTO so callers see the seeded
		// permissions immediately, not null.
		created.Permissions = s.loadKeyGrantsInternal(ctx, created.ID)
	}
	return created, nil
}

func (s *ApiKeyService) GetApiKey(ctx context.Context, id string) (*apikey.ApiKey, error) {
	var ak models.ApiKey
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&ak).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrApiKeyNotFound
		}
		return nil, fmt.Errorf("failed to get API key: %w", err)
	}
	dto := s.toAPIKeyDTOWithPermissionsInternal(ctx, &ak)
	return &dto, nil
}

func (s *ApiKeyService) ListApiKeys(ctx context.Context, params pagination.QueryParams) ([]apikey.ApiKey, pagination.Response, error) {
	var apiKeys []models.ApiKey
	query := s.db.WithContext(ctx).Model(&models.ApiKey{})

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

	result := make([]apikey.ApiKey, len(apiKeys))
	for i := range apiKeys {
		// Include per-key permission grants so the edit form preloads with the
		// key's actual current grants. Without this, the form starts empty and
		// "Save" would wipe whatever grants the DB has.
		result[i] = s.toAPIKeyDTOWithPermissionsInternal(ctx, &apiKeys[i])
	}

	return result, paginationResp, nil
}

// ListApiKeysByUser returns every non-static, non-bootstrap API key owned by
// userID. Used by the self-service personal-keys flow.
func (s *ApiKeyService) ListApiKeysByUser(ctx context.Context, userID string) ([]apikey.ApiKey, error) {
	var apiKeys []models.ApiKey
	if err := s.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Find(&apiKeys).Error; err != nil {
		return nil, fmt.Errorf("failed to list user api keys: %w", err)
	}

	result := make([]apikey.ApiKey, 0, len(apiKeys))
	for i := range apiKeys {
		if isStaticAPIKeyInternal(apiKeys[i]) || isEnvironmentBootstrapKeyInternal(apiKeys[i]) {
			continue
		}
		result = append(result, s.toAPIKeyDTOWithPermissionsInternal(ctx, &apiKeys[i]))
	}
	return result, nil
}

func (s *ApiKeyService) UpdateApiKey(ctx context.Context, callerUserID, id string, req apikey.UpdateApiKey) (*apikey.ApiKey, error) {
	var ak models.ApiKey
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&ak).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrApiKeyNotFound
		}
		return nil, fmt.Errorf("failed to get API key: %w", err)
	}
	if isStaticAPIKeyInternal(ak) || isEnvironmentBootstrapKeyInternal(ak) {
		return nil, ErrApiKeyProtected
	}

	if req.Permissions != nil {
		// Validate against the key owner's permissions, not the caller's, so a
		// holder of apikeys:update cannot escalate another user's key beyond
		// what that user's own roles allow. Owner-less keys (env bootstrap)
		// fall back to the caller — static admin keys are rejected above.
		ownerID := callerUserID
		if ak.UserID != nil {
			ownerID = *ak.UserID
		}
		if err := s.validateGrantsAgainstUserInternal(ctx, ownerID, req.Permissions); err != nil {
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
			return fmt.Errorf("failed to update API key: %w", err)
		}
		if req.Permissions != nil && s.roleService != nil {
			if err := s.roleService.setApiKeyPermissionsInternal(ctx, tx, ak.ID, toApiKeyPermissionRowsInternal(ak.ID, req.Permissions)); err != nil {
				return fmt.Errorf("failed to update api key permissions: %w", err)
			}
			permissionsUpdated = true
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if permissionsUpdated {
		s.roleService.apiKeyCache.Delete(ak.ID)
	}

	return &apikey.ApiKey{
		ID:          ak.ID,
		Name:        ak.Name,
		Description: ak.Description,
		KeyPrefix:   ak.KeyPrefix,
		UserID:      ak.UserID,
		IsStatic:    isStaticAPIKeyInternal(ak),
		IsBootstrap: isEnvironmentBootstrapKeyInternal(ak),
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
	var rows []models.ApiKeyPermission
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
	var apiKey models.ApiKey
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&apiKey).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrApiKeyNotFound
		}
		return fmt.Errorf("failed to load API key: %w", err)
	}
	if isStaticAPIKeyInternal(apiKey) || isEnvironmentBootstrapKeyInternal(apiKey) {
		return ErrApiKeyProtected
	}

	result := s.db.WithContext(ctx).Delete(&models.ApiKey{}, "id = ?", id)
	if result.Error != nil {
		return fmt.Errorf("failed to delete API key: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrApiKeyNotFound
	}
	return nil
}

func (s *ApiKeyService) ValidateApiKey(ctx context.Context, rawKey string) (*models.User, error) {
	user, _, err := s.ValidateApiKeyWithID(ctx, rawKey)
	return user, err
}

// ValidateApiKeyWithID is like ValidateApiKey but additionally returns the
// API key's database ID so callers can resolve per-key permissions.
func (s *ApiKeyService) ValidateApiKeyWithID(ctx context.Context, rawKey string) (*models.User, string, error) {
	keyPrefix, err := parseAPIKeyPrefixInternal(rawKey)
	if err != nil {
		return nil, "", err
	}

	var apiKeys []models.ApiKey
	if err := s.db.WithContext(ctx).Where("key_prefix = ?", keyPrefix).Find(&apiKeys).Error; err != nil {
		return nil, "", fmt.Errorf("failed to find API keys: %w", err)
	}

	rawKey = normalizeAPIKeyInputInternal(rawKey)
	for _, apiKey := range apiKeys {
		if err := s.validateApiKeyHash(apiKey.KeyHash, rawKey); err == nil {
			if apiKey.ExpiresAt != nil && apiKey.ExpiresAt.Before(time.Now()) {
				return nil, "", ErrApiKeyExpired
			}

			if apiKey.UserID == nil {
				return nil, "", ErrApiKeyInvalid
			}

			s.markApiKeyUsedAsync(ctx, apiKey.ID)

			user, err := s.userService.GetUserByID(ctx, *apiKey.UserID)
			if err != nil {
				return nil, "", fmt.Errorf("failed to get user for API key: %w", err)
			}

			return user, apiKey.ID, nil
		}
	}

	return nil, "", ErrApiKeyInvalid
}

func (s *ApiKeyService) GetEnvironmentByApiKey(ctx context.Context, rawKey string) (*string, error) {
	keyPrefix, err := parseAPIKeyPrefixInternal(rawKey)
	if err != nil {
		return nil, err
	}

	var apiKeys []models.ApiKey
	if err := s.db.WithContext(ctx).Where("key_prefix = ?", keyPrefix).Find(&apiKeys).Error; err != nil {
		return nil, fmt.Errorf("failed to find API keys: %w", err)
	}

	rawKey = normalizeAPIKeyInputInternal(rawKey)
	for _, apiKey := range apiKeys {
		if err := s.validateApiKeyHash(apiKey.KeyHash, rawKey); err == nil {
			if apiKey.ExpiresAt != nil && apiKey.ExpiresAt.Before(time.Now()) {
				return nil, ErrApiKeyExpired
			}

			s.markApiKeyUsedAsync(ctx, apiKey.ID)

			return apiKey.EnvironmentID, nil
		}
	}

	return nil, ErrApiKeyInvalid
}
