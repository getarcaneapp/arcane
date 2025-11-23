package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ofkm/arcane-backend/internal/database"
	"github.com/ofkm/arcane-backend/internal/dto"
	"github.com/ofkm/arcane-backend/internal/models"
	"github.com/ofkm/arcane-backend/internal/utils"
	"github.com/ofkm/arcane-backend/internal/utils/pagination"
)

type GitOpsRepositoryService struct {
	db *database.DB
}

func NewGitOpsRepositoryService(db *database.DB) *GitOpsRepositoryService {
	return &GitOpsRepositoryService{db: db}
}

func (s *GitOpsRepositoryService) GetAllRepositories(ctx context.Context) ([]models.GitOpsRepository, error) {
	var repositories []models.GitOpsRepository
	if err := s.db.WithContext(ctx).Find(&repositories).Error; err != nil {
		return nil, fmt.Errorf("failed to get gitops repositories: %w", err)
	}
	return repositories, nil
}

func (s *GitOpsRepositoryService) GetRepositoriesPaginated(ctx context.Context, params pagination.QueryParams) ([]dto.GitOpsRepositoryDto, pagination.Response, error) {
	var repositories []models.GitOpsRepository
	q := s.db.WithContext(ctx).Model(&models.GitOpsRepository{})

	if term := strings.TrimSpace(params.Search); term != "" {
		searchPattern := "%" + term + "%"
		q = q.Where(
			"url LIKE ? OR branch LIKE ? OR username LIKE ? OR compose_path LIKE ? OR COALESCE(description, '') LIKE ?",
			searchPattern, searchPattern, searchPattern, searchPattern, searchPattern,
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

	if autoSync := params.Filters["autoSync"]; autoSync != "" {
		switch autoSync {
		case "true", "1":
			q = q.Where("auto_sync = ?", true)
		case "false", "0":
			q = q.Where("auto_sync = ?", false)
		}
	}

	paginationResp, err := pagination.PaginateAndSortDB(params, q, &repositories)
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to paginate gitops repositories: %w", err)
	}

	out, mapErr := dto.MapSlice[models.GitOpsRepository, dto.GitOpsRepositoryDto](repositories)
	if mapErr != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to map repositories: %w", mapErr)
	}

	return out, paginationResp, nil
}

func (s *GitOpsRepositoryService) GetRepositoryByID(ctx context.Context, id string) (*models.GitOpsRepository, error) {
	var repository models.GitOpsRepository
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&repository).Error; err != nil {
		return nil, fmt.Errorf("failed to get gitops repository: %w", err)
	}
	return &repository, nil
}

func (s *GitOpsRepositoryService) CreateRepository(ctx context.Context, req models.CreateGitOpsRepositoryRequest) (*models.GitOpsRepository, error) {
	// Encrypt the token before storing
	var encryptedToken string
	if req.Token != "" {
		encrypted, err := utils.Encrypt(req.Token)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt token: %w", err)
		}
		encryptedToken = encrypted
	}

	// Set default branch if not provided
	branch := req.Branch
	if branch == "" {
		branch = "main"
	}

	// Set default sync interval if not provided (60 minutes)
	syncInterval := 60
	if req.SyncInterval != nil && *req.SyncInterval > 0 {
		syncInterval = *req.SyncInterval
	}

	repository := &models.GitOpsRepository{
		URL:          req.URL,
		Branch:       branch,
		Username:     req.Username,
		Token:        encryptedToken,
		ComposePath:  req.ComposePath,
		Description:  req.Description,
		AutoSync:     req.AutoSync != nil && *req.AutoSync,
		SyncInterval: syncInterval,
		Enabled:      req.Enabled == nil || *req.Enabled,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	if err := s.db.WithContext(ctx).Create(repository).Error; err != nil {
		return nil, fmt.Errorf("failed to create gitops repository: %w", err)
	}

	return repository, nil
}

func (s *GitOpsRepositoryService) UpdateRepository(ctx context.Context, id string, req models.UpdateGitOpsRepositoryRequest) (*models.GitOpsRepository, error) {
	repository, err := s.GetRepositoryByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// Update fields
	utils.UpdateIfChanged(&repository.URL, req.URL)
	utils.UpdateIfChanged(&repository.Branch, req.Branch)
	utils.UpdateIfChanged(&repository.Username, req.Username)
	utils.UpdateIfChanged(&repository.ComposePath, req.ComposePath)

	if req.Token != nil && *req.Token != "" {
		// Encrypt the new token
		encryptedToken, err := utils.Encrypt(*req.Token)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt token: %w", err)
		}
		utils.UpdateIfChanged(&repository.Token, encryptedToken)
	}

	utils.UpdateIfChanged(&repository.Description, req.Description)
	utils.UpdateIfChanged(&repository.AutoSync, req.AutoSync)
	utils.UpdateIfChanged(&repository.SyncInterval, req.SyncInterval)
	utils.UpdateIfChanged(&repository.Enabled, req.Enabled)

	repository.UpdatedAt = time.Now()

	if err := s.db.WithContext(ctx).Save(repository).Error; err != nil {
		return nil, fmt.Errorf("failed to update gitops repository: %w", err)
	}

	return repository, nil
}

func (s *GitOpsRepositoryService) DeleteRepository(ctx context.Context, id string) error {
	if err := s.db.WithContext(ctx).Where("id = ?", id).Delete(&models.GitOpsRepository{}).Error; err != nil {
		return fmt.Errorf("failed to delete gitops repository: %w", err)
	}
	return nil
}

// GetDecryptedToken returns the decrypted token for a repository
func (s *GitOpsRepositoryService) GetDecryptedToken(ctx context.Context, id string) (string, error) {
	repository, err := s.GetRepositoryByID(ctx, id)
	if err != nil {
		return "", err
	}

	if repository.Token == "" {
		return "", nil
	}

	decryptedToken, err := utils.Decrypt(repository.Token)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt token: %w", err)
	}

	return decryptedToken, nil
}

// GetEnabledRepositories returns all enabled repositories
func (s *GitOpsRepositoryService) GetEnabledRepositories(ctx context.Context) ([]models.GitOpsRepository, error) {
	var repositories []models.GitOpsRepository
	if err := s.db.WithContext(ctx).Where("enabled = ?", true).Find(&repositories).Error; err != nil {
		return nil, fmt.Errorf("failed to get enabled gitops repositories: %w", err)
	}
	return repositories, nil
}

// SyncRepositories syncs repositories from a manager to this agent instance
// It creates, updates, or deletes repositories to match the provided list
func (s *GitOpsRepositoryService) SyncRepositories(ctx context.Context, syncItems []dto.GitOpsRepositorySyncDto) error {
	existingMap, err := s.getExistingRepositoriesMapInternal(ctx)
	if err != nil {
		return err
	}

	syncedIDs := make(map[string]bool)

	// Process each sync item
	for _, item := range syncItems {
		syncedIDs[item.ID] = true

		if err := s.processSyncItemInternal(ctx, item, existingMap); err != nil {
			return err
		}
	}

	// Delete repositories that are not in the sync list
	return s.deleteUnsyncedInternal(ctx, existingMap, syncedIDs)
}

func (s *GitOpsRepositoryService) getExistingRepositoriesMapInternal(ctx context.Context) (map[string]*models.GitOpsRepository, error) {
	var existingRepositories []models.GitOpsRepository
	if err := s.db.WithContext(ctx).Find(&existingRepositories).Error; err != nil {
		return nil, fmt.Errorf("failed to get existing repositories: %w", err)
	}

	existingMap := make(map[string]*models.GitOpsRepository)
	for i := range existingRepositories {
		existingMap[existingRepositories[i].ID] = &existingRepositories[i]
	}

	return existingMap, nil
}

func (s *GitOpsRepositoryService) processSyncItemInternal(ctx context.Context, item dto.GitOpsRepositorySyncDto, existingMap map[string]*models.GitOpsRepository) error {
	existing, exists := existingMap[item.ID]
	if exists {
		return s.updateExistingRepositoryInternal(ctx, item, existing)
	}
	return s.createNewRepositoryInternal(ctx, item)
}

func (s *GitOpsRepositoryService) updateExistingRepositoryInternal(ctx context.Context, item dto.GitOpsRepositorySyncDto, existing *models.GitOpsRepository) error {
	needsUpdate := s.checkRepositoryNeedsUpdateInternal(item, existing)

	if needsUpdate {
		existing.UpdatedAt = time.Now()
		if err := s.db.WithContext(ctx).Save(existing).Error; err != nil {
			return fmt.Errorf("failed to update repository %s: %w", item.ID, err)
		}
	}

	return nil
}

func (s *GitOpsRepositoryService) checkRepositoryNeedsUpdateInternal(item dto.GitOpsRepositorySyncDto, existing *models.GitOpsRepository) bool {
	needsUpdate := utils.UpdateIfChanged(&existing.URL, item.URL)
	needsUpdate = utils.UpdateIfChanged(&existing.Branch, item.Branch) || needsUpdate
	needsUpdate = utils.UpdateIfChanged(&existing.Username, item.Username) || needsUpdate
	needsUpdate = utils.UpdateIfChanged(&existing.ComposePath, item.ComposePath) || needsUpdate

	// Always update token as it comes decrypted from manager
	if item.Token != "" {
		encryptedToken, err := utils.Encrypt(item.Token)
		if err == nil {
			needsUpdate = utils.UpdateIfChanged(&existing.Token, encryptedToken) || needsUpdate
		}
	}

	needsUpdate = utils.UpdateIfChanged(&existing.Description, item.Description) || needsUpdate
	needsUpdate = utils.UpdateIfChanged(&existing.AutoSync, item.AutoSync) || needsUpdate
	needsUpdate = utils.UpdateIfChanged(&existing.SyncInterval, item.SyncInterval) || needsUpdate
	needsUpdate = utils.UpdateIfChanged(&existing.Enabled, item.Enabled) || needsUpdate
	needsUpdate = utils.UpdateIfChanged(&existing.LastSyncedAt, item.LastSyncedAt) || needsUpdate
	needsUpdate = utils.UpdateIfChanged(&existing.LastSyncStatus, item.LastSyncStatus) || needsUpdate
	needsUpdate = utils.UpdateIfChanged(&existing.LastSyncError, item.LastSyncError) || needsUpdate

	return needsUpdate
}

func (s *GitOpsRepositoryService) createNewRepositoryInternal(ctx context.Context, item dto.GitOpsRepositorySyncDto) error {
	var encryptedToken string
	if item.Token != "" {
		encrypted, err := utils.Encrypt(item.Token)
		if err != nil {
			return fmt.Errorf("failed to encrypt token for new repository %s: %w", item.ID, err)
		}
		encryptedToken = encrypted
	}

	newRepository := &models.GitOpsRepository{
		BaseModel: models.BaseModel{
			ID: item.ID,
		},
		URL:            item.URL,
		Branch:         item.Branch,
		Username:       item.Username,
		Token:          encryptedToken,
		ComposePath:    item.ComposePath,
		Description:    item.Description,
		AutoSync:       item.AutoSync,
		SyncInterval:   item.SyncInterval,
		Enabled:        item.Enabled,
		LastSyncedAt:   item.LastSyncedAt,
		LastSyncStatus: item.LastSyncStatus,
		LastSyncError:  item.LastSyncError,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	if err := s.db.WithContext(ctx).Create(newRepository).Error; err != nil {
		return fmt.Errorf("failed to create repository %s: %w", item.ID, err)
	}

	return nil
}

func (s *GitOpsRepositoryService) deleteUnsyncedInternal(ctx context.Context, existingMap map[string]*models.GitOpsRepository, syncedIDs map[string]bool) error {
	for id := range existingMap {
		if !syncedIDs[id] {
			if err := s.db.WithContext(ctx).Where("id = ?", id).Delete(&models.GitOpsRepository{}).Error; err != nil {
				return fmt.Errorf("failed to delete repository %s: %w", id, err)
			}
		}
	}
	return nil
}
