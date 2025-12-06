package services

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
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
	db                *database.DB
	repoService       *GitRepositoryService
	projectService    *ProjectService
}

func NewGitOpsSyncService(db *database.DB, repoService *GitRepositoryService, projectService *ProjectService) *GitOpsSyncService {
	return &GitOpsSyncService{
		db:                db,
		repoService:       repoService,
		projectService:    projectService,
	}
}

func (s *GitOpsSyncService) GetAllSyncs(ctx context.Context) ([]models.GitOpsSync, error) {
	var syncs []models.GitOpsSync
	if err := s.db.WithContext(ctx).Preload("Repository").Preload("Project").Find(&syncs).Error; err != nil {
		return nil, fmt.Errorf("failed to get gitops syncs: %w", err)
	}
	return syncs, nil
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
		if err == gorm.ErrRecordNotFound {
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

	// Validate project exists
	_, err = s.projectService.GetProjectFromDatabaseByID(ctx, req.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("project not found: %w", err)
	}

	sync := models.GitOpsSync{
		Name:         req.Name,
		RepositoryID: req.RepositoryID,
		Branch:       req.Branch,
		ComposePath:  req.ComposePath,
		ProjectID:    req.ProjectID,
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
	if req.ProjectID != nil {
		// Validate project exists
		_, err := s.projectService.GetProjectFromDatabaseByID(ctx, *req.ProjectID)
		if err != nil {
			return nil, fmt.Errorf("project not found: %w", err)
		}
		updates["project_id"] = *req.ProjectID
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
	}

	return s.GetSyncByID(ctx, id)
}

func (s *GitOpsSyncService) DeleteSync(ctx context.Context, id string) error {
	if err := s.db.WithContext(ctx).Where("id = ?", id).Delete(&models.GitOpsSync{}).Error; err != nil {
		return fmt.Errorf("failed to delete sync: %w", err)
	}
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
		return result, fmt.Errorf("repository not found")
	}

	authConfig, err := s.repoService.GetAuthConfig(ctx, repository)
	if err != nil {
		result.Message = "Failed to get authentication config"
		errMsg := err.Error()
		result.Error = &errMsg
		s.updateSyncStatus(ctx, id, "failed", errMsg)
		return result, err
	}

	// Clone the repository
	repoPath, err := s.repoService.gitClient.Clone(repository.URL, sync.Branch, authConfig)
	if err != nil {
		result.Message = "Failed to clone repository"
		errMsg := err.Error()
		result.Error = &errMsg
		s.updateSyncStatus(ctx, id, "failed", errMsg)
		return result, err
	}
	defer s.repoService.gitClient.Cleanup(repoPath)

	// Check if compose file exists
	composePath := filepath.Join(repoPath, sync.ComposePath)
	if !s.repoService.gitClient.FileExists(repoPath, sync.ComposePath) {
		result.Message = fmt.Sprintf("Compose file not found at %s", sync.ComposePath)
		errMsg := fmt.Sprintf("compose file not found: %s", sync.ComposePath)
		result.Error = &errMsg
		s.updateSyncStatus(ctx, id, "failed", errMsg)
		return result, fmt.Errorf("compose file not found: %s", sync.ComposePath)
	}

	// Get project
	project, err := s.projectService.GetProjectFromDatabaseByID(ctx, sync.ProjectID)
	if err != nil {
		result.Message = "Project not found"
		errMsg := err.Error()
		result.Error = &errMsg
		s.updateSyncStatus(ctx, id, "failed", errMsg)
		return result, err
	}

	// Ensure project directory exists
	projectComposeDir := project.Path
	if projectComposeDir == "" {
		result.Message = "Project path is empty"
		errMsg := "project path is empty"
		result.Error = &errMsg
		s.updateSyncStatus(ctx, id, "failed", errMsg)
		return result, fmt.Errorf("project path is empty")
	}

	// Copy compose file to project directory
	destPath := filepath.Join(projectComposeDir, "docker-compose.yml")
	if err := s.repoService.gitClient.CopyFile(composePath, destPath); err != nil {
		result.Message = "Failed to copy compose file"
		errMsg := err.Error()
		result.Error = &errMsg
		s.updateSyncStatus(ctx, id, "failed", errMsg)
		return result, err
	}

	// Update sync status
	s.updateSyncStatus(ctx, id, "success", "")

	result.Success = true
	result.Message = fmt.Sprintf("Successfully synced compose file from %s to project %s", sync.ComposePath, project.Name)

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
	defer s.repoService.gitClient.Cleanup(repoPath)

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
