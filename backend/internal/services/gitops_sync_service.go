package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/utils/mapper"
	"github.com/getarcaneapp/arcane/backend/internal/utils/pagination"
	"go.getarcane.app/types/gitops"
	"gorm.io/gorm"
)

type GitOpsSyncService struct {
	db             *database.DB
	repoService    *GitRepositoryService
	projectService *ProjectService
	eventService   *EventService
}

func NewGitOpsSyncService(db *database.DB, repoService *GitRepositoryService, projectService *ProjectService, eventService *EventService) *GitOpsSyncService {
	return &GitOpsSyncService{
		db:             db,
		repoService:    repoService,
		projectService: projectService,
		eventService:   eventService,
	}
}

func (s *GitOpsSyncService) GetSyncsPaginated(ctx context.Context, params pagination.QueryParams) ([]gitops.GitOpsSync, pagination.Response, error) {
	var syncs []models.GitOpsSync
	q := s.db.WithContext(ctx).Model(&models.GitOpsSync{}).Preload("Repository").Preload("Project")

	if term := strings.TrimSpace(params.Search); term != "" {
		searchPattern := "%" + term + "%"
		q = q.Where(
			"name LIKE ? OR branch LIKE ? OR compose_path LIKE ?",
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

	if autoSync := params.Filters["autoSync"]; autoSync != "" {
		switch autoSync {
		case "true", "1":
			q = q.Where("auto_sync = ?", true)
		case "false", "0":
			q = q.Where("auto_sync = ?", false)
		}
	}

	if repositoryID := params.Filters["repositoryId"]; repositoryID != "" {
		q = q.Where("repository_id = ?", repositoryID)
	}

	if projectID := params.Filters["projectId"]; projectID != "" {
		q = q.Where("project_id = ?", projectID)
	}

	paginationResp, err := pagination.PaginateAndSortDB(params, q, &syncs)
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to paginate gitops syncs: %w", err)
	}

	out, mapErr := mapper.MapSlice[models.GitOpsSync, gitops.GitOpsSync](syncs)
	if mapErr != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to map syncs: %w", mapErr)
	}

	return out, paginationResp, nil
}

func (s *GitOpsSyncService) GetSyncByID(ctx context.Context, id string) (*models.GitOpsSync, error) {
	var sync models.GitOpsSync
	if err := s.db.WithContext(ctx).Preload("Repository").Preload("Project").Where("id = ?", id).First(&sync).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("sync not found")
		}
		return nil, fmt.Errorf("failed to get sync: %w", err)
	}
	return &sync, nil
}

func (s *GitOpsSyncService) CreateSync(ctx context.Context, req models.CreateGitOpsSyncRequest) (*models.GitOpsSync, error) {
	// Validate repository exists
	_, err := s.repoService.GetRepositoryByID(ctx, req.RepositoryID)
	if err != nil {
		return nil, fmt.Errorf("repository not found: %w", err)
	}

	// Store the project name - project will be created during first sync
	sync := models.GitOpsSync{
		Name:         req.Name,
		RepositoryID: req.RepositoryID,
		Branch:       req.Branch,
		ComposePath:  req.ComposePath,
		ProjectName:  req.ProjectName,
		ProjectID:    nil, // Will be set during first sync
		AutoSync:     false,
		SyncInterval: 60,
		Enabled:      true,
	}

	if req.AutoSync != nil {
		sync.AutoSync = *req.AutoSync
	}
	if req.SyncInterval != nil {
		sync.SyncInterval = *req.SyncInterval
	}
	if req.Enabled != nil {
		sync.Enabled = *req.Enabled
	}

	if err := s.db.WithContext(ctx).Create(&sync).Error; err != nil {
		return nil, fmt.Errorf("failed to create sync: %w", err)
	}

	// Log event
	resourceType := "git_sync"
	_, _ = s.eventService.CreateEvent(ctx, CreateEventRequest{
		Type:         models.EventTypeGitSyncCreate,
		Severity:     models.EventSeveritySuccess,
		Title:        "Git sync created",
		Description:  fmt.Sprintf("Created git sync configuration '%s'", sync.Name),
		ResourceType: &resourceType,
		ResourceID:   &sync.ID,
		ResourceName: &sync.Name,
	})

	return s.GetSyncByID(ctx, sync.ID)
}

func (s *GitOpsSyncService) UpdateSync(ctx context.Context, id string, req models.UpdateGitOpsSyncRequest) (*models.GitOpsSync, error) {
	sync, err := s.GetSyncByID(ctx, id)
	if err != nil {
		return nil, err
	}

	updates := make(map[string]interface{})

	if req.Name != nil {
		updates["name"] = *req.Name
	}
	if req.RepositoryID != nil {
		// Validate repository exists
		_, err := s.repoService.GetRepositoryByID(ctx, *req.RepositoryID)
		if err != nil {
			return nil, fmt.Errorf("repository not found: %w", err)
		}
		updates["repository_id"] = *req.RepositoryID
	}
	if req.Branch != nil {
		updates["branch"] = *req.Branch
	}
	if req.ComposePath != nil {
		updates["compose_path"] = *req.ComposePath
	}
	if req.ProjectName != nil {
		updates["project_name"] = *req.ProjectName
	}
	if req.AutoSync != nil {
		updates["auto_sync"] = *req.AutoSync
	}
	if req.SyncInterval != nil {
		updates["sync_interval"] = *req.SyncInterval
	}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}

	if len(updates) > 0 {
		if err := s.db.WithContext(ctx).Model(sync).Updates(updates).Error; err != nil {
			return nil, fmt.Errorf("failed to update sync: %w", err)
		}

		// Log event
		resourceType := "git_sync"
		_, _ = s.eventService.CreateEvent(ctx, CreateEventRequest{
			Type:         models.EventTypeGitSyncUpdate,
			Severity:     models.EventSeveritySuccess,
			Title:        "Git sync updated",
			Description:  fmt.Sprintf("Updated git sync configuration '%s'", sync.Name),
			ResourceType: &resourceType,
			ResourceID:   &sync.ID,
			ResourceName: &sync.Name,
		})
	}

	return s.GetSyncByID(ctx, id)
}

func (s *GitOpsSyncService) DeleteSync(ctx context.Context, id string) error {
	// Get sync info before deleting
	sync, err := s.GetSyncByID(ctx, id)
	if err != nil {
		return err
	}

	if err := s.db.WithContext(ctx).Where("id = ?", id).Delete(&models.GitOpsSync{}).Error; err != nil {
		return fmt.Errorf("failed to delete sync: %w", err)
	}

	// Log event
	resourceType := "git_sync"
	_, _ = s.eventService.CreateEvent(ctx, CreateEventRequest{
		Type:         models.EventTypeGitSyncDelete,
		Severity:     models.EventSeverityInfo,
		Title:        "Git sync deleted",
		Description:  fmt.Sprintf("Deleted git sync configuration '%s'", sync.Name),
		ResourceType: &resourceType,
		ResourceID:   &sync.ID,
		ResourceName: &sync.Name,
	})

	return nil
}

func (s *GitOpsSyncService) PerformSync(ctx context.Context, id string) (*gitops.SyncResult, error) {
	sync, err := s.GetSyncByID(ctx, id)
	if err != nil {
		return nil, err
	}

	result := &gitops.SyncResult{
		Success:  false,
		SyncedAt: time.Now(),
	}

	// Get repository and auth config
	repository := sync.Repository
	if repository == nil {
		result.Message = "Repository not found"
		errMsg := "repository not found"
		result.Error = &errMsg
		s.updateSyncStatus(ctx, id, "failed", errMsg)
		s.logSyncError(ctx, sync, "Repository not found")
		return result, fmt.Errorf("repository not found")
	}

	authConfig, err := s.repoService.GetAuthConfig(ctx, repository)
	if err != nil {
		result.Message = "Failed to get authentication config"
		errMsg := err.Error()
		result.Error = &errMsg
		s.updateSyncStatus(ctx, id, "failed", errMsg)
		s.logSyncError(ctx, sync, fmt.Sprintf("Failed to get authentication: %s", err.Error()))
		return result, err
	}

	// Clone the repository
	repoPath, err := s.repoService.gitClient.Clone(repository.URL, sync.Branch, authConfig)
	if err != nil {
		result.Message = "Failed to clone repository"
		errMsg := err.Error()
		result.Error = &errMsg
		s.updateSyncStatus(ctx, id, "failed", errMsg)
		s.logSyncError(ctx, sync, fmt.Sprintf("Failed to clone repository: %s", err.Error()))
		return result, err
	}
	defer func() {
		if cleanupErr := s.repoService.gitClient.Cleanup(repoPath); cleanupErr != nil {
			slog.WarnContext(ctx, "Failed to cleanup repository", "path", repoPath, "error", cleanupErr)
		}
	}()

	// Check if compose file exists
	if !s.repoService.gitClient.FileExists(repoPath, sync.ComposePath) {
		result.Message = fmt.Sprintf("Compose file not found at %s", sync.ComposePath)
		errMsg := fmt.Sprintf("compose file not found: %s", sync.ComposePath)
		result.Error = &errMsg
		s.updateSyncStatus(ctx, id, "failed", errMsg)
		s.logSyncError(ctx, sync, errMsg)
		return result, fmt.Errorf("compose file not found: %s", sync.ComposePath)
	}

	// Read compose file content
	composeContent, err := s.repoService.gitClient.ReadFile(repoPath, sync.ComposePath)
	if err != nil {
		result.Message = "Failed to read compose file"
		errMsg := err.Error()
		result.Error = &errMsg
		s.updateSyncStatus(ctx, id, "failed", errMsg)
		s.logSyncError(ctx, sync, fmt.Sprintf("Failed to read compose file: %s", err.Error()))
		return result, err
	}

	// Get or create project
	var project *models.Project
	if sync.ProjectID != nil && *sync.ProjectID != "" {
		// Try to get existing project
		project, err = s.projectService.GetProjectFromDatabaseByID(ctx, *sync.ProjectID)
		if err != nil {
			slog.WarnContext(ctx, "Existing project not found, will create new one", "projectId", *sync.ProjectID, "error", err)
			project = nil
		}
	}

	// Create project if it doesn't exist
	if project == nil {
		// Create project from compose file
		project, err = s.projectService.CreateProject(ctx, sync.ProjectName, composeContent, nil, systemUser)
		if err != nil {
			result.Message = "Failed to create project"
			errMsg := err.Error()
			result.Error = &errMsg
			s.updateSyncStatus(ctx, id, "failed", errMsg)
			s.logSyncError(ctx, sync, fmt.Sprintf("Failed to create project: %s", err.Error()))
			return result, err
		}

		// Update sync with project ID
		if err := s.db.WithContext(ctx).Model(&models.GitOpsSync{}).Where("id = ?", id).Updates(map[string]interface{}{
			"project_id": project.ID,
		}).Error; err != nil {
			slog.ErrorContext(ctx, "Failed to update sync with project ID", "syncId", id, "projectId", project.ID, "error", err)
		}
		slog.InfoContext(ctx, "Created project for GitOps sync", "projectName", sync.ProjectName, "projectId", project.ID)
	} else {
		// Update existing project's compose file
		_, err := s.projectService.UpdateProject(ctx, project.ID, nil, &composeContent, nil)
		if err != nil {
			result.Message = "Failed to update project compose file"
			errMsg := err.Error()
			result.Error = &errMsg
			s.updateSyncStatus(ctx, id, "failed", errMsg)
			s.logSyncError(ctx, sync, fmt.Sprintf("Failed to update project: %s", err.Error()))
			return result, err
		}
		slog.InfoContext(ctx, "Updated project compose file", "projectName", project.Name, "projectId", project.ID)
	}

	// Update sync status
	s.updateSyncStatus(ctx, id, "success", "")

	result.Success = true
	result.Message = fmt.Sprintf("Successfully synced compose file from %s to project %s", sync.ComposePath, project.Name)

	// Log success event
	resourceType := "git_sync"
	_, _ = s.eventService.CreateEvent(ctx, CreateEventRequest{
		Type:         models.EventTypeGitSyncRun,
		Severity:     models.EventSeveritySuccess,
		Title:        "Git sync completed",
		Description:  fmt.Sprintf("Successfully synced '%s' to project '%s'", sync.Name, project.Name),
		ResourceType: &resourceType,
		ResourceID:   &sync.ID,
		ResourceName: &sync.Name,
	})

	slog.InfoContext(ctx, "GitOps sync completed", "syncId", id, "project", project.Name)

	return result, nil
}

func (s *GitOpsSyncService) updateSyncStatus(ctx context.Context, id, status, errorMsg string) {
	now := time.Now()
	updates := map[string]interface{}{
		"last_sync_at":     now,
		"last_sync_status": status,
	}

	if errorMsg != "" {
		updates["last_sync_error"] = errorMsg
	} else {
		updates["last_sync_error"] = nil
	}

	if err := s.db.WithContext(ctx).Model(&models.GitOpsSync{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		slog.ErrorContext(ctx, "Failed to update sync status", "error", err, "syncId", id)
	}
}

func (s *GitOpsSyncService) GetSyncStatus(ctx context.Context, id string) (*gitops.SyncStatus, error) {
	sync, err := s.GetSyncByID(ctx, id)
	if err != nil {
		return nil, err
	}

	status := &gitops.SyncStatus{
		ID:             sync.ID,
		Enabled:        sync.Enabled,
		AutoSync:       sync.AutoSync,
		LastSyncAt:     sync.LastSyncAt,
		LastSyncStatus: sync.LastSyncStatus,
		LastSyncError:  sync.LastSyncError,
	}

	// Calculate next sync time
	if sync.AutoSync && sync.Enabled && sync.LastSyncAt != nil {
		nextSync := sync.LastSyncAt.Add(time.Duration(sync.SyncInterval) * time.Minute)
		status.NextSyncAt = &nextSync
	}

	return status, nil
}

func (s *GitOpsSyncService) SyncAllEnabled(ctx context.Context) error {
	var syncs []models.GitOpsSync
	if err := s.db.WithContext(ctx).
		Preload("Repository").
		Preload("Project").
		Where("enabled = ? AND auto_sync = ?", true, true).
		Find(&syncs).Error; err != nil {
		return fmt.Errorf("failed to get enabled syncs: %w", err)
	}

	for _, sync := range syncs {
		// Check if sync is due
		if sync.LastSyncAt != nil {
			nextSync := sync.LastSyncAt.Add(time.Duration(sync.SyncInterval) * time.Minute)
			if time.Now().Before(nextSync) {
				continue
			}
		}

		// Perform sync
		result, err := s.PerformSync(ctx, sync.ID)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to sync", "syncId", sync.ID, "error", err)
			continue
		}

		if result.Success {
			slog.InfoContext(ctx, "Sync completed", "syncId", sync.ID, "message", result.Message)
		}
	}

	return nil
}

func (s *GitOpsSyncService) BrowseFiles(ctx context.Context, id string, path string) (*gitops.BrowseResponse, error) {
	sync, err := s.GetSyncByID(ctx, id)
	if err != nil {
		return nil, err
	}

	repository := sync.Repository
	if repository == nil {
		return nil, fmt.Errorf("repository not found")
	}

	authConfig, err := s.repoService.GetAuthConfig(ctx, repository)
	if err != nil {
		return nil, err
	}

	// Clone the repository
	repoPath, err := s.repoService.gitClient.Clone(repository.URL, sync.Branch, authConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to clone repository: %w", err)
	}
	defer func() {
		if cleanupErr := s.repoService.gitClient.Cleanup(repoPath); cleanupErr != nil {
			slog.WarnContext(ctx, "Failed to cleanup repository", "path", repoPath, "error", cleanupErr)
		}
	}()

	// Browse the tree
	files, err := s.repoService.gitClient.BrowseTree(repoPath, path)
	if err != nil {
		return nil, err
	}

	return &gitops.BrowseResponse{
		Path:  path,
		Files: files,
	}, nil
}

func (s *GitOpsSyncService) logSyncError(ctx context.Context, sync *models.GitOpsSync, errorMsg string) {
	resourceType := "git_sync"
	_, _ = s.eventService.CreateEvent(ctx, CreateEventRequest{
		Type:         models.EventTypeGitSyncError,
		Severity:     models.EventSeverityError,
		Title:        "Git sync failed",
		Description:  fmt.Sprintf("Failed to sync '%s': %s", sync.Name, errorMsg),
		ResourceType: &resourceType,
		ResourceID:   &sync.ID,
		ResourceName: &sync.Name,
	})
}
