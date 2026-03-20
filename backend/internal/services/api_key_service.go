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
	arcstorage "github.com/getarcaneapp/arcane/backend/internal/storage"
	"github.com/getarcaneapp/arcane/backend/pkg/pagination"
	"github.com/getarcaneapp/arcane/types/apikey"
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
	description := "Environment-managed static API key for the built-in admin account"
	return &description
}()

type ApiKeyService struct {
	db           *database.DB
	repo         arcstorage.APIKeyRepository
	userService  *UserService
	argon2Params *Argon2Params
}

func NewApiKeyService(db *database.DB, userService *UserService, repo ...arcstorage.APIKeyRepository) *ApiKeyService {
	var selectedRepo arcstorage.APIKeyRepository
	if len(repo) > 0 && repo[0] != nil {
		selectedRepo = repo[0]
	} else if db != nil {
		selectedRepo = arcstorage.NewSQLAPIKeyRepository(db)
	}
	return &ApiKeyService{
		db:           db,
		repo:         selectedRepo,
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
		apiKey, err := s.repo.GetByID(bgCtx, keyID)
		if err != nil || apiKey == nil {
			return
		}
		if apiKey.LastUsedAt != nil && !apiKey.LastUsedAt.Before(cutoff) {
			return
		}
		apiKey.LastUsedAt = &now
		_ = s.repo.Upsert(bgCtx, apiKey)
	}(keyID)
}

func (s *ApiKeyService) CreateApiKey(ctx context.Context, userID string, req apikey.CreateApiKey) (*apikey.ApiKeyCreatedDto, error) {
	rawKey, err := s.generateApiKey()
	if err != nil {
		return nil, err
	}

	return s.createAPIKeyWithRawKey(ctx, userID, rawKey, req, nil, nil)
}

func (s *ApiKeyService) createAPIKeyWithRawKey(
	ctx context.Context,
	userID string,
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

	if err := s.repo.Create(ctx, ak); err != nil {
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
	managedBy := managedByAdminBootstrap
	return s.createAPIKeyWithRawKey(ctx, userID, rawKey, apikey.CreateApiKey{
		Name:        defaultAdminAPIKeyName,
		Description: defaultAdminAPIKeyDescription,
	}, &managedBy, nil)
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

func (s *ApiKeyService) listManagedAPIKeys(ctx context.Context, userID string) ([]models.ApiKey, error) {
	return s.repo.ListManagedByUser(ctx, userID, managedByAdminBootstrap)
}

func (s *ApiKeyService) deleteManagedAPIKeysByIDs(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	if err := s.repo.DeleteMany(ctx, ids); err != nil {
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

func (s *ApiKeyService) updateMatchingManagedAPIKey(ctx context.Context, apiKeyID string) error {
	apiKey, err := s.repo.GetByID(ctx, apiKeyID)
	if err != nil {
		return fmt.Errorf("failed to load managed API key metadata: %w", err)
	}
	if apiKey == nil {
		return ErrApiKeyNotFound
	}
	apiKey.Name = defaultAdminAPIKeyName
	apiKey.Description = defaultAdminAPIKeyDescription
	managedBy := managedByAdminBootstrap
	apiKey.ManagedBy = &managedBy
	if err := s.repo.Upsert(ctx, apiKey); err != nil {
		return fmt.Errorf("failed to update managed API key metadata: %w", err)
	}
	return nil
}

func (s *ApiKeyService) createManagedDefaultAdminAPIKey(ctx context.Context, userID, rawKey string) error {
	keyPrefix, err := parseAPIKeyPrefixInternal(rawKey)
	if err != nil {
		return err
	}

	keyHash, err := s.hashApiKey(rawKey)
	if err != nil {
		return fmt.Errorf("failed to hash API key: %w", err)
	}

	managedBy := managedByAdminBootstrap
	ak := &models.ApiKey{
		Name:        defaultAdminAPIKeyName,
		Description: defaultAdminAPIKeyDescription,
		KeyHash:     keyHash,
		KeyPrefix:   keyPrefix,
		ManagedBy:   &managedBy,
		UserID:      userID,
	}

	if err := s.repo.Create(ctx, ak); err != nil {
		return fmt.Errorf("failed to create managed API key: %w", err)
	}
	return nil
}

func (s *ApiKeyService) reconcileManagedAPIKeys(ctx context.Context, userID string, rawKey string) error {
	managedKeys, err := s.listManagedAPIKeys(ctx, userID)
	if err != nil {
		return err
	}

	if rawKey == "" {
		return s.deleteManagedAPIKeysByIDs(ctx, managedAPIKeyDeleteIDsInternal(managedKeys, -1))
	}

	matchingIndex := s.findMatchingManagedAPIKey(rawKey, managedKeys)
	if matchingIndex >= 0 {
		if err := s.updateMatchingManagedAPIKey(ctx, managedKeys[matchingIndex].ID); err != nil {
			return err
		}
		return s.deleteManagedAPIKeysByIDs(ctx, managedAPIKeyDeleteIDsInternal(managedKeys, matchingIndex))
	}

	if err := s.deleteManagedAPIKeysByIDs(ctx, managedAPIKeyDeleteIDsInternal(managedKeys, -1)); err != nil {
		return err
	}

	return s.createManagedDefaultAdminAPIKey(ctx, userID, rawKey)
}

func (s *ApiKeyService) ReconcileDefaultAdminAPIKey(ctx context.Context, rawKey string) error {
	rawKey = normalizeAPIKeyInputInternal(rawKey)

	adminUser, err := s.getDefaultAdminUser(ctx)
	if err != nil || adminUser == nil {
		return err
	}

	return s.reconcileManagedAPIKeys(ctx, adminUser.ID, rawKey)
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
	description := "Auto-generated key for environment pairing"

	return s.createAPIKeyWithRawKey(ctx, userID, rawKey, apikey.CreateApiKey{
		Name:        name,
		Description: &description,
	}, nil, &environmentID)
}

func (s *ApiKeyService) GetApiKey(ctx context.Context, id string) (*apikey.ApiKey, error) {
	ak, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get API key: %w", err)
	}
	if ak == nil {
		return nil, ErrApiKeyNotFound
	}

	return &apikey.ApiKey{
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
	}, nil
}

func (s *ApiKeyService) ListApiKeys(ctx context.Context, params pagination.QueryParams) ([]apikey.ApiKey, pagination.Response, error) {
	apiKeys, err := s.repo.List(ctx)
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to list API keys: %w", err)
	}
	searchConfig := pagination.Config[models.ApiKey]{
		SearchAccessors: []pagination.SearchAccessor[models.ApiKey]{
			func(ak models.ApiKey) (string, error) { return ak.Name, nil },
			func(ak models.ApiKey) (string, error) {
				if ak.Description == nil {
					return "", nil
				}
				return *ak.Description, nil
			},
		},
		SortBindings: []pagination.SortBinding[models.ApiKey]{
			{Key: "name", Fn: func(a, b models.ApiKey) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) }},
			{Key: "expires_at", Fn: func(a, b models.ApiKey) int { return compareTimePointers(a.ExpiresAt, b.ExpiresAt) }},
			{Key: "last_used_at", Fn: func(a, b models.ApiKey) int { return compareTimePointers(a.LastUsedAt, b.LastUsedAt) }},
			{Key: "created_at", Fn: func(a, b models.ApiKey) int { return a.CreatedAt.Compare(b.CreatedAt) }},
		},
	}
	filtered := pagination.SearchOrderAndPaginate(apiKeys, params, searchConfig)
	paginationResp := pagination.BuildResponseFromFilterResult(filtered, params)

	result := make([]apikey.ApiKey, len(filtered.Items))
	for i, ak := range filtered.Items {
		result[i] = toAPIKeyDTOInternal(&ak)
	}

	return result, paginationResp, nil
}

func (s *ApiKeyService) UpdateApiKey(ctx context.Context, id string, req apikey.UpdateApiKey) (*apikey.ApiKey, error) {
	ak, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get API key: %w", err)
	}
	if ak == nil {
		return nil, ErrApiKeyNotFound
	}
	if isStaticAPIKeyInternal(*ak) {
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

	if err := s.repo.Upsert(ctx, ak); err != nil {
		return nil, fmt.Errorf("failed to update API key: %w", err)
	}

	return &apikey.ApiKey{
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
	}, nil
}

func (s *ApiKeyService) DeleteApiKey(ctx context.Context, id string) error {
	apiKey, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to load API key: %w", err)
	}
	if apiKey == nil {
		return ErrApiKeyNotFound
	}
	if isStaticAPIKeyInternal(*apiKey) {
		return ErrApiKeyProtected
	}
	return s.repo.Delete(ctx, id)
}

func (s *ApiKeyService) ValidateApiKey(ctx context.Context, rawKey string) (*models.User, error) {
	keyPrefix, err := parseAPIKeyPrefixInternal(rawKey)
	if err != nil {
		return nil, err
	}

	apiKeys, err := s.repo.ListByKeyPrefix(ctx, keyPrefix)
	if err != nil {
		return nil, fmt.Errorf("failed to find API keys: %w", err)
	}

	rawKey = normalizeAPIKeyInputInternal(rawKey)
	for _, apiKey := range apiKeys {
		if err := s.validateApiKeyHash(apiKey.KeyHash, rawKey); err == nil {
			if apiKey.ExpiresAt != nil && apiKey.ExpiresAt.Before(time.Now()) {
				return nil, ErrApiKeyExpired
			}

			s.markApiKeyUsedAsync(ctx, apiKey.ID)

			user, err := s.userService.GetUserByID(ctx, apiKey.UserID)
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

	apiKeys, err := s.repo.ListByKeyPrefix(ctx, keyPrefix)
	if err != nil {
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
