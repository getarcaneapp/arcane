package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	bootstraputils "github.com/getarcaneapp/arcane/backend/internal/utils"
	"github.com/getarcaneapp/arcane/backend/internal/utils/fs"
	"github.com/getarcaneapp/arcane/backend/internal/utils/mapper"
	"github.com/getarcaneapp/arcane/backend/internal/utils/pagination"
	"github.com/getarcaneapp/arcane/types/gitops"
	"gorm.io/gorm"
)

type GitOpsSyncService struct {
	db              *database.DB
	repoService     *GitRepositoryService
	projectService  *ProjectService
	eventService    *EventService
	settingsService *SettingsService
}

const defaultGitSyncTimeout = 5 * time.Minute

// Directory sync limits
const (
	maxSyncFiles      = 500              // Maximum number of files to sync
	maxSyncTotalSize  = 50 * 1024 * 1024 // 50MB total size limit
	maxSyncBinarySize = 10 * 1024 * 1024 // 10MB per binary file limit
)

func NewGitOpsSyncService(db *database.DB, repoService *GitRepositoryService, projectService *ProjectService, eventService *EventService, settingsService *SettingsService) *GitOpsSyncService {
	return &GitOpsSyncService{
		db:              db,
		repoService:     repoService,
		projectService:  projectService,
		eventService:    eventService,
		settingsService: settingsService,
	}
}

func (s *GitOpsSyncService) ListSyncIntervalsRaw(ctx context.Context) ([]bootstraputils.IntervalMigrationItem, error) {
	rows, err := s.db.WithContext(ctx).Raw("SELECT id, sync_interval FROM gitops_syncs").Rows()
	if err != nil {
		return nil, fmt.Errorf("failed to load git sync intervals: %w", err)
	}
	defer rows.Close()

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

func (s *GitOpsSyncService) GetSyncsPaginated(ctx context.Context, environmentID string, params pagination.QueryParams) ([]gitops.GitOpsSync, pagination.Response, error) {
	var syncs []models.GitOpsSync
	q := s.db.WithContext(ctx).Model(&models.GitOpsSync{}).Preload("Repository").Preload("Project").
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

func (s *GitOpsSyncService) CreateSync(ctx context.Context, environmentID string, req gitops.CreateSyncRequest) (*models.GitOpsSync, error) {
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
		SyncDirectory: true, // Default to directory sync
	}

	if req.AutoSync != nil {
		sync.AutoSync = *req.AutoSync
	}
	if req.SyncInterval != nil {
		sync.SyncInterval = *req.SyncInterval
	}
	if req.SyncDirectory != nil {
		sync.SyncDirectory = *req.SyncDirectory
	}

	if err := s.db.WithContext(ctx).Create(&sync).Error; err != nil {
		slog.ErrorContext(ctx, "Failed to create GitOps sync in database", "name", req.Name, "repositoryID", req.RepositoryID, "environmentID", environmentID, "error", err)
		return nil, fmt.Errorf("failed to create sync: %w", err)
	}
	slog.InfoContext(ctx, "GitOps sync created successfully", "syncID", sync.ID, "name", sync.Name)

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
		UserID:       &systemUser.ID,
		Username:     &systemUser.Username,
	})

	if _, err := s.PerformSync(ctx, sync.EnvironmentID, sync.ID); err != nil {
		slog.ErrorContext(ctx, "Failed to perform initial sync after creation", "syncId", sync.ID, "error", err)
		// Don't fail the entire creation - the sync config exists and can be retried
	}

	return s.GetSyncByID(ctx, "", sync.ID)
}

func (s *GitOpsSyncService) UpdateSync(ctx context.Context, environmentID, id string, req gitops.UpdateSyncRequest) (*models.GitOpsSync, error) {
	sync, err := s.GetSyncByID(ctx, environmentID, id)
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
	if req.SyncDirectory != nil {
		updates["sync_directory"] = *req.SyncDirectory
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

	return s.GetSyncByID(ctx, environmentID, id)
}

func (s *GitOpsSyncService) DeleteSync(ctx context.Context, environmentID, id string) error {
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
	resourceType := "git_sync"
	_, _ = s.eventService.CreateEvent(ctx, CreateEventRequest{
		Type:         models.EventTypeGitSyncDelete,
		Severity:     models.EventSeverityInfo,
		Title:        "Git sync deleted",
		Description:  fmt.Sprintf("Deleted git sync configuration '%s'", sync.Name),
		ResourceType: &resourceType,
		ResourceID:   &sync.ID,
		ResourceName: &sync.Name, UserID: &systemUser.ID,
		Username: &systemUser.Username})

	return nil
}

func (s *GitOpsSyncService) PerformSync(ctx context.Context, environmentID, id string) (*gitops.SyncResult, error) {
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
		return result, s.failSync(syncCtx, id, result, sync, "Repository not found", "repository not found")
	}

	authConfig, err := s.repoService.GetAuthConfig(syncCtx, repository)
	if err != nil {
		return result, s.failSync(syncCtx, id, result, sync, "Failed to get authentication config", err.Error())
	}

	// Clone the repository
	repoPath, err := s.repoService.gitClient.Clone(syncCtx, repository.URL, sync.Branch, authConfig)
	if err != nil {
		return result, s.failSync(syncCtx, id, result, sync, "Failed to clone repository", err.Error())
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
		return result, s.failSync(syncCtx, id, result, sync, fmt.Sprintf("Compose file not found at %s", sync.ComposePath), errMsg)
	}

	var project *models.Project
	var syncedFiles []string
	var composeContent string

	if sync.SyncDirectory {
		// Directory sync mode - sync entire directory containing compose file
		slog.InfoContext(syncCtx, "Using directory sync mode", "syncId", id, "composePath", sync.ComposePath)

		// Walk directory once and get all files
		var syncFiles []fs.SyncFile
		var err error
		composeContent, syncFiles, err = s.walkAndParseSyncDirectory(syncCtx, sync, repoPath)
		if err != nil {
			return result, s.failSync(syncCtx, id, result, sync, "Failed to walk directory", err.Error())
		}

		// Get or create project with compose content
		project, err = s.getOrCreateProject(syncCtx, sync, id, composeContent, result)
		if err != nil {
			return result, err
		}

		// Write all directory files to the project
		oldSyncedFiles := parseSyncedFiles(sync.SyncedFiles)
		syncedFiles, err = s.writeSyncFilesToProject(syncCtx, sync, project, syncFiles, oldSyncedFiles)
		if err != nil {
			slog.ErrorContext(syncCtx, "Failed to write directory files to project", "error", err, "syncId", id)
			// Don't fail the sync - the project was created/updated with compose content
			// Fall back to just tracking the file paths
			syncedFiles = make([]string, len(syncFiles))
			for i, f := range syncFiles {
				syncedFiles[i] = f.RelativePath
			}
		}

		// Update sync status with synced files
		s.updateSyncStatusWithFiles(syncCtx, id, "success", "", commitHash, syncedFiles)
	} else {
		// Single file sync mode - existing behavior
		slog.InfoContext(syncCtx, "Using single file sync mode", "syncId", id, "composePath", sync.ComposePath)

		// Read compose file content
		var err error
		composeContent, err = s.repoService.gitClient.ReadFile(syncCtx, repoPath, sync.ComposePath)
		if err != nil {
			return result, s.failSync(syncCtx, id, result, sync, "Failed to read compose file", err.Error())
		}

		// Get or create project
		project, err = s.getOrCreateProject(syncCtx, sync, id, composeContent, result)
		if err != nil {
			return result, err
		}

		// Track single compose file as synced
		syncedFiles = []string{filepath.Base(sync.ComposePath)}

		// Update sync status with synced files
		s.updateSyncStatusWithFiles(syncCtx, id, "success", "", commitHash, syncedFiles)
	}

	result.Success = true
	if sync.SyncDirectory {
		result.Message = fmt.Sprintf("Successfully synced directory with %d files to project %s", len(syncedFiles), project.Name)
	} else {
		result.Message = fmt.Sprintf("Successfully synced compose file from %s to project %s", sync.ComposePath, project.Name)
	}

	// Log success event
	resourceType := "git_sync"
	_, _ = s.eventService.CreateEvent(syncCtx, CreateEventRequest{
		Type:         models.EventTypeGitSyncRun,
		Severity:     models.EventSeveritySuccess,
		Title:        "Git sync completed",
		Description:  fmt.Sprintf("Successfully synced '%s' to project '%s'", sync.Name, project.Name),
		ResourceType: &resourceType,
		ResourceID:   &sync.ID,
		ResourceName: &sync.Name,
		UserID:       &systemUser.ID,
		Username:     &systemUser.Username,
	})

	slog.InfoContext(syncCtx, "GitOps sync completed", "syncId", id, "project", project.Name)

	return result, nil
}

func (s *GitOpsSyncService) updateSyncStatus(ctx context.Context, id, status, errorMsg, commitHash string) {
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
		result, err := s.PerformSync(ctx, sync.EnvironmentID, sync.ID)
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

func (s *GitOpsSyncService) ImportSyncs(ctx context.Context, environmentID string, req []gitops.ImportGitOpsSyncRequest) (*gitops.ImportGitOpsSyncResponse, error) {
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
			Name:          importItem.SyncName,
			RepositoryID:  repo.ID,
			Branch:        importItem.Branch,
			ComposePath:   importItem.DockerComposePath,
			ProjectName:   importItem.SyncName,
			AutoSync:      &importItem.AutoSync,
			SyncInterval:  &importItem.SyncInterval,
			SyncDirectory: importItem.SyncDirectory,
		}

		_, err = s.CreateSync(ctx, environmentID, createReq)
		if err != nil {
			response.FailedCount++
			response.Errors = append(response.Errors, fmt.Sprintf("Stack '%s': %v", importItem.SyncName, err))
		} else {
			response.SuccessCount++
		}
	}

	return response, nil
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
		ResourceName: &sync.Name, UserID: &systemUser.ID,
		Username: &systemUser.Username})
}

func (s *GitOpsSyncService) failSync(ctx context.Context, id string, result *gitops.SyncResult, sync *models.GitOpsSync, message, errMsg string) error {
	result.Message = message
	result.Error = &errMsg
	s.updateSyncStatus(ctx, id, "failed", errMsg, "")
	s.logSyncError(ctx, sync, errMsg)
	return fmt.Errorf("%s", errMsg)
}

func (s *GitOpsSyncService) createProjectForSync(ctx context.Context, sync *models.GitOpsSync, id string, composeContent string, result *gitops.SyncResult) (*models.Project, error) {
	project, err := s.projectService.CreateProject(ctx, sync.ProjectName, composeContent, nil, systemUser)
	if err != nil {
		return nil, s.failSync(ctx, id, result, sync, "Failed to create project", err.Error())
	}

	// Update sync with project ID
	if err := s.db.WithContext(ctx).Model(&models.GitOpsSync{}).Where("id = ?", id).Updates(map[string]interface{}{
		"project_id": project.ID,
	}).Error; err != nil {
		return nil, s.failSync(ctx, id, result, sync, "Failed to update sync with project ID", err.Error())
	}

	// Mark project as GitOps-managed
	if err := s.db.WithContext(ctx).Model(&models.Project{}).Where("id = ?", project.ID).Update("gitops_managed_by", id).Error; err != nil {
		return nil, s.failSync(ctx, id, result, sync, "Failed to mark project as GitOps-managed", err.Error())
	}

	slog.InfoContext(ctx, "Created project for GitOps sync", "projectName", sync.ProjectName, "projectId", project.ID)

	// Deploy the project immediately after creation
	slog.InfoContext(ctx, "Deploying project after initial Git sync", "projectName", project.Name, "projectId", project.ID)
	if err := s.projectService.DeployProject(ctx, project.ID, systemUser); err != nil {
		slog.ErrorContext(ctx, "Failed to deploy project after initial Git sync", "error", err, "projectId", project.ID)
	}

	return project, nil
}

func (s *GitOpsSyncService) getOrCreateProject(ctx context.Context, sync *models.GitOpsSync, id string, composeContent string, result *gitops.SyncResult) (*models.Project, error) {
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
		return s.createProjectForSync(ctx, sync, id, composeContent, result)
	}

	if err := s.updateProjectForSync(ctx, sync, id, project, composeContent, result); err != nil {
		return nil, err
	}
	return project, nil
}

func (s *GitOpsSyncService) updateProjectForSync(ctx context.Context, sync *models.GitOpsSync, id string, project *models.Project, composeContent string, result *gitops.SyncResult) error {
	// Get current content to see if it changed
	oldCompose, _, _ := s.projectService.GetProjectContent(ctx, project.ID)
	contentChanged := oldCompose != composeContent

	// Update existing project's compose file
	_, err := s.projectService.UpdateProject(ctx, project.ID, nil, &composeContent, nil)
	if err != nil {
		return s.failSync(ctx, id, result, sync, "Failed to update project compose file", err.Error())
	}
	slog.InfoContext(ctx, "Updated project compose file", "projectName", project.Name, "projectId", project.ID)

	// If content changed and project is running, redeploy
	if contentChanged {
		details, err := s.projectService.GetProjectDetails(ctx, project.ID)
		if err == nil && (details.Status == string(models.ProjectStatusRunning) || details.Status == string(models.ProjectStatusPartiallyRunning)) {
			slog.InfoContext(ctx, "Redeploying project due to content change from Git sync", "projectName", project.Name, "projectId", project.ID)
			if err := s.projectService.RedeployProject(ctx, project.ID, systemUser); err != nil {
				slog.ErrorContext(ctx, "Failed to redeploy project after Git sync", "error", err, "projectId", project.ID)
			}
		}
	}

	return nil
}

// getProjectsDirectory returns the configured projects directory path
func (s *GitOpsSyncService) getProjectsDirectory(ctx context.Context) (string, error) {
	projectsDirSetting := s.settingsService.GetStringSetting(ctx, "projectsDirectory", "/app/data/projects")
	return fs.GetProjectsDirectory(ctx, strings.TrimSpace(projectsDirSetting))
}

// parseSyncedFiles parses the JSON array of synced file paths from the database
func parseSyncedFiles(syncedFilesJSON *string) []string {
	if syncedFilesJSON == nil || *syncedFilesJSON == "" {
		return nil
	}
	var files []string
	if err := json.Unmarshal([]byte(*syncedFilesJSON), &files); err != nil {
		return nil
	}
	return files
}

// marshalSyncedFiles converts a list of file paths to JSON for storage
func marshalSyncedFiles(files []string) *string {
	if len(files) == 0 {
		return nil
	}
	data, err := json.Marshal(files)
	if err != nil {
		return nil
	}
	result := string(data)
	return &result
}

// walkAndParseSyncDirectory walks the repository directory and returns all files with their contents.
// Returns the compose file content, the list of SyncFile entries, and an error if any.
func (s *GitOpsSyncService) walkAndParseSyncDirectory(ctx context.Context, sync *models.GitOpsSync, repoPath string) (string, []fs.SyncFile, error) {
	slog.InfoContext(ctx, "Starting directory walk", "syncId", sync.ID, "composePath", sync.ComposePath)

	// Walk the directory to get all files
	walkResult, err := s.repoService.gitClient.WalkDirectory(ctx, repoPath, sync.ComposePath, maxSyncFiles, maxSyncTotalSize, maxSyncBinarySize)
	if err != nil {
		return "", nil, fmt.Errorf("failed to walk directory: %w", err)
	}

	slog.InfoContext(ctx, "Directory walk complete",
		"syncId", sync.ID,
		"totalFiles", walkResult.TotalFiles,
		"totalSize", walkResult.TotalSize,
		"skippedBinaries", walkResult.SkippedBinaries)

	// Find the compose file content from the walked files
	composeFileName := filepath.Base(sync.ComposePath)
	var composeContent string

	// Convert walked files to SyncFile format
	syncFiles := make([]fs.SyncFile, len(walkResult.Files))
	for i, f := range walkResult.Files {
		syncFiles[i] = fs.SyncFile{
			RelativePath: f.RelativePath,
			Content:      f.Content,
		}
		if f.RelativePath == composeFileName {
			composeContent = string(f.Content)
		}
	}

	if composeContent == "" {
		return "", nil, fmt.Errorf("compose file %s not found in walked directory", composeFileName)
	}

	return composeContent, syncFiles, nil
}

// writeSyncFilesToProject writes the given sync files to the project directory.
// If oldSyncedFiles is provided, removed files will be cleaned up first.
func (s *GitOpsSyncService) writeSyncFilesToProject(ctx context.Context, sync *models.GitOpsSync, project *models.Project, syncFiles []fs.SyncFile, oldSyncedFiles []string) ([]string, error) {
	projectsDir, err := s.getProjectsDirectory(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get projects directory: %w", err)
	}

	// Build list of new file paths
	newFiles := make([]string, len(syncFiles))
	for i, f := range syncFiles {
		newFiles[i] = f.RelativePath
	}

	// Clean up removed files if we have old sync data
	if len(oldSyncedFiles) > 0 {
		if err := fs.CleanupRemovedFiles(projectsDir, project.Path, oldSyncedFiles, newFiles); err != nil {
			slog.WarnContext(ctx, "Failed to cleanup removed files", "error", err, "syncId", sync.ID)
			// Continue despite cleanup error - it's best effort
		}
	}

	// Write all files to project directory
	writtenPaths, err := fs.WriteSyncedDirectory(projectsDir, project.Path, syncFiles)
	if err != nil {
		return nil, fmt.Errorf("failed to write synced directory: %w", err)
	}

	slog.InfoContext(ctx, "Directory sync written", "syncId", sync.ID, "projectId", project.ID, "filesWritten", len(writtenPaths))
	return writtenPaths, nil
}

// updateSyncStatusWithFiles updates sync status including the list of synced files
func (s *GitOpsSyncService) updateSyncStatusWithFiles(ctx context.Context, id, status, errorMsg, commitHash string, syncedFiles []string) {
	now := time.Now()
	updates := map[string]interface{}{
		"last_sync_at":     now,
		"last_sync_status": status,
		"synced_files":     marshalSyncedFiles(syncedFiles),
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
		slog.ErrorContext(ctx, "Failed to update sync status with files", "error", err, "syncId", id)
	}
}
