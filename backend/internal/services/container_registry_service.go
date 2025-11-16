package services

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ofkm/arcane-backend/internal/database"
	"github.com/ofkm/arcane-backend/internal/dto"
	"github.com/ofkm/arcane-backend/internal/models"
	"github.com/ofkm/arcane-backend/internal/utils"
	"github.com/ofkm/arcane-backend/internal/utils/pagination"
)

type ContainerRegistryService struct {
	db         *database.DB
	envService *EnvironmentService
}

func NewContainerRegistryService(db *database.DB) *ContainerRegistryService {
	return &ContainerRegistryService{db: db}
}

// SetEnvironmentService sets the environment service for triggering syncs
func (s *ContainerRegistryService) SetEnvironmentService(envService *EnvironmentService) {
	s.envService = envService
}

// syncToAllEnvironments triggers a sync to all remote environments
func (s *ContainerRegistryService) syncToAllEnvironments(ctx context.Context) {
	if s.envService == nil {
		return
	}

	// Get all environments
	var environments []models.Environment
	if err := s.db.WithContext(ctx).Where("id != ?", "0").Where("enabled = ?", true).Find(&environments).Error; err != nil {
		slog.WarnContext(ctx, "Failed to get environments for registry sync",
			slog.String("error", err.Error()))
		return
	}

	// Sync to each environment in background
	for _, env := range environments {
		envID := env.ID
		go func(parentCtx context.Context, id string) {
			// Use context.WithoutCancel to create a context that won't be cancelled
			// when the parent request context is cancelled, since this runs in background
			bgCtx := context.WithoutCancel(parentCtx)
			if err := s.envService.SyncRegistriesToEnvironment(bgCtx, id); err != nil {
				slog.WarnContext(bgCtx, "Failed to sync registries after registry change",
					slog.String("environmentID", id),
					slog.String("error", err.Error()))
			}
		}(ctx, envID)
	}
}

func (s *ContainerRegistryService) GetAllRegistries(ctx context.Context) ([]models.ContainerRegistry, error) {
	var registries []models.ContainerRegistry
	if err := s.db.WithContext(ctx).Find(&registries).Error; err != nil {
		return nil, fmt.Errorf("failed to get container registries: %w", err)
	}
	return registries, nil
}

func (s *ContainerRegistryService) GetRegistriesPaginated(ctx context.Context, params pagination.QueryParams) ([]dto.ContainerRegistryDto, pagination.Response, error) {
	var registries []models.ContainerRegistry
	q := s.db.WithContext(ctx).Model(&models.ContainerRegistry{})

	if term := strings.TrimSpace(params.Search); term != "" {
		searchPattern := "%" + term + "%"
		q = q.Where(
			"url LIKE ? OR username LIKE ? OR COALESCE(description, '') LIKE ?",
			searchPattern, searchPattern, searchPattern,
		)
	}

	if enabled := params.Filters["enabled"]; enabled != "" {
		switch enabled {
		case "true", "1":
			q = q.Where("enabled = ?", true)
		case "false", "0":
			q = q.Where("enabled = ?", false)
		}
	}

	if insecure := params.Filters["insecure"]; insecure != "" {
		switch insecure {
		case "true", "1":
			q = q.Where("insecure = ?", true)
		case "false", "0":
			q = q.Where("insecure = ?", false)
		}
	}

	paginationResp, err := pagination.PaginateAndSortDB(params, q, &registries)
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to paginate container registries: %w", err)
	}

	out, mapErr := dto.MapSlice[models.ContainerRegistry, dto.ContainerRegistryDto](registries)
	if mapErr != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to map registries: %w", mapErr)
	}

	return out, paginationResp, nil
}

func (s *ContainerRegistryService) GetRegistryByID(ctx context.Context, id string) (*models.ContainerRegistry, error) {
	var registry models.ContainerRegistry
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&registry).Error; err != nil {
		return nil, fmt.Errorf("failed to get container registry: %w", err)
	}
	return &registry, nil
}

func (s *ContainerRegistryService) CreateRegistry(ctx context.Context, req models.CreateContainerRegistryRequest) (*models.ContainerRegistry, error) {
	// Encrypt the token before storing
	encryptedToken, err := utils.Encrypt(req.Token)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt token: %w", err)
	}

	registry := &models.ContainerRegistry{
		URL:         req.URL,
		Username:    req.Username,
		Token:       encryptedToken,
		Description: req.Description,
		Insecure:    req.Insecure != nil && *req.Insecure,
		Enabled:     req.Enabled == nil || *req.Enabled,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := s.db.WithContext(ctx).Create(registry).Error; err != nil {
		return nil, fmt.Errorf("failed to create registry: %w", err)
	}

	s.syncToAllEnvironments(ctx)

	return registry, nil
}

func (s *ContainerRegistryService) UpdateRegistry(ctx context.Context, id string, req models.UpdateContainerRegistryRequest) (*models.ContainerRegistry, error) {
	registry, err := s.GetRegistryByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// Update fields
	if req.URL != nil {
		registry.URL = *req.URL
	}
	if req.Username != nil {
		registry.Username = *req.Username
	}
	if req.Token != nil && *req.Token != "" {
		// Encrypt the new token
		encryptedToken, err := utils.Encrypt(*req.Token)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt token: %w", err)
		}
		registry.Token = encryptedToken
	}
	if req.Description != nil {
		registry.Description = req.Description
	}
	if req.Insecure != nil {
		registry.Insecure = *req.Insecure
	}
	if req.Enabled != nil {
		registry.Enabled = *req.Enabled
	}

	registry.UpdatedAt = time.Now()

	if err := s.db.WithContext(ctx).Save(registry).Error; err != nil {
		return nil, fmt.Errorf("failed to update registry: %w", err)
	}

	s.syncToAllEnvironments(ctx)

	return registry, nil
}

func (s *ContainerRegistryService) DeleteRegistry(ctx context.Context, id string) error {
	if err := s.db.WithContext(ctx).Where("id = ?", id).Delete(&models.ContainerRegistry{}).Error; err != nil {
		return fmt.Errorf("failed to delete container registry: %w", err)
	}

	s.syncToAllEnvironments(ctx)

	return nil
}

// GetDecryptedToken returns the decrypted token for a registry
func (s *ContainerRegistryService) GetDecryptedToken(ctx context.Context, id string) (string, error) {
	registry, err := s.GetRegistryByID(ctx, id)
	if err != nil {
		return "", err
	}

	decryptedToken, err := utils.Decrypt(registry.Token)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt token: %w", err)
	}

	return decryptedToken, nil
}

// GetEnabledRegistries returns all enabled registries
func (s *ContainerRegistryService) GetEnabledRegistries(ctx context.Context) ([]models.ContainerRegistry, error) {
	var registries []models.ContainerRegistry
	if err := s.db.WithContext(ctx).Where("enabled = ?", true).Find(&registries).Error; err != nil {
		return nil, fmt.Errorf("failed to get enabled container registries: %w", err)
	}
	return registries, nil
}

// SyncRegistries syncs registries from a manager to this agent instance
// It creates, updates, or deletes registries to match the provided list
func (s *ContainerRegistryService) SyncRegistries(ctx context.Context, syncItems []models.SyncRegistryItem) error {
	existingMap, err := s.getExistingRegistriesMap(ctx)
	if err != nil {
		return err
	}

	syncedIDs := make(map[string]bool)

	// Process each sync item
	for _, item := range syncItems {
		syncedIDs[item.ID] = true

		if err := s.processSyncItem(ctx, item, existingMap); err != nil {
			return err
		}
	}

	// Delete registries that are not in the sync list
	return s.deleteUnsynced(ctx, existingMap, syncedIDs)
}

func (s *ContainerRegistryService) getExistingRegistriesMap(ctx context.Context) (map[string]*models.ContainerRegistry, error) {
	var existingRegistries []models.ContainerRegistry
	if err := s.db.WithContext(ctx).Find(&existingRegistries).Error; err != nil {
		return nil, fmt.Errorf("failed to get existing registries: %w", err)
	}

	existingMap := make(map[string]*models.ContainerRegistry)
	for i := range existingRegistries {
		existingMap[existingRegistries[i].ID] = &existingRegistries[i]
	}

	return existingMap, nil
}

func (s *ContainerRegistryService) processSyncItem(ctx context.Context, item models.SyncRegistryItem, existingMap map[string]*models.ContainerRegistry) error {
	existing, exists := existingMap[item.ID]
	if exists {
		return s.updateExistingRegistry(ctx, item, existing)
	}
	return s.createNewRegistry(ctx, item)
}

func (s *ContainerRegistryService) updateExistingRegistry(ctx context.Context, item models.SyncRegistryItem, existing *models.ContainerRegistry) error {
	needsUpdate := s.checkRegistryNeedsUpdate(item, existing)

	if needsUpdate {
		existing.UpdatedAt = time.Now()
		if err := s.db.WithContext(ctx).Save(existing).Error; err != nil {
			return fmt.Errorf("failed to update registry %s: %w", item.ID, err)
		}
	}

	return nil
}

func (s *ContainerRegistryService) checkRegistryNeedsUpdate(item models.SyncRegistryItem, existing *models.ContainerRegistry) bool {
	needsUpdate := false

	if existing.URL != item.URL {
		existing.URL = item.URL
		needsUpdate = true
	}
	if existing.Username != item.Username {
		existing.Username = item.Username
		needsUpdate = true
	}

	// Always update token as it comes decrypted from manager
	encryptedToken, err := utils.Encrypt(item.Token)
	if err == nil && existing.Token != encryptedToken {
		existing.Token = encryptedToken
		needsUpdate = true
	}

	descriptionChanged := false
	if item.Description == nil || existing.Description == nil {
		descriptionChanged = item.Description != existing.Description
	} else {
		descriptionChanged = *item.Description != *existing.Description
	}
	if descriptionChanged {
		existing.Description = item.Description
		needsUpdate = true
	}

	if existing.Insecure != item.Insecure {
		existing.Insecure = item.Insecure
		needsUpdate = true
	}
	if existing.Enabled != item.Enabled {
		existing.Enabled = item.Enabled
		needsUpdate = true
	}

	return needsUpdate
}

func (s *ContainerRegistryService) createNewRegistry(ctx context.Context, item models.SyncRegistryItem) error {
	encryptedToken, err := utils.Encrypt(item.Token)
	if err != nil {
		return fmt.Errorf("failed to encrypt token for new registry %s: %w", item.ID, err)
	}

	newRegistry := &models.ContainerRegistry{
		BaseModel: models.BaseModel{
			ID: item.ID,
		},
		URL:         item.URL,
		Username:    item.Username,
		Token:       encryptedToken,
		Description: item.Description,
		Insecure:    item.Insecure,
		Enabled:     item.Enabled,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := s.db.WithContext(ctx).Create(newRegistry).Error; err != nil {
		return fmt.Errorf("failed to create registry %s: %w", item.ID, err)
	}

	return nil
}

func (s *ContainerRegistryService) deleteUnsynced(ctx context.Context, existingMap map[string]*models.ContainerRegistry, syncedIDs map[string]bool) error {
	for id := range existingMap {
		if !syncedIDs[id] {
			if err := s.db.WithContext(ctx).Where("id = ?", id).Delete(&models.ContainerRegistry{}).Error; err != nil {
				return fmt.Errorf("failed to delete registry %s: %w", id, err)
			}
		}
	}
	return nil
}
