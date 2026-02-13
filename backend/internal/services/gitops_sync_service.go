package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	bootstraputils "github.com/getarcaneapp/arcane/backend/internal/utils"
	"github.com/getarcaneapp/arcane/backend/internal/utils/mapper"
	"github.com/getarcaneapp/arcane/backend/internal/utils/pagination"
	"github.com/getarcaneapp/arcane/types/gitops"
	"gorm.io/gorm"
)

type GitOpsSyncService struct {
	db             *database.DB
	repoService    *GitRepositoryService
	projectService *ProjectService
	eventService   *EventService
}

const defaultGitSyncTimeout = 5 * time.Minute

func NewGitOpsSyncService(db *database.DB, repoService *GitRepositoryService, projectService *ProjectService, eventService *EventService) *GitOpsSyncService {
	return &GitOpsSyncService{
		db:             db,
		repoService:    repoService,
		projectService: projectService,
		eventService:   eventService,
	}
}

func (s *GitOpsSyncService) ListSyncIntervalsRaw(ctx context.Context) ([]bootstraputils.IntervalMigrationItem, error) {
	rows, err := s.db.WithContext(ctx).Raw("SELECT id, sync_interval FROM gitops_syncs").Rows()
	if err != nil {
		return nil, fmt.Errorf("failed to load git sync intervals: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]bootstraputils.IntervalMigrationItem, 0)
	for rows.Next() {
		var id string
		var raw any
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, fmt.Errorf("failed to scan git sync interval: %w", err)
		}
		items = append(items, bootstraputils.IntervalMigrationItem{
			ID:       id,
			RawValue: strings.TrimSpace(fmt.Sprint(raw)),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read git sync intervals: %w", err)
	}

	return items, nil
}

func (s *GitOpsSyncService) UpdateSyncIntervalMinutes(ctx context.Context, id string, minutes int) error {
	if minutes <= 0 {
		return fmt.Errorf("sync interval must be positive")
	}
	return s.db.WithContext(ctx).
		Model(&models.GitOpsSync{}).
		Where("id = ?", id).
		Update("sync_interval", minutes).Error
}

func (s *GitOpsSyncService) GetSyncsPaginated(ctx context.Context, environmentID string, params pagination.QueryParams) ([]gitops.GitOpsSync, pagination.Response, gitops.SyncCounts, error) {
	var syncs []models.GitOpsSync
	q := s.db.WithContext(ctx).Model(&models.GitOpsSync{}).
		Where("environment_id = ?", environmentID)

	if term := strings.TrimSpace(params.Search); term != "" {
		searchPattern := "%" + term + "%"
		q = q.Where(
			"name LIKE ? OR branch LIKE ? OR compose_path LIKE ?",
			searchPattern, searchPattern, searchPattern,
		)
	}

	q = pagination.ApplyBooleanFilter(q, "auto_sync", params.Filters["autoSync"])

	q = pagination.ApplyFilter(q, "repository_id", params.Filters["repositoryId"])
	q = pagination.ApplyFilter(q, "project_id", params.Filters["projectId"])

	counts, err := s.getFilteredSyncCounts(q)
	if err != nil {
		return nil, pagination.Response{}, gitops.SyncCounts{}, fmt.Errorf("failed to get sync counts: %w", err)
	}

	paginationResp, err := pagination.PaginateAndSortDB(params, q.Preload("Repository").Preload("Project"), &syncs)
	if err != nil {
		return nil, pagination.Response{}, gitops.SyncCounts{}, fmt.Errorf("failed to paginate gitops syncs: %w", err)
	}

	out, mapErr := mapper.MapSlice[models.GitOpsSync, gitops.GitOpsSync](syncs)
	if mapErr != nil {
		return nil, pagination.Response{}, gitops.SyncCounts{}, fmt.Errorf("failed to map syncs: %w", mapErr)
	}

	return out, paginationResp, counts, nil
}

func (s *GitOpsSyncService) getFilteredSyncCounts(query *gorm.DB) (gitops.SyncCounts, error) {
	var totalSyncs int64
	if err := query.Session(&gorm.Session{}).Count(&totalSyncs).Error; err != nil {
		return gitops.SyncCounts{}, err
	}

	var activeSyncs int64
	if err := query.Session(&gorm.Session{}).Where("auto_sync = ?", true).Count(&activeSyncs).Error; err != nil {
		return gitops.SyncCounts{}, err
	}

	var successfulSyncs int64
	if err := query.Session(&gorm.Session{}).Where("last_sync_status = ?", "success").Count(&successfulSyncs).Error; err != nil {
		return gitops.SyncCounts{}, err
	}

	return gitops.SyncCounts{
		TotalSyncs:      int(totalSyncs),
		ActiveSyncs:     int(activeSyncs),
		SuccessfulSyncs: int(successfulSyncs),
	}, nil
}

func (s *GitOpsSyncService) GetSyncByID(ctx context.Context, environmentID, id string) (*models.GitOpsSync, error) {
	var sync models.GitOpsSync
	q := s.db.WithContext(ctx).Preload("Repository").Preload("Project").Where("id = ?", id)
	if environmentID != "" {
		q = q.Where("environment_id = ?", environmentID)
	}
	if err := q.First(&sync).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			slog.WarnContext(ctx, "GitOps sync not found", "syncID", id, "environmentID", environmentID)
			return nil, fmt.Errorf("sync not found")
		}
		slog.ErrorContext(ctx, "Failed to get GitOps sync", "syncID", id, "environmentID", environmentID, "error", err)
		return nil, fmt.Errorf("failed to get sync: %w", err)
	}
	return &sync, nil
}

func (s *GitOpsSyncService) CreateSync(ctx context.Context, environmentID string, req gitops.CreateSyncRequest, actor models.User) (*models.GitOpsSync, error) {
	slog.InfoContext(ctx, "Creating GitOps sync", "environmentID", environmentID, "name", req.Name, "repositoryID", req.RepositoryID)

	// Validate repository exists
	repo, err := s.repoService.GetRepositoryByID(ctx, req.RepositoryID)
	if err != nil {
		slog.ErrorContext(ctx, "Repository not found for GitOps sync", "repositoryID", req.RepositoryID, "error", err)
		return nil, fmt.Errorf("repository not found: %w", err)
	}
	slog.InfoContext(ctx, "Found repository for GitOps sync", "repositoryID", req.RepositoryID, "repositoryName", repo.Name)

	// Store the project name - use sync name if project name not provided
	projectName := req.ProjectName
	if projectName == "" {
		projectName = req.Name
	}

	sync := models.GitOpsSync{
		Name:          req.Name,
		EnvironmentID: environmentID,
		RepositoryID:  req.RepositoryID,
		Branch:        req.Branch,
		ComposePath:   req.ComposePath,
		ProjectName:   projectName,
		ProjectID:     nil, // Will be set during first sync
		AutoSync:      false,
		SyncInterval:  60,
	}

	if req.AutoSync != nil {
		sync.AutoSync = *req.AutoSync
	}
	if req.SyncInterval != nil {
		sync.SyncInterval = *req.SyncInterval
	}

	if err := s.db.WithContext(ctx).Create(&sync).Error; err != nil {
		slog.ErrorContext(ctx, "Failed to create GitOps sync in database", "name", req.Name, "repositoryID", req.RepositoryID, "environmentID", environmentID, "error", err)
		return nil, fmt.Errorf("failed to create sync: %w", err)
	}
	slog.InfoContext(ctx, "GitOps sync created successfully", "syncID", sync.ID, "name", sync.Name)

	// Log event
	_, _ = s.eventService.CreateEvent(ctx, CreateEventRequest{
		Type:          models.EventTypeGitSyncCreate,
		Severity:      models.EventSeveritySuccess,
		Title:         "Git sync created",
		Description:   fmt.Sprintf("Created git sync configuration '%s'", sync.Name),
		ResourceType:  new("git_sync"),
		ResourceID:    new(sync.ID),
		ResourceName:  new(sync.Name),
		UserID:        new(actor.ID),
		Username:      new(actor.Username),
		EnvironmentID: new(sync.EnvironmentID),
	})

	if _, err := s.PerformSync(ctx, sync.EnvironmentID, sync.ID, actor); err != nil {
		slog.ErrorContext(ctx, "Failed to perform initial sync after creation", "syncId", sync.ID, "error", err)
		// Don't fail the entire creation - the sync config exists and can be retried
	}

	return s.GetSyncByID(ctx, "", sync.ID)
}

func (s *GitOpsSyncService) UpdateSync(ctx context.Context, environmentID, id string, req gitops.UpdateSyncRequest, actor models.User) (*models.GitOpsSync, error) {
	sync, err := s.GetSyncByID(ctx, environmentID, id)
	if err != nil {
		return nil, err
	}

	updates := make(map[string]any)

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

	if len(updates) > 0 {
		if err := s.db.WithContext(ctx).Model(sync).Updates(updates).Error; err != nil {
			return nil, fmt.Errorf("failed to update sync: %w", err)
		}

		// Log event
		_, _ = s.eventService.CreateEvent(ctx, CreateEventRequest{
			Type:          models.EventTypeGitSyncUpdate,
			Severity:      models.EventSeveritySuccess,
			Title:         "Git sync updated",
			Description:   fmt.Sprintf("Updated git sync configuration '%s'", sync.Name),
			ResourceType:  new("git_sync"),
			ResourceID:    new(sync.ID),
			ResourceName:  new(sync.Name),
			UserID:        new(actor.ID),
			Username:      new(actor.Username),
			EnvironmentID: new(sync.EnvironmentID),
		})
	}

	return s.GetSyncByID(ctx, environmentID, id)
}

func (s *GitOpsSyncService) DeleteSync(ctx context.Context, environmentID, id string, actor models.User) error {
	// Get sync info before deleting
	sync, err := s.GetSyncByID(ctx, environmentID, id)
	if err != nil {
		return err
	}

	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Clear gitops_managed_by for the associated project, if any.
		if sync.ProjectID != nil && *sync.ProjectID != "" {
			if err := tx.Model(&models.Project{}).
				Where("id = ? AND gitops_managed_by = ?", *sync.ProjectID, id).
				Update("gitops_managed_by", nil).Error; err != nil {
				return fmt.Errorf("failed to clear gitops_managed_by: %w", err)
			}
		}

		if err := tx.Where("id = ?", id).Delete(&models.GitOpsSync{}).Error; err != nil {
			return fmt.Errorf("failed to delete sync: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}

	// Log event
	_, _ = s.eventService.CreateEvent(ctx, CreateEventRequest{
		Type:          models.EventTypeGitSyncDelete,
		Severity:      models.EventSeverityInfo,
		Title:         "Git sync deleted",
		Description:   fmt.Sprintf("Deleted git sync configuration '%s'", sync.Name),
		ResourceType:  new("git_sync"),
		ResourceID:    new(sync.ID),
		ResourceName:  new(sync.Name),
		UserID:        new(actor.ID),
		Username:      new(actor.Username),
		EnvironmentID: new(sync.EnvironmentID),
	})

	return nil
}

func (s *GitOpsSyncService) PerformSync(ctx context.Context, environmentID, id string, actor models.User) (*gitops.SyncResult, error) {
	syncCtx, cancel := context.WithTimeout(ctx, defaultGitSyncTimeout)
	defer cancel()

	sync, err := s.GetSyncByID(syncCtx, environmentID, id)
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
		return result, s.failSync(syncCtx, id, result, sync, actor, "Repository not found", "repository not found")
	}

	authConfig, err := s.repoService.GetAuthConfig(syncCtx, repository)
	if err != nil {
		return result, s.failSync(syncCtx, id, result, sync, actor, "Failed to get authentication config", err.Error())
	}

	// Clone the repository
	repoPath, err := s.repoService.gitClient.Clone(syncCtx, repository.URL, sync.Branch, authConfig)
	if err != nil {
		return result, s.failSync(syncCtx, id, result, sync, actor, "Failed to clone repository", err.Error())
	}
	defer func() {
		if cleanupErr := s.repoService.gitClient.Cleanup(repoPath); cleanupErr != nil {
			slog.WarnContext(syncCtx, "Failed to cleanup repository", "path", repoPath, "error", cleanupErr)
		}
	}()

	// Get the current commit hash
	commitHash, err := s.repoService.gitClient.GetCurrentCommit(syncCtx, repoPath)
	if err != nil {
		slog.WarnContext(syncCtx, "Failed to get commit hash", "error", err)
		commitHash = ""
	}

	// Check if compose file exists
	if !s.repoService.gitClient.FileExists(syncCtx, repoPath, sync.ComposePath) {
		errMsg := fmt.Sprintf("compose file not found: %s", sync.ComposePath)
		return result, s.failSync(syncCtx, id, result, sync, actor, fmt.Sprintf("Compose file not found at %s", sync.ComposePath), errMsg)
	}

	// Read compose file content
	composeContent, err := s.repoService.gitClient.ReadFile(syncCtx, repoPath, sync.ComposePath)
	if err != nil {
		return result, s.failSync(syncCtx, id, result, sync, actor, "Failed to read compose file", err.Error())
	}

	// Try to read .env file from the same directory as the compose file
	var envContent *string
	envPath := filepath.Join(filepath.Dir(sync.ComposePath), ".env")
	if s.repoService.gitClient.FileExists(syncCtx, repoPath, envPath) {
		content, err := s.repoService.gitClient.ReadFile(syncCtx, repoPath, envPath)
		if err != nil {
			slog.WarnContext(syncCtx, "Failed to read .env file", "path", envPath, "error", err)
		} else {
			envContent = &content
		}
	}

	// Get or create project
	project, err := s.getOrCreateProjectInternal(syncCtx, sync, id, composeContent, envContent, result, actor)
	if err != nil {
		return result, err
	}

	// Update sync status
	s.updateSyncStatus(syncCtx, id, "success", "", commitHash)

	result.Success = true
	result.Message = fmt.Sprintf("Successfully synced compose file from %s to project %s", sync.ComposePath, project.Name)

	// Log success event
	_, _ = s.eventService.CreateEvent(syncCtx, CreateEventRequest{
		Type:          models.EventTypeGitSyncRun,
		Severity:      models.EventSeveritySuccess,
		Title:         "Git sync completed",
		Description:   fmt.Sprintf("Successfully synced '%s' to project '%s'", sync.Name, project.Name),
		ResourceType:  new("git_sync"),
		ResourceID:    new(sync.ID),
		ResourceName:  new(sync.Name),
		UserID:        new(actor.ID),
		Username:      new(actor.Username),
		EnvironmentID: new(sync.EnvironmentID),
	})

	slog.InfoContext(syncCtx, "GitOps sync completed", "syncId", id, "project", project.Name)

	return result, nil
}

func (s *GitOpsSyncService) updateSyncStatus(ctx context.Context, id, status, errorMsg, commitHash string) {
	now := time.Now()
	updates := map[string]any{
		"last_sync_at":     now,
		"last_sync_status": status,
	}

	if errorMsg != "" {
		updates["last_sync_error"] = errorMsg
	} else {
		updates["last_sync_error"] = nil
	}

	if commitHash != "" {
		updates["last_sync_commit"] = commitHash
	}

	if err := s.db.WithContext(ctx).Model(&models.GitOpsSync{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		slog.ErrorContext(ctx, "Failed to update sync status", "error", err, "syncId", id)
	}
}

func (s *GitOpsSyncService) GetSyncStatus(ctx context.Context, environmentID, id string) (*gitops.SyncStatus, error) {
	sync, err := s.GetSyncByID(ctx, environmentID, id)
	if err != nil {
		return nil, err
	}

	status := &gitops.SyncStatus{
		ID:             sync.ID,
		AutoSync:       sync.AutoSync,
		LastSyncAt:     sync.LastSyncAt,
		LastSyncStatus: sync.LastSyncStatus,
		LastSyncError:  sync.LastSyncError,
		LastSyncCommit: sync.LastSyncCommit,
	}

	// Calculate next sync time
	if sync.AutoSync && sync.LastSyncAt != nil {
		status.NextSyncAt = new(sync.LastSyncAt.Add(time.Duration(sync.SyncInterval) * time.Minute))
	}

	return status, nil
}

func (s *GitOpsSyncService) SyncAllEnabled(ctx context.Context) error {
	var syncs []models.GitOpsSync
	if err := s.db.WithContext(ctx).
		Preload("Repository").
		Preload("Project").
		Where("auto_sync = ?", true).
		Find(&syncs).Error; err != nil {
		return fmt.Errorf("failed to get auto-sync enabled syncs: %w", err)
	}

	for _, sync := range syncs {
		// Check if sync is due
		if sync.LastSyncAt != nil {
			nextSync := sync.LastSyncAt.Add(time.Duration(sync.SyncInterval) * time.Minute)
			// Use a 30-second buffer to account for execution time drift
			if time.Now().Add(30 * time.Second).Before(nextSync) {
				continue
			}
		}

		// Perform sync
		result, err := s.PerformSync(ctx, sync.EnvironmentID, sync.ID, systemUser)
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

func (s *GitOpsSyncService) BrowseFiles(ctx context.Context, environmentID, id string, path string) (*gitops.BrowseResponse, error) {
	browseCtx, cancel := context.WithTimeout(ctx, defaultGitSyncTimeout)
	defer cancel()

	sync, err := s.GetSyncByID(browseCtx, environmentID, id)
	if err != nil {
		return nil, err
	}

	repository := sync.Repository
	if repository == nil {
		return nil, fmt.Errorf("repository not found")
	}

	authConfig, err := s.repoService.GetAuthConfig(browseCtx, repository)
	if err != nil {
		return nil, err
	}

	// Clone the repository
	repoPath, err := s.repoService.gitClient.Clone(browseCtx, repository.URL, sync.Branch, authConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to clone repository: %w", err)
	}
	defer func() {
		if cleanupErr := s.repoService.gitClient.Cleanup(repoPath); cleanupErr != nil {
			slog.WarnContext(browseCtx, "Failed to cleanup repository", "path", repoPath, "error", cleanupErr)
		}
	}()

	// Browse the tree
	files, err := s.repoService.gitClient.BrowseTree(browseCtx, repoPath, path)
	if err != nil {
		return nil, err
	}

	return &gitops.BrowseResponse{
		Path:  path,
		Files: files,
	}, nil
}

func (s *GitOpsSyncService) ImportSyncs(ctx context.Context, environmentID string, req []gitops.ImportGitOpsSyncRequest, actor models.User) (*gitops.ImportGitOpsSyncResponse, error) {
	response := &gitops.ImportGitOpsSyncResponse{
		SuccessCount: 0,
		FailedCount:  0,
		Errors:       []string{},
	}

	for _, importItem := range req {
		// Find repository by name
		repo, err := s.repoService.GetRepositoryByName(ctx, importItem.GitRepo)
		if err != nil {
			response.FailedCount++
			response.Errors = append(response.Errors, fmt.Sprintf("Stack '%s': Repository '%s' not found (%v)", importItem.SyncName, importItem.GitRepo, err))
			continue
		}

		createReq := gitops.CreateSyncRequest{
			Name:         importItem.SyncName,
			RepositoryID: repo.ID,
			Branch:       importItem.Branch,
			ComposePath:  importItem.DockerComposePath,
			ProjectName:  importItem.SyncName,
			AutoSync:     new(importItem.AutoSync),
			SyncInterval: new(importItem.SyncInterval),
		}

		_, err = s.CreateSync(ctx, environmentID, createReq, actor)
		if err != nil {
			response.FailedCount++
			response.Errors = append(response.Errors, fmt.Sprintf("Stack '%s': %v", importItem.SyncName, err))
		} else {
			response.SuccessCount++
		}
	}

	return response, nil
}

func (s *GitOpsSyncService) logSyncError(ctx context.Context, sync *models.GitOpsSync, actor models.User, errorMsg string) {
	_, _ = s.eventService.CreateEvent(ctx, CreateEventRequest{
		Type:          models.EventTypeGitSyncError,
		Severity:      models.EventSeverityError,
		Title:         "Git sync failed",
		Description:   fmt.Sprintf("Failed to sync '%s': %s", sync.Name, errorMsg),
		ResourceType:  new("git_sync"),
		ResourceID:    new(sync.ID),
		ResourceName:  new(sync.Name),
		UserID:        new(actor.ID),
		Username:      new(actor.Username),
		EnvironmentID: new(sync.EnvironmentID),
	})
}

func (s *GitOpsSyncService) failSync(ctx context.Context, id string, result *gitops.SyncResult, sync *models.GitOpsSync, actor models.User, message, errMsg string) error {
	result.Message = message
	result.Error = new(errMsg)
	s.updateSyncStatus(ctx, id, "failed", errMsg, "")
	s.logSyncError(ctx, sync, actor, errMsg)
	return fmt.Errorf("%s", errMsg)
}

func (s *GitOpsSyncService) createProjectForSyncInternal(ctx context.Context, sync *models.GitOpsSync, id string, composeContent string, envContent *string, result *gitops.SyncResult, actor models.User) (*models.Project, error) {
	project, err := s.projectService.CreateProject(ctx, sync.ProjectName, composeContent, envContent, actor)
	if err != nil {
		return nil, s.failSync(ctx, id, result, sync, actor, "Failed to create project", err.Error())
	}

	// Update sync with project ID
	if err := s.db.WithContext(ctx).Model(&models.GitOpsSync{}).Where("id = ?", id).Updates(map[string]any{
		"project_id": project.ID,
	}).Error; err != nil {
		return nil, s.failSync(ctx, id, result, sync, actor, "Failed to update sync with project ID", err.Error())
	}

	// Mark project as GitOps-managed
	if err := s.db.WithContext(ctx).Model(&models.Project{}).Where("id = ?", project.ID).Update("gitops_managed_by", id).Error; err != nil {
		return nil, s.failSync(ctx, id, result, sync, actor, "Failed to mark project as GitOps-managed", err.Error())
	}

	slog.InfoContext(ctx, "Created project for GitOps sync", "projectName", sync.ProjectName, "projectId", project.ID)

	// Deploy the project immediately after creation
	slog.InfoContext(ctx, "Deploying project after initial Git sync", "projectName", project.Name, "projectId", project.ID)
	if err := s.projectService.DeployProject(ctx, project.ID, actor); err != nil {
		slog.ErrorContext(ctx, "Failed to deploy project after initial Git sync", "error", err, "projectId", project.ID)
	}

	return project, nil
}

func (s *GitOpsSyncService) getOrCreateProjectInternal(ctx context.Context, sync *models.GitOpsSync, id string, composeContent string, envContent *string, result *gitops.SyncResult, actor models.User) (*models.Project, error) {
	var project *models.Project
	var err error

	if sync.ProjectID != nil && *sync.ProjectID != "" {
		project, err = s.projectService.GetProjectFromDatabaseByID(ctx, *sync.ProjectID)
		if err != nil {
			slog.WarnContext(ctx, "Existing project not found, will create new one", "projectId", *sync.ProjectID, "error", err)
			project = nil
		}
	}

	if project == nil {
		return s.createProjectForSyncInternal(ctx, sync, id, composeContent, envContent, result, actor)
	}

	if err := s.updateProjectForSyncInternal(ctx, sync, id, project, composeContent, envContent, result, actor); err != nil {
		return nil, err
	}
	return project, nil
}

func (s *GitOpsSyncService) updateProjectForSyncInternal(ctx context.Context, sync *models.GitOpsSync, id string, project *models.Project, composeContent string, envContent *string, result *gitops.SyncResult, actor models.User) error {
	// Get current content to see if it changed
	oldCompose, oldEnv, _ := s.projectService.GetProjectContent(ctx, project.ID)
	contentChanged := oldCompose != composeContent
	if envContent != nil {
		if oldEnv != *envContent {
			contentChanged = true
		}
	} else if oldEnv != "" {
		contentChanged = true
	}

	// Update existing project's compose and env files
	_, err := s.projectService.UpdateProject(ctx, project.ID, nil, &composeContent, envContent, actor)
	if err != nil {
		return s.failSync(ctx, id, result, sync, actor, "Failed to update project files", err.Error())
	}
	slog.InfoContext(ctx, "Updated project files", "projectName", project.Name, "projectId", project.ID)

	// If content changed and project is running, redeploy
	if contentChanged {
		details, err := s.projectService.GetProjectDetails(ctx, project.ID)
		if err == nil && (details.Status == string(models.ProjectStatusRunning) || details.Status == string(models.ProjectStatusPartiallyRunning)) {
			slog.InfoContext(ctx, "Redeploying project due to content change from Git sync", "projectName", project.Name, "projectId", project.ID)
			if err := s.projectService.RedeployProject(ctx, project.ID, actor); err != nil {
				slog.ErrorContext(ctx, "Failed to redeploy project after Git sync", "error", err, "projectId", project.ID)
			}
		}
	}

	return nil
}
