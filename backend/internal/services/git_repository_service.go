package services

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/utils"
	"github.com/getarcaneapp/arcane/backend/internal/utils/crypto"
	"github.com/getarcaneapp/arcane/backend/internal/utils/git"
	"github.com/getarcaneapp/arcane/backend/internal/utils/mapper"
	"github.com/getarcaneapp/arcane/backend/internal/utils/pagination"
	"github.com/getarcaneapp/arcane/types/gitops"
	"gorm.io/gorm"
)

type GitRepositoryService struct {
	db           *database.DB
	gitClient    *git.Client
	eventService *EventService
}

func NewGitRepositoryService(db *database.DB, workDir string, eventService *EventService) *GitRepositoryService {
	return &GitRepositoryService{
		db:           db,
		gitClient:    git.NewClient(workDir),
		eventService: eventService,
	}
}

func (s *GitRepositoryService) GetRepositoriesPaginated(ctx context.Context, params pagination.QueryParams) ([]gitops.GitRepository, pagination.Response, error) {
	var repositories []models.GitRepository
	q := s.db.WithContext(ctx).Model(&models.GitRepository{})

	if term := strings.TrimSpace(params.Search); term != "" {
		searchPattern := "%" + term + "%"
		q = q.Where(
			"name LIKE ? OR url LIKE ? OR COALESCE(description, '') LIKE ?",
			searchPattern, searchPattern, searchPattern,
		)
	}

	q = pagination.ApplyBooleanFilter(q, "enabled", params.Filters["enabled"])
	q = pagination.ApplyFilter(q, "auth_type", params.Filters["authType"])

	paginationResp, err := pagination.PaginateAndSortDB(params, q, &repositories)
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to paginate git repositories: %w", err)
	}

	out, mapErr := mapper.MapSlice[models.GitRepository, gitops.GitRepository](repositories)
	if mapErr != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to map repositories: %w", mapErr)
	}

	return out, paginationResp, nil
}

func (s *GitRepositoryService) GetRepositoryByID(ctx context.Context, id string) (*models.GitRepository, error) {
	var repository models.GitRepository
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&repository).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("repository not found")
		}
		return nil, fmt.Errorf("failed to get repository: %w", err)
	}
	return &repository, nil
}

func (s *GitRepositoryService) GetRepositoryByName(ctx context.Context, name string) (*models.GitRepository, error) {
	var repository models.GitRepository
	if err := s.db.WithContext(ctx).Where("name = ?", name).First(&repository).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("repository not found")
		}
		return nil, fmt.Errorf("failed to get repository: %w", err)
	}
	return &repository, nil
}

func (s *GitRepositoryService) CreateRepository(ctx context.Context, req models.CreateGitRepositoryRequest) (*models.GitRepository, error) {
	repository := models.GitRepository{
		Name:                   req.Name,
		URL:                    req.URL,
		AuthType:               req.AuthType,
		Username:               req.Username,
		SSHHostKeyVerification: req.SSHHostKeyVerification,
		Description:            req.Description,
		Enabled:                true,
	}

	// Default to accept_new if not specified
	if repository.SSHHostKeyVerification == "" {
		repository.SSHHostKeyVerification = "accept_new"
	}

	if req.Enabled != nil {
		repository.Enabled = *req.Enabled
	}

	// Encrypt sensitive fields
	if req.Token != "" {
		encrypted, err := crypto.Encrypt(req.Token)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt token: %w", err)
		}
		repository.Token = encrypted
	}

	if req.SSHKey != "" {
		encrypted, err := crypto.Encrypt(req.SSHKey)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt SSH key: %w", err)
		}
		repository.SSHKey = encrypted
	}

	if err := s.db.WithContext(ctx).Create(&repository).Error; err != nil {
		return nil, fmt.Errorf("failed to create repository: %w", err)
	}

	// Log event
	resourceType := "git_repository"
	_, _ = s.eventService.CreateEvent(ctx, CreateEventRequest{
		Type:         models.EventTypeGitRepositoryCreate,
		Severity:     models.EventSeveritySuccess,
		Title:        "Git repository created",
		Description:  fmt.Sprintf("Created git repository '%s' (%s)", repository.Name, repository.URL),
		ResourceType: &resourceType,
		ResourceID:   &repository.ID,
		ResourceName: &repository.Name,
	})

	return &repository, nil
}

func (s *GitRepositoryService) UpdateRepository(ctx context.Context, id string, req models.UpdateGitRepositoryRequest) (*models.GitRepository, error) {
	repository, err := s.GetRepositoryByID(ctx, id)
	if err != nil {
		return nil, err
	}

	updates := make(map[string]interface{})

	if req.Name != nil {
		updates["name"] = *req.Name
	}
	if req.URL != nil {
		updates["url"] = *req.URL
	}
	if req.AuthType != nil {
		updates["auth_type"] = *req.AuthType
	}
	if req.Username != nil {
		updates["username"] = *req.Username
	}
	if req.Description != nil {
		updates["description"] = *req.Description
	}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}
	if req.SSHHostKeyVerification != nil {
		updates["ssh_host_key_verification"] = *req.SSHHostKeyVerification
	}

	if req.Token != nil {
		if *req.Token == "" {
			updates["token"] = ""
		} else {
			encrypted, err := crypto.Encrypt(*req.Token)
			if err != nil {
				return nil, fmt.Errorf("failed to encrypt token: %w", err)
			}
			updates["token"] = encrypted
		}
	}

	if req.SSHKey != nil {
		if *req.SSHKey == "" {
			updates["ssh_key"] = ""
		} else {
			encrypted, err := crypto.Encrypt(*req.SSHKey)
			if err != nil {
				return nil, fmt.Errorf("failed to encrypt SSH key: %w", err)
			}
			updates["ssh_key"] = encrypted
		}
	}

	if len(updates) > 0 {
		if err := s.db.WithContext(ctx).Model(repository).Updates(updates).Error; err != nil {
			return nil, fmt.Errorf("failed to update repository: %w", err)
		}

		// Log event
		resourceType := "git_repository"
		_, _ = s.eventService.CreateEvent(ctx, CreateEventRequest{
			Type:         models.EventTypeGitRepositoryUpdate,
			Severity:     models.EventSeveritySuccess,
			Title:        "Git repository updated",
			Description:  fmt.Sprintf("Updated git repository '%s'", repository.Name),
			ResourceType: &resourceType,
			ResourceID:   &repository.ID,
			ResourceName: &repository.Name,
		})
	}

	return s.GetRepositoryByID(ctx, id)
}

func (s *GitRepositoryService) DeleteRepository(ctx context.Context, id string) error {
	// Check if repository is used by any syncs
	var count int64
	if err := s.db.WithContext(ctx).Model(&models.GitOpsSync{}).Where("repository_id = ?", id).Count(&count).Error; err != nil {
		return fmt.Errorf("failed to check repository usage: %w", err)
	}

	if count > 0 {
		return fmt.Errorf("repository is used by %d sync configuration(s)", count)
	}

	// Get repository info before deleting
	repository, err := s.GetRepositoryByID(ctx, id)
	if err != nil {
		return err
	}

	if err := s.db.WithContext(ctx).Where("id = ?", id).Delete(&models.GitRepository{}).Error; err != nil {
		return fmt.Errorf("failed to delete repository: %w", err)
	}

	// Log event
	resourceType := "git_repository"
	_, _ = s.eventService.CreateEvent(ctx, CreateEventRequest{
		Type:         models.EventTypeGitRepositoryDelete,
		Severity:     models.EventSeverityInfo,
		Title:        "Git repository deleted",
		Description:  fmt.Sprintf("Deleted git repository '%s'", repository.Name),
		ResourceType: &resourceType,
		ResourceID:   &repository.ID,
		ResourceName: &repository.Name,
	})

	return nil
}

func (s *GitRepositoryService) TestConnection(ctx context.Context, id string, branch string) error {
	repository, err := s.GetRepositoryByID(ctx, id)
	if err != nil {
		return err
	}

	authConfig, err := s.GetAuthConfig(ctx, repository)
	if err != nil {
		return err
	}

	if branch == "" {
		branch = "main"
	}

	err = s.gitClient.TestConnection(repository.URL, branch, authConfig)
	if err != nil {
		// Log error event
		resourceType := "git_repository"
		_, _ = s.eventService.CreateEvent(ctx, CreateEventRequest{
			Type:         models.EventTypeGitRepositoryError,
			Severity:     models.EventSeverityError,
			Title:        "Git repository connection test failed",
			Description:  fmt.Sprintf("Failed to connect to repository '%s': %s", repository.Name, err.Error()),
			ResourceType: &resourceType,
			ResourceID:   &repository.ID,
			ResourceName: &repository.Name,
		})
		return err
	}

	// Log success event
	resourceType := "git_repository"
	_, _ = s.eventService.CreateEvent(ctx, CreateEventRequest{
		Type:         models.EventTypeGitRepositoryTest,
		Severity:     models.EventSeveritySuccess,
		Title:        "Git repository connection successful",
		Description:  fmt.Sprintf("Successfully connected to repository '%s'", repository.Name),
		ResourceType: &resourceType,
		ResourceID:   &repository.ID,
		ResourceName: &repository.Name,
	})

	return nil
}

func (s *GitRepositoryService) GetAuthConfig(ctx context.Context, repository *models.GitRepository) (git.AuthConfig, error) {
	authConfig := git.AuthConfig{
		AuthType:               repository.AuthType,
		Username:               repository.Username,
		SSHHostKeyVerification: repository.SSHHostKeyVerification,
	}

	if repository.Token != "" {
		token, err := crypto.Decrypt(repository.Token)
		if err != nil {
			return authConfig, fmt.Errorf("failed to decrypt token: %w", err)
		}
		authConfig.Token = token
	}

	if repository.SSHKey != "" {
		sshKey, err := crypto.Decrypt(repository.SSHKey)
		if err != nil {
			return authConfig, fmt.Errorf("failed to decrypt SSH key: %w", err)
		}
		authConfig.SSHKey = sshKey
	}

	return authConfig, nil
}

func (s *GitRepositoryService) ListBranches(ctx context.Context, id string) ([]gitops.BranchInfo, error) {
	repository, err := s.GetRepositoryByID(ctx, id)
	if err != nil {
		return nil, err
	}

	authConfig, err := s.GetAuthConfig(ctx, repository)
	if err != nil {
		return nil, err
	}

	branches, err := s.gitClient.ListBranches(repository.URL, authConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to list branches: %w", err)
	}

	var result []gitops.BranchInfo
	for _, branch := range branches {
		result = append(result, gitops.BranchInfo{
			Name:      branch.Name,
			IsDefault: branch.IsDefault,
		})
	}

	return result, nil
}

func (s *GitRepositoryService) BrowseFiles(ctx context.Context, id, branch, path string) (*gitops.BrowseResponse, error) {
	repository, err := s.GetRepositoryByID(ctx, id)
	if err != nil {
		return nil, err
	}

	authConfig, err := s.GetAuthConfig(ctx, repository)
	if err != nil {
		return nil, err
	}

	// Clone the repository
	repoPath, err := s.gitClient.Clone(repository.URL, branch, authConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to clone repository: %w", err)
	}
	defer func() {
		if cleanupErr := s.gitClient.Cleanup(repoPath); cleanupErr != nil {
			// Log cleanup error but don't fail the operation
			_ = cleanupErr
		}
	}()

	// Browse the tree
	files, err := s.gitClient.BrowseTree(repoPath, path)
	if err != nil {
		return nil, err
	}

	return &gitops.BrowseResponse{
		Path:  path,
		Files: files,
	}, nil
}

// SyncRepositories syncs repositories from a manager to this agent instance.
// It creates, updates, or deletes repositories to match the provided list.
func (s *GitRepositoryService) SyncRepositories(ctx context.Context, syncItems []gitops.RepositorySync) error {
	existingMap, err := s.getExistingRepositoriesMap(ctx)
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

	// Delete repositories that are not in the sync list
	return s.deleteUnsynced(ctx, existingMap, syncedIDs)
}

func (s *GitRepositoryService) getExistingRepositoriesMap(ctx context.Context) (map[string]*models.GitRepository, error) {
	var existing []models.GitRepository
	if err := s.db.WithContext(ctx).Find(&existing).Error; err != nil {
		return nil, fmt.Errorf("failed to get existing repositories: %w", err)
	}

	existingMap := make(map[string]*models.GitRepository)
	for i := range existing {
		existingMap[existing[i].ID] = &existing[i]
	}
	return existingMap, nil
}

func (s *GitRepositoryService) processSyncItem(ctx context.Context, item gitops.RepositorySync, existingMap map[string]*models.GitRepository) error {
	existing, exists := existingMap[item.ID]
	if exists {
		return s.updateExistingRepository(ctx, item, existing)
	}
	return s.createNewRepository(ctx, item)
}

func (s *GitRepositoryService) updateExistingRepository(ctx context.Context, item gitops.RepositorySync, existing *models.GitRepository) error {
	needsUpdate := s.checkRepositoryNeedsUpdate(item, existing)

	if needsUpdate {
		// Use Save to trigger GORM callbacks including UpdatedAt
		if err := s.db.WithContext(ctx).Save(existing).Error; err != nil {
			return fmt.Errorf("failed to update repository %s: %w", item.ID, err)
		}
	}

	return nil
}

func (s *GitRepositoryService) checkRepositoryNeedsUpdate(item gitops.RepositorySync, existing *models.GitRepository) bool {
	needsUpdate := utils.UpdateIfChanged(&existing.Name, item.Name)
	needsUpdate = utils.UpdateIfChanged(&existing.URL, item.URL) || needsUpdate
	needsUpdate = utils.UpdateIfChanged(&existing.AuthType, item.AuthType) || needsUpdate
	needsUpdate = utils.UpdateIfChanged(&existing.Username, item.Username) || needsUpdate
	needsUpdate = utils.UpdateIfChanged(&existing.SSHHostKeyVerification, item.SSHHostKeyVerification) || needsUpdate
	needsUpdate = utils.UpdateIfChanged(&existing.Description, item.Description) || needsUpdate
	needsUpdate = utils.UpdateIfChanged(&existing.Enabled, item.Enabled) || needsUpdate

	// Handle Token update
	if item.Token != "" {
		encryptedToken, err := crypto.Encrypt(item.Token)
		if err == nil {
			needsUpdate = utils.UpdateIfChanged(&existing.Token, encryptedToken) || needsUpdate
		}
	} else if existing.Token != "" {
		existing.Token = ""
		needsUpdate = true
	}

	// Handle SSH Key update
	if item.SSHKey != "" {
		encryptedSSHKey, err := crypto.Encrypt(item.SSHKey)
		if err == nil {
			needsUpdate = utils.UpdateIfChanged(&existing.SSHKey, encryptedSSHKey) || needsUpdate
		}
	} else if existing.SSHKey != "" {
		existing.SSHKey = ""
		needsUpdate = true
	}

	return needsUpdate
}

func (s *GitRepositoryService) createNewRepository(ctx context.Context, item gitops.RepositorySync) error {
	var encryptedToken, encryptedSSHKey string
	var err error

	if item.Token != "" {
		encryptedToken, err = crypto.Encrypt(item.Token)
		if err != nil {
			return fmt.Errorf("failed to encrypt token for repository %s: %w", item.ID, err)
		}
	}

	if item.SSHKey != "" {
		encryptedSSHKey, err = crypto.Encrypt(item.SSHKey)
		if err != nil {
			return fmt.Errorf("failed to encrypt SSH key for repository %s: %w", item.ID, err)
		}
	}

	sshHostKeyVerification := item.SSHHostKeyVerification
	if sshHostKeyVerification == "" {
		sshHostKeyVerification = "accept_new"
	}

	repo := models.GitRepository{
		Name:                   item.Name,
		URL:                    item.URL,
		AuthType:               item.AuthType,
		Username:               item.Username,
		Token:                  encryptedToken,
		SSHKey:                 encryptedSSHKey,
		SSHHostKeyVerification: sshHostKeyVerification,
		Description:            item.Description,
		Enabled:                item.Enabled,
	}
	repo.ID = item.ID

	if err := s.db.WithContext(ctx).Create(&repo).Error; err != nil {
		return fmt.Errorf("failed to create repository %s: %w", item.ID, err)
	}

	return nil
}

func (s *GitRepositoryService) deleteUnsynced(ctx context.Context, existingMap map[string]*models.GitRepository, syncedIDs map[string]bool) error {
	for id := range existingMap {
		if !syncedIDs[id] {
			if err := s.db.WithContext(ctx).Delete(&models.GitRepository{}, "id = ?", id).Error; err != nil {
				return fmt.Errorf("failed to delete repository %s: %w", id, err)
			}
		}
	}
	return nil
}
