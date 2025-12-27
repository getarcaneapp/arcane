package services

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/utils"
	"github.com/getarcaneapp/arcane/backend/internal/utils/git"
	"github.com/getarcaneapp/arcane/backend/internal/utils/mapper"
	"github.com/getarcaneapp/arcane/backend/internal/utils/pagination"
	"go.getarcane.app/types/gitops"
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

	if enabled := params.Filters["enabled"]; enabled != "" {
		switch enabled {
		case "true", "1":
			q = q.Where("enabled = ?", true)
		case "false", "0":
			q = q.Where("enabled = ?", false)
		}
	}

	if authType := params.Filters["authType"]; authType != "" {
		q = q.Where("auth_type = ?", authType)
	}

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

func (s *GitRepositoryService) CreateRepository(ctx context.Context, req models.CreateGitRepositoryRequest) (*models.GitRepository, error) {
	repository := models.GitRepository{
		Name:        req.Name,
		URL:         req.URL,
		AuthType:    req.AuthType,
		Username:    req.Username,
		Description: req.Description,
		Enabled:     true,
	}

	if req.Enabled != nil {
		repository.Enabled = *req.Enabled
	}

	// Encrypt sensitive fields
	if req.Token != "" {
		encrypted, err := utils.Encrypt(req.Token)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt token: %w", err)
		}
		repository.Token = encrypted
	}

	if req.SSHKey != "" {
		encrypted, err := utils.Encrypt(req.SSHKey)
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

	if req.Token != nil {
		if *req.Token == "" {
			updates["token"] = ""
		} else {
			encrypted, err := utils.Encrypt(*req.Token)
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
			encrypted, err := utils.Encrypt(*req.SSHKey)
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

	authConfig := git.AuthConfig{
		AuthType: repository.AuthType,
		Username: repository.Username,
	}

	if repository.Token != "" {
		token, err := utils.Decrypt(repository.Token)
		if err != nil {
			return fmt.Errorf("failed to decrypt token: %w", err)
		}
		authConfig.Token = token
	}

	if repository.SSHKey != "" {
		sshKey, err := utils.Decrypt(repository.SSHKey)
		if err != nil {
			return fmt.Errorf("failed to decrypt SSH key: %w", err)
		}
		authConfig.SSHKey = sshKey
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
		AuthType: repository.AuthType,
		Username: repository.Username,
	}

	if repository.Token != "" {
		token, err := utils.Decrypt(repository.Token)
		if err != nil {
			return authConfig, fmt.Errorf("failed to decrypt token: %w", err)
		}
		authConfig.Token = token
	}

	if repository.SSHKey != "" {
		sshKey, err := utils.Decrypt(repository.SSHKey)
		if err != nil {
			return authConfig, fmt.Errorf("failed to decrypt SSH key: %w", err)
		}
		authConfig.SSHKey = sshKey
	}

	return authConfig, nil
}
