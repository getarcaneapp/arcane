package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/dto"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/utils/pagination"
	"gorm.io/gorm"
)

var (
	ErrApiKeyNotFound = errors.New("API key not found")
	ErrApiKeyExpired  = errors.New("API key has expired")
	ErrApiKeyInvalid  = errors.New("invalid API key")
)

const (
	apiKeyPrefix    = "arc_"
	apiKeyLength    = 32
	apiKeyPrefixLen = 8
)

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

func (s *ApiKeyService) CreateApiKey(ctx context.Context, userID string, req dto.CreateApiKeyDto) (*dto.ApiKeyCreatedDto, error) {
	rawKey, err := s.generateApiKey()
	if err != nil {
		return nil, err
	}

	keyHash, err := s.hashApiKey(rawKey)
	if err != nil {
		return nil, fmt.Errorf("failed to hash API key: %w", err)
	}

	keyPrefix := rawKey[:len(apiKeyPrefix)+apiKeyPrefixLen]

	apiKey := &models.ApiKey{
		Name:        req.Name,
		Description: req.Description,
		KeyHash:     keyHash,
		KeyPrefix:   keyPrefix,
		UserID:      userID,
		ExpiresAt:   req.ExpiresAt,
	}

	if err := s.db.WithContext(ctx).Create(apiKey).Error; err != nil {
		return nil, fmt.Errorf("failed to create API key: %w", err)
	}

	return &dto.ApiKeyCreatedDto{
		ApiKeyDto: dto.ApiKeyDto{
			ID:          apiKey.ID,
			Name:        apiKey.Name,
			Description: apiKey.Description,
			KeyPrefix:   apiKey.KeyPrefix,
			UserID:      apiKey.UserID,
			ExpiresAt:   apiKey.ExpiresAt,
			LastUsedAt:  apiKey.LastUsedAt,
			CreatedAt:   apiKey.CreatedAt,
			UpdatedAt:   apiKey.UpdatedAt,
		},
		Key: rawKey,
	}, nil
}

func (s *ApiKeyService) GetApiKey(ctx context.Context, id string) (*dto.ApiKeyDto, error) {
	var apiKey models.ApiKey
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&apiKey).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrApiKeyNotFound
		}
		return nil, fmt.Errorf("failed to get API key: %w", err)
	}

	return &dto.ApiKeyDto{
		ID:          apiKey.ID,
		Name:        apiKey.Name,
		Description: apiKey.Description,
		KeyPrefix:   apiKey.KeyPrefix,
		UserID:      apiKey.UserID,
		ExpiresAt:   apiKey.ExpiresAt,
		LastUsedAt:  apiKey.LastUsedAt,
		CreatedAt:   apiKey.CreatedAt,
		UpdatedAt:   apiKey.UpdatedAt,
	}, nil
}

func (s *ApiKeyService) ListApiKeys(ctx context.Context, params pagination.QueryParams) ([]dto.ApiKeyDto, pagination.Response, error) {
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

	result := make([]dto.ApiKeyDto, len(apiKeys))
	for i, apiKey := range apiKeys {
		result[i] = dto.ApiKeyDto{
			ID:          apiKey.ID,
			Name:        apiKey.Name,
			Description: apiKey.Description,
			KeyPrefix:   apiKey.KeyPrefix,
			UserID:      apiKey.UserID,
			ExpiresAt:   apiKey.ExpiresAt,
			LastUsedAt:  apiKey.LastUsedAt,
			CreatedAt:   apiKey.CreatedAt,
			UpdatedAt:   apiKey.UpdatedAt,
		}
	}

	return result, paginationResp, nil
}

func (s *ApiKeyService) UpdateApiKey(ctx context.Context, id string, req dto.UpdateApiKeyDto) (*dto.ApiKeyDto, error) {
	var apiKey models.ApiKey
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&apiKey).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrApiKeyNotFound
		}
		return nil, fmt.Errorf("failed to get API key: %w", err)
	}

	if req.Name != nil {
		apiKey.Name = *req.Name
	}
	if req.Description != nil {
		apiKey.Description = req.Description
	}
	if req.ExpiresAt != nil {
		apiKey.ExpiresAt = req.ExpiresAt
	}

	if err := s.db.WithContext(ctx).Save(&apiKey).Error; err != nil {
		return nil, fmt.Errorf("failed to update API key: %w", err)
	}

	return &dto.ApiKeyDto{
		ID:          apiKey.ID,
		Name:        apiKey.Name,
		Description: apiKey.Description,
		KeyPrefix:   apiKey.KeyPrefix,
		UserID:      apiKey.UserID,
		ExpiresAt:   apiKey.ExpiresAt,
		LastUsedAt:  apiKey.LastUsedAt,
		CreatedAt:   apiKey.CreatedAt,
		UpdatedAt:   apiKey.UpdatedAt,
	}, nil
}

func (s *ApiKeyService) DeleteApiKey(ctx context.Context, id string) error {
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
	if !strings.HasPrefix(rawKey, apiKeyPrefix) {
		return nil, ErrApiKeyInvalid
	}

	keyPrefix := rawKey[:len(apiKeyPrefix)+apiKeyPrefixLen]

	var apiKeys []models.ApiKey
	if err := s.db.WithContext(ctx).Where("key_prefix = ?", keyPrefix).Find(&apiKeys).Error; err != nil {
		return nil, fmt.Errorf("failed to find API keys: %w", err)
	}

	for _, apiKey := range apiKeys {
		if err := s.validateApiKeyHash(apiKey.KeyHash, rawKey); err == nil {
			if apiKey.ExpiresAt != nil && apiKey.ExpiresAt.Before(time.Now()) {
				return nil, ErrApiKeyExpired
			}

			// Update last_used_at asynchronously to avoid blocking auth flow
			go func(keyID string) {
				bgCtx := context.WithoutCancel(ctx)
				now := time.Now()
				s.db.WithContext(bgCtx).Model(&models.ApiKey{}).Where("id = ?", keyID).Update("last_used_at", now)
			}(apiKey.ID)

			user, err := s.userService.GetUserByID(ctx, apiKey.UserID)
			if err != nil {
				return nil, fmt.Errorf("failed to get user for API key: %w", err)
			}

			return user, nil
		}
	}

	return nil, ErrApiKeyInvalid
}
