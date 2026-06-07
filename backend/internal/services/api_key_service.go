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
	"github.com/getarcaneapp/arcane/backend/pkg/pagination"
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
	argon2Params *Argon2Params
}

func NewApiKeyService(db *database.DB, userService *UserService) *ApiKeyService {
	return &ApiKeyService{
		db:           db,
		userService:  userService,
		argon2Params: DefaultArgon2Params(),
	}
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
	rawKey, err := s.generateApiKey()
	if err != nil {
		return nil, err
	}

	return s.createAPIKeyWithRawKey(ctx, &userID, rawKey, req, nil, nil)
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

func toAPIKeyDTOInternal(ak *models.ApiKey) apikey.ApiKey {
	return apikey.ApiKey{
		ID:          ak.ID,
		Name:        ak.Name,
		Description: ak.Description,
		KeyPrefix:   ak.KeyPrefix,
		UserID:      ak.UserID,
		IsStatic:    isStaticAPIKeyInternal(*ak),
		ExpiresAt:   ak.ExpiresAt,
		LastUsedAt:  ak.LastUsedAt,
		CreatedAt:   ak.CreatedAt,
		UpdatedAt:   ak.UpdatedAt,
	}
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
	return s.createAPIKeyWithRawKey(ctx, nil, rawKey, apikey.CreateApiKey{
		Name:        name,
		Description: new("Auto-generated key for environment pairing"),
	}, nil, &environmentID)
}

func (s *ApiKeyService) GetApiKey(ctx context.Context, id string) (*apikey.ApiKey, error) {
	var ak models.ApiKey
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&ak).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrApiKeyNotFound
		}
		return nil, fmt.Errorf("failed to get API key: %w", err)
	}

	return &apikey.ApiKey{
		ID:          ak.ID,
		Name:        ak.Name,
		Description: ak.Description,
		KeyPrefix:   ak.KeyPrefix,
		UserID:      ak.UserID,
		IsStatic:    isStaticAPIKeyInternal(ak),
		ExpiresAt:   ak.ExpiresAt,
		LastUsedAt:  ak.LastUsedAt,
		CreatedAt:   ak.CreatedAt,
		UpdatedAt:   ak.UpdatedAt,
	}, nil
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
	for i, ak := range apiKeys {
		result[i] = toAPIKeyDTOInternal(&ak)
	}

	return result, paginationResp, nil
}

func (s *ApiKeyService) UpdateApiKey(ctx context.Context, id string, req apikey.UpdateApiKey) (*apikey.ApiKey, error) {
	var ak models.ApiKey
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&ak).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrApiKeyNotFound
		}
		return nil, fmt.Errorf("failed to get API key: %w", err)
	}
	if isStaticAPIKeyInternal(ak) {
		return nil, ErrApiKeyProtected
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

	if err := s.db.WithContext(ctx).Save(&ak).Error; err != nil {
		return nil, fmt.Errorf("failed to update API key: %w", err)
	}

	return &apikey.ApiKey{
		ID:          ak.ID,
		Name:        ak.Name,
		Description: ak.Description,
		KeyPrefix:   ak.KeyPrefix,
		UserID:      ak.UserID,
		IsStatic:    isStaticAPIKeyInternal(ak),
		ExpiresAt:   ak.ExpiresAt,
		LastUsedAt:  ak.LastUsedAt,
		CreatedAt:   ak.CreatedAt,
		UpdatedAt:   ak.UpdatedAt,
	}, nil
}

func (s *ApiKeyService) DeleteApiKey(ctx context.Context, id string) error {
	var apiKey models.ApiKey
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&apiKey).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrApiKeyNotFound
		}
		return fmt.Errorf("failed to load API key: %w", err)
	}
	if isStaticAPIKeyInternal(apiKey) {
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

			if apiKey.UserID == nil {
				return nil, ErrApiKeyInvalid
			}

			s.markApiKeyUsedAsync(ctx, apiKey.ID)

			user, err := s.userService.GetUserByID(ctx, *apiKey.UserID)
			if err != nil {
				return nil, fmt.Errorf("failed to get user for API key: %w", err)
			}

			return user, nil
		}
	}

	return nil, ErrApiKeyInvalid
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
