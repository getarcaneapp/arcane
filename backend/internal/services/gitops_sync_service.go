package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/pkg/libarcane/startup"
	"github.com/getarcaneapp/arcane/backend/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/pkg/utils/mapper"
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

const (
	defaultMaxSyncFiles        = 500
	defaultMaxSyncTotalSizeMB  = 50
	defaultMaxSyncBinarySizeMB = 10
	defaultMaxSyncTotalSize    = defaultMaxSyncTotalSizeMB * 1024 * 1024
	defaultMaxSyncBinarySize   = defaultMaxSyncBinarySizeMB * 1024 * 1024
)

type scheduledGitOpsSync struct {
	ID            string
	EnvironmentID string
	SyncInterval  int
	LastSyncAt    *time.Time
}

// preparedSyncSource captures the repository data needed by the sync execution
// paths after the source repository has been cloned and validated.
type preparedSyncSource struct {
	repoPath       string
	commitHash     string
	composeContent string
	envContent     *string
}

// stagedDirectorySync holds the fully prepared directory-sync result before it
// is promoted into the live project path.
type stagedDirectorySync struct {
	stagePath       string
	composeFileName string
	project         *models.Project
	syncedFiles     []string
	serviceCount    int
	contentsChanged bool
}

func validateSyncLimits(maxFiles *int, maxTotalSize, maxBinarySize *int64) error {
	if maxFiles != nil && *maxFiles < 0 {
		return fmt.Errorf("maxSyncFiles must be non-negative")
	}
	if maxTotalSize != nil && *maxTotalSize < 0 {
		return fmt.Errorf("maxSyncTotalSize must be non-negative")
	}
	if maxBinarySize != nil && *maxBinarySize < 0 {
		return fmt.Errorf("maxSyncBinarySize must be non-negative")
	}
	return nil
}

func normalizeSyncLimitSetting(value, defaultValue int) int {
	if value < 0 {
		return defaultValue
	}
	return value
}

func megabytesToBytes(value int) int64 {
	return int64(value) * 1024 * 1024
}

func effectiveIntLimit(syncLimit, environmentLimit int) int {
	switch {
	case syncLimit == 0:
		return environmentLimit
	case environmentLimit == 0:
		return syncLimit
	case syncLimit < environmentLimit:
		return syncLimit
	default:
		return environmentLimit
	}
}

func effectiveInt64Limit(syncLimit, environmentLimit int64) int64 {
	switch {
	case syncLimit == 0:
		return environmentLimit
	case environmentLimit == 0:
		return syncLimit
	case syncLimit < environmentLimit:
		return syncLimit
	default:
		return environmentLimit
	}
}

func NewGitOpsSyncService(db *database.DB, repoService *GitRepositoryService, projectService *ProjectService, eventService *EventService, settingsService *SettingsService) *GitOpsSyncService {
	return &GitOpsSyncService{
		db:              db,
		repoService:     repoService,
		projectService:  projectService,
		eventService:    eventService,
		settingsService: settingsService,
	}
}

func (s *GitOpsSyncService) getEnvironmentSyncLimits(ctx context.Context) (int, int64, int64) {
	if s.settingsService == nil {
		return defaultMaxSyncFiles, defaultMaxSyncTotalSize, defaultMaxSyncBinarySize
	}

	maxFiles := normalizeSyncLimitSetting(s.settingsService.GetIntSetting(ctx, "gitSyncMaxFiles", defaultMaxSyncFiles), defaultMaxSyncFiles)
	maxTotalSizeMB := normalizeSyncLimitSetting(
		s.settingsService.GetIntSetting(ctx, "gitSyncMaxTotalSizeMb", defaultMaxSyncTotalSizeMB),
		defaultMaxSyncTotalSizeMB,
	)
	maxBinarySizeMB := normalizeSyncLimitSetting(
		s.settingsService.GetIntSetting(ctx, "gitSyncMaxBinarySizeMb", defaultMaxSyncBinarySizeMB),
		defaultMaxSyncBinarySizeMB,
	)

	return maxFiles, megabytesToBytes(maxTotalSizeMB), megabytesToBytes(maxBinarySizeMB)
}

func (s *GitOpsSyncService) getEffectiveSyncLimits(ctx context.Context, sync *models.GitOpsSync) (int, int64, int64) {
	environmentMaxFiles, environmentMaxTotalSize, environmentMaxBinarySize := s.getEnvironmentSyncLimits(ctx)

	return effectiveIntLimit(sync.MaxSyncFiles, environmentMaxFiles),
		effectiveInt64Limit(sync.MaxSyncTotalSize, environmentMaxTotalSize),
		effectiveInt64Limit(sync.MaxSyncBinarySize, environmentMaxBinarySize)
}

func (s *GitOpsSyncService) ListSyncIntervalsRaw(ctx context.Context) ([]startup.IntervalMigrationItem, error) {
	rows, err := s.db.WithContext(ctx).Raw("SELECT id, sync_interval FROM gitops_syncs").Rows()
	if err != nil {
		return nil, fmt.Errorf("failed to load git sync intervals: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]startup.IntervalMigrationItem, 0)
	for rows.Next() {
		var id string
		var raw any
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, fmt.Errorf("failed to scan git sync interval: %w", err)
		}
		items = append(items, startup.IntervalMigrationItem{
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

	defaultMaxFiles, defaultMaxTotalSize, defaultMaxBinarySize := s.getEnvironmentSyncLimits(ctx)

	sync := models.GitOpsSync{
		Name:              req.Name,
		EnvironmentID:     environmentID,
		RepositoryID:      req.RepositoryID,
		Branch:            req.Branch,
		ComposePath:       req.ComposePath,
		ProjectName:       projectName,
		ProjectID:         nil, // Will be set during first sync
		AutoSync:          false,
		SyncInterval:      60,
		SyncDirectory:     false, // Default to single-file sync
		MaxSyncFiles:      defaultMaxFiles,
		MaxSyncTotalSize:  defaultMaxTotalSize,
		MaxSyncBinarySize: defaultMaxBinarySize,
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
	if err := validateSyncLimits(req.MaxSyncFiles, req.MaxSyncTotalSize, req.MaxSyncBinarySize); err != nil {
		return nil, err
	}
	if req.MaxSyncFiles != nil {
		sync.MaxSyncFiles = *req.MaxSyncFiles
	}
	if req.MaxSyncTotalSize != nil {
		sync.MaxSyncTotalSize = *req.MaxSyncTotalSize
	}
	if req.MaxSyncBinarySize != nil {
		sync.MaxSyncBinarySize = *req.MaxSyncBinarySize
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
	if req.SyncDirectory != nil {
		updates["sync_directory"] = *req.SyncDirectory
	}
	if err := validateSyncLimits(req.MaxSyncFiles, req.MaxSyncTotalSize, req.MaxSyncBinarySize); err != nil {
		return nil, err
	}
	if req.MaxSyncFiles != nil {
		updates["max_sync_files"] = *req.MaxSyncFiles
	}
	if req.MaxSyncTotalSize != nil {
		updates["max_sync_total_size"] = *req.MaxSyncTotalSize
	}
	if req.MaxSyncBinarySize != nil {
		updates["max_sync_binary_size"] = *req.MaxSyncBinarySize
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

	source, err := s.prepareSyncSource(syncCtx, sync, result, actor)
	if source != nil && source.repoPath != "" {
		defer func() {
			if cleanupErr := s.repoService.gitClient.Cleanup(source.repoPath); cleanupErr != nil {
				slog.WarnContext(syncCtx, "Failed to cleanup repository", "path", source.repoPath, "error", cleanupErr)
			}
		}()
	}
	if err != nil {
		return result, err
	}

	if sync.SyncDirectory {
		return s.performDirectorySync(syncCtx, sync, id, actor, result, source)
	}

	return s.performSingleFileSync(syncCtx, sync, id, actor, result, source)
}

// prepareSyncSource clones the source repository, validates that the configured
// compose file exists, and reads the compose/env inputs for the sync flow.
func (s *GitOpsSyncService) prepareSyncSource(ctx context.Context, sync *models.GitOpsSync, result *gitops.SyncResult, actor models.User) (*preparedSyncSource, error) {
	repository := sync.Repository
	if repository == nil {
		return nil, s.failSync(ctx, sync.ID, result, sync, actor, "Repository not found", "repository not found")
	}

	authConfig, err := s.repoService.GetAuthConfig(ctx, repository)
	if err != nil {
		return nil, s.failSync(ctx, sync.ID, result, sync, actor, "Failed to get authentication config", err.Error())
	}

	repoPath, err := s.repoService.gitClient.Clone(ctx, repository.URL, sync.Branch, authConfig)
	if err != nil {
		return nil, s.failSync(ctx, sync.ID, result, sync, actor, "Failed to clone repository", err.Error())
	}

	commitHash, err := s.repoService.gitClient.GetCurrentCommit(ctx, repoPath)
	if err != nil {
		slog.WarnContext(ctx, "Failed to get commit hash", "error", err)
		commitHash = ""
	}

	if !s.repoService.gitClient.FileExists(ctx, repoPath, sync.ComposePath) {
		errMsg := fmt.Sprintf("compose file not found: %s", sync.ComposePath)
		return &preparedSyncSource{repoPath: repoPath, commitHash: commitHash}, s.failSync(ctx, sync.ID, result, sync, actor, fmt.Sprintf("Compose file not found at %s", sync.ComposePath), errMsg)
	}

	composeContent, err := s.repoService.gitClient.ReadFile(ctx, repoPath, sync.ComposePath)
	if err != nil {
		return &preparedSyncSource{repoPath: repoPath, commitHash: commitHash}, s.failSync(ctx, sync.ID, result, sync, actor, "Failed to read compose file", err.Error())
	}

	source := &preparedSyncSource{
		repoPath:       repoPath,
		commitHash:     commitHash,
		composeContent: composeContent,
	}

	envPath := filepath.Join(filepath.Dir(sync.ComposePath), ".env")
	if s.repoService.gitClient.FileExists(ctx, repoPath, envPath) {
		content, err := s.repoService.gitClient.ReadFile(ctx, repoPath, envPath)
		if err != nil {
			slog.WarnContext(ctx, "Failed to read .env file", "path", envPath, "error", err)
		} else {
			source.envContent = &content
		}
	}

	return source, nil
}

// performDirectorySync runs the directory-sync path and only triggers a
// redeploy when an already running project's synced contents changed.
func (s *GitOpsSyncService) performDirectorySync(ctx context.Context, sync *models.GitOpsSync, id string, actor models.User, result *gitops.SyncResult, source *preparedSyncSource) (*gitops.SyncResult, error) {
	slog.InfoContext(ctx, "Using directory sync mode", "syncId", id, "composePath", sync.ComposePath)

	_, syncFiles, err := s.walkAndParseSyncDirectory(ctx, sync, source.repoPath)
	if err != nil {
		return result, s.failSync(ctx, id, result, sync, actor, "Failed to walk directory", err.Error())
	}

	project, syncedFiles, _, contentsChanged, err := s.syncProjectDirectoryInternal(ctx, sync, syncFiles, actor)
	if err != nil {
		return result, s.failSync(ctx, id, result, sync, actor, "Failed to sync project directory", err.Error())
	}

	if contentsChanged {
		s.redeployIfRunningAfterSync(ctx, project, actor, "directory")
	}

	s.updateSyncStatusWithFiles(ctx, id, "success", "", source.commitHash, syncedFiles)
	result.Success = true
	result.Message = fmt.Sprintf("Successfully synced directory with %d files to project %s", len(syncedFiles), project.Name)
	s.logSyncSuccess(ctx, sync, project, actor)
	slog.InfoContext(ctx, "GitOps sync completed", "syncId", id, "project", project.Name)

	return result, nil
}

// performSingleFileSync preserves the legacy compose-only Git sync behavior.
func (s *GitOpsSyncService) performSingleFileSync(ctx context.Context, sync *models.GitOpsSync, id string, actor models.User, result *gitops.SyncResult, source *preparedSyncSource) (*gitops.SyncResult, error) {
	slog.InfoContext(ctx, "Using single file sync mode", "syncId", id, "composePath", sync.ComposePath)

	project, err := s.getOrCreateProjectInternal(ctx, sync, id, source.composeContent, source.envContent, result, actor)
	if err != nil {
		return result, err
	}

	syncedFiles := []string{filepath.Base(sync.ComposePath)}
	s.updateSyncStatusWithFiles(ctx, id, "success", "", source.commitHash, syncedFiles)
	result.Success = true
	result.Message = fmt.Sprintf("Successfully synced compose file from %s to project %s", sync.ComposePath, project.Name)
	s.logSyncSuccess(ctx, sync, project, actor)
	slog.InfoContext(ctx, "GitOps sync completed", "syncId", id, "project", project.Name)

	return result, nil
}

// redeployIfRunningAfterSync redeploys a project only when it is already
// running and the latest sync actually changed managed content.
func (s *GitOpsSyncService) redeployIfRunningAfterSync(ctx context.Context, project *models.Project, actor models.User, syncMode string) {
	details, err := s.projectService.GetProjectDetails(ctx, project.ID)
	if err != nil {
		return
	}
	if details.Status != string(models.ProjectStatusRunning) && details.Status != string(models.ProjectStatusPartiallyRunning) {
		return
	}

	slog.InfoContext(ctx, "Redeploying project due to content change from Git sync", "syncMode", syncMode, "projectName", project.Name, "projectId", project.ID)
	if err := s.projectService.RedeployProject(ctx, project.ID, actor); err != nil {
		slog.ErrorContext(ctx, "Failed to redeploy project after Git sync", "syncMode", syncMode, "error", err, "projectId", project.ID)
	}
}

// logSyncSuccess records the Git sync completion event once the filesystem and
// sync-status updates have already succeeded.
func (s *GitOpsSyncService) logSyncSuccess(ctx context.Context, sync *models.GitOpsSync, project *models.Project, actor models.User) {
	_, _ = s.eventService.CreateEvent(ctx, CreateEventRequest{
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
	var syncs []scheduledGitOpsSync
	if err := s.db.WithContext(ctx).
		Table("gitops_syncs").
		Select("id", "environment_id", "sync_interval", "last_sync_at").
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
			Name:              importItem.SyncName,
			RepositoryID:      repo.ID,
			Branch:            importItem.Branch,
			ComposePath:       importItem.DockerComposePath,
			ProjectName:       importItem.SyncName,
			AutoSync:          new(importItem.AutoSync),
			SyncInterval:      new(importItem.SyncInterval),
			SyncDirectory:     importItem.SyncDirectory,
			MaxSyncFiles:      importItem.MaxSyncFiles,
			MaxSyncTotalSize:  importItem.MaxSyncTotalSize,
			MaxSyncBinarySize: importItem.MaxSyncBinarySize,
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

	if _, err := s.projectService.ApplyGitSyncProjectFiles(ctx, project.ID, composeContent, envContent, actor); err != nil {
		return nil, s.failSync(ctx, id, result, sync, actor, "Failed to sync project env files", err.Error())
	}

	slog.InfoContext(ctx, "Created project for GitOps sync", "projectName", sync.ProjectName, "projectId", project.ID)

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

	// Update existing project's compose and env files
	_, err := s.projectService.ApplyGitSyncProjectFiles(ctx, project.ID, composeContent, envContent, actor)
	if err != nil {
		return s.failSync(ctx, id, result, sync, actor, "Failed to update project files", err.Error())
	}
	slog.InfoContext(ctx, "Updated project files", "projectName", project.Name, "projectId", project.ID)

	newCompose, newEnv, _ := s.projectService.GetProjectContent(ctx, project.ID)
	contentChanged := oldCompose != newCompose || envContentChangedInternal(oldEnv, newEnv)

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

func envContentChangedInternal(oldEnv, newEnv string) bool {
	oldEnvMap, oldErr := projects.ParseProjectEnvContent(oldEnv, nil)
	newEnvMap, newErr := projects.ParseProjectEnvContent(newEnv, nil)
	if oldErr != nil || newErr != nil {
		return oldEnv != newEnv
	}

	return !maps.Equal(oldEnvMap, newEnvMap)
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
	return new(string(data))
}

// walkAndParseSyncDirectory walks the repository directory and returns all files with their contents.
// Returns the compose file content, the list of SyncFile entries, and an error if any.
func (s *GitOpsSyncService) walkAndParseSyncDirectory(ctx context.Context, sync *models.GitOpsSync, repoPath string) (string, []projects.SyncFile, error) {
	slog.InfoContext(ctx, "Starting directory walk", "syncId", sync.ID, "composePath", sync.ComposePath)

	// Walk the directory to get all files
	maxFiles, maxTotalSize, maxBinarySize := s.getEffectiveSyncLimits(ctx, sync)

	walkResult, err := s.repoService.gitClient.WalkDirectory(ctx, repoPath, sync.ComposePath, maxFiles, maxTotalSize, maxBinarySize)
	if err != nil {
		return "", nil, fmt.Errorf("failed to walk directory: %w", err)
	}

	slog.InfoContext(ctx, "Directory walk complete",
		"syncId", sync.ID,
		"totalFiles", walkResult.TotalFiles,
		"totalSize", walkResult.TotalSize,
		"skippedBinaries", walkResult.SkippedBinaries)

	// WalkDirectory roots the walk at filepath.Dir(sync.ComposePath), so the
	// compose file is always emitted at the top level as filepath.Base(sync.ComposePath).
	composeFileName := filepath.Base(sync.ComposePath)
	var composeContent string

	// Convert walked files to SyncFile format
	syncFiles := make([]projects.SyncFile, len(walkResult.Files))
	for i, f := range walkResult.Files {
		syncFiles[i] = projects.SyncFile{
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

// syncProjectDirectoryInternal runs the new directory-sync path end to end:
// stage files, validate the staged tree, then create or update the project.
func (s *GitOpsSyncService) syncProjectDirectoryInternal(ctx context.Context, sync *models.GitOpsSync, syncFiles []projects.SyncFile, actor models.User) (*models.Project, []string, bool, bool, error) {
	stage, err := s.stageDirectorySyncInternal(ctx, sync, syncFiles)
	if err != nil {
		return nil, nil, false, false, err
	}
	defer func() {
		if stage != nil && stage.stagePath != "" {
			_ = os.RemoveAll(stage.stagePath)
		}
	}()

	if stage.project == nil {
		project, err := s.createDirectorySyncProjectInternal(ctx, sync, stage, actor)
		if err != nil {
			return nil, nil, false, false, err
		}
		return project, stage.syncedFiles, true, true, nil
	}

	project, err := s.updateDirectorySyncProjectInternal(ctx, sync, stage)
	if err != nil {
		return nil, nil, false, false, err
	}
	return project, stage.syncedFiles, false, stage.contentsChanged, nil
}

// stageDirectorySyncInternal builds a temporary project tree that reflects the exact
// repo layout after sync, including cleanup of files removed from the repo.
func (s *GitOpsSyncService) stageDirectorySyncInternal(ctx context.Context, sync *models.GitOpsSync, syncFiles []projects.SyncFile) (*stagedDirectorySync, error) {
	projectsDir, err := s.projectService.getProjectsDirectoryInternal(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get projects directory: %w", err)
	}

	stagePath, err := os.MkdirTemp(projectsDir, ".gitops-sync-stage-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create staging directory: %w", err)
	}

	project, err := s.getDirectorySyncProjectInternal(ctx, sync)
	if err != nil {
		_ = os.RemoveAll(stagePath)
		return nil, err
	}

	if project != nil {
		if err := copyDirectoryContentsInternal(project.Path, stagePath); err != nil {
			_ = os.RemoveAll(stagePath)
			return nil, fmt.Errorf("failed to stage current project files: %w", err)
		}
	}

	syncedFiles := make([]string, len(syncFiles))
	for i, file := range syncFiles {
		syncedFiles[i] = file.RelativePath
	}

	oldSyncedFiles := parseSyncedFiles(sync.SyncedFiles)
	if len(oldSyncedFiles) > 0 {
		if err := projects.CleanupRemovedFiles(projectsDir, stagePath, oldSyncedFiles, syncedFiles); err != nil {
			_ = os.RemoveAll(stagePath)
			return nil, fmt.Errorf("failed to clean removed synced files: %w", err)
		}
	}

	composeFileName := filepath.Base(sync.ComposePath)
	if err := removeStaleComposeFilesInternal(stagePath, composeFileName, syncedFiles); err != nil {
		_ = os.RemoveAll(stagePath)
		return nil, fmt.Errorf("failed to remove stale compose files: %w", err)
	}

	contentsChanged, err := directorySyncContentsChangedInternal(project, syncFiles, oldSyncedFiles, composeFileName)
	if err != nil {
		_ = os.RemoveAll(stagePath)
		return nil, fmt.Errorf("failed to compare staged directory changes: %w", err)
	}

	// Write the repo files after cleanup so validation sees the final on-disk
	// tree exactly as it will exist in the managed project.
	if _, err := projects.WriteSyncedDirectory(projectsDir, stagePath, syncFiles); err != nil {
		_ = os.RemoveAll(stagePath)
		return nil, fmt.Errorf("failed to write staged sync files: %w", err)
	}

	serviceCount, err := s.validateDirectorySyncStageInternal(ctx, sync.ProjectName, stagePath, composeFileName)
	if err != nil {
		_ = os.RemoveAll(stagePath)
		return nil, fmt.Errorf("invalid compose file: %w", err)
	}

	return &stagedDirectorySync{
		stagePath:       stagePath,
		composeFileName: composeFileName,
		project:         project,
		syncedFiles:     syncedFiles,
		serviceCount:    serviceCount,
		contentsChanged: contentsChanged,
	}, nil
}

// getDirectorySyncProjectInternal resolves the linked project for a sync when one
// exists, while tolerating deleted/stale project references.
func (s *GitOpsSyncService) getDirectorySyncProjectInternal(ctx context.Context, sync *models.GitOpsSync) (*models.Project, error) {
	if sync.ProjectID == nil || *sync.ProjectID == "" {
		return nil, nil
	}

	project, err := s.projectService.GetProjectFromDatabaseByID(ctx, *sync.ProjectID)
	if err != nil {
		slog.WarnContext(ctx, "Existing project not found, will create new one", "projectId", *sync.ProjectID, "error", err)
		return nil, nil
	}

	if err := s.projectService.ensureProjectPathUnderRoot(ctx, project, false); err != nil {
		return nil, err
	}

	return project, nil
}

// validateDirectorySyncStageInternal loads the staged compose project using the real
// synced compose filename so include/env_file resolution happens against the
// fully copied directory contents.
func (s *GitOpsSyncService) validateDirectorySyncStageInternal(ctx context.Context, projectName, stagePath, composeFileName string) (int, error) {
	projectsDir, err := s.projectService.getProjectsDirectoryInternal(ctx)
	if err != nil {
		return 0, err
	}

	pathMapper, pmErr := s.projectService.getPathMapper(ctx)
	if pmErr != nil {
		slog.WarnContext(ctx, "failed to create path mapper for directory sync validation, continuing without translation", "error", pmErr)
	}

	autoInjectEnv := s.settingsService.GetBoolSetting(ctx, "autoInjectEnv", false)
	project, err := projects.LoadComposeProject(
		ctx,
		filepath.Join(stagePath, composeFileName),
		normalizeComposeProjectName(projectName),
		projectsDir,
		autoInjectEnv,
		pathMapper,
	)
	if err != nil {
		return 0, err
	}

	return len(project.Services), nil
}

// createDirectorySyncProjectInternal promotes a validated staged tree into a new
// managed project directory and links it back to the Git sync record.
func (s *GitOpsSyncService) createDirectorySyncProjectInternal(ctx context.Context, sync *models.GitOpsSync, stage *stagedDirectorySync, actor models.User) (*models.Project, error) {
	projectsDir, err := s.projectService.getProjectsDirectoryInternal(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get projects directory: %w", err)
	}

	basePath := filepath.Join(projectsDir, projects.SanitizeProjectName(sync.ProjectName))
	projectPath, folderName, err := projects.CreateUniqueDir(projectsDir, basePath, sync.ProjectName, 0o755)
	if err != nil {
		return nil, fmt.Errorf("failed to create project directory: %w", err)
	}

	if err := os.Remove(projectPath); err != nil {
		return nil, fmt.Errorf("failed to prepare project directory: %w", err)
	}

	if err := os.Rename(stage.stagePath, projectPath); err != nil {
		return nil, fmt.Errorf("failed to promote staged project directory: %w", err)
	}
	stage.stagePath = ""

	project := &models.Project{
		Name:         sync.ProjectName,
		DirName:      new(folderName),
		Path:         projectPath,
		Status:       models.ProjectStatusStopped,
		ServiceCount: stage.serviceCount,
		RunningCount: 0,
	}

	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(project).Error; err != nil {
			return fmt.Errorf("failed to create project: %w", err)
		}

		if err := tx.Model(&models.GitOpsSync{}).Where("id = ?", sync.ID).Update("project_id", project.ID).Error; err != nil {
			return fmt.Errorf("failed to update sync with project ID: %w", err)
		}

		if err := tx.Model(&models.Project{}).Where("id = ?", project.ID).Update("gitops_managed_by", sync.ID).Error; err != nil {
			return fmt.Errorf("failed to mark project as GitOps-managed: %w", err)
		}

		return nil
	}); err != nil {
		_ = os.RemoveAll(projectPath)
		return nil, err
	}

	sync.ProjectID = &project.ID
	s.projectService.cacheComposeProjectIDInternal(normalizeComposeProjectName(project.Name), project.ID)

	if s.projectService.eventService != nil {
		metadata := models.JSON{"action": "create", "projectID": project.ID, "projectName": project.Name, "path": projectPath}
		if logErr := s.projectService.eventService.LogProjectEvent(ctx, models.EventTypeProjectCreate, project.ID, project.Name, actor.ID, actor.Username, "0", metadata); logErr != nil {
			slog.ErrorContext(ctx, "could not log project creation", "error", logErr)
		}
	}

	return project, nil
}

// updateDirectorySyncProjectInternal swaps a validated staged tree into the existing
// project path with a temporary backup so failed promotion can roll back.
func (s *GitOpsSyncService) updateDirectorySyncProjectInternal(ctx context.Context, sync *models.GitOpsSync, stage *stagedDirectorySync) (*models.Project, error) {
	project := stage.project
	projectPath := filepath.Clean(project.Path)
	backupPath := ""

	if info, err := os.Stat(projectPath); err == nil {
		if !info.IsDir() {
			return nil, fmt.Errorf("project path is not a directory: %s", projectPath)
		}
		backupPath = fmt.Sprintf("%s.gitops-backup-%d", projectPath, time.Now().UnixNano())
		if err := os.Rename(projectPath, backupPath); err != nil {
			return nil, fmt.Errorf("failed to move current project directory out of the way: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("failed to inspect current project directory: %w", err)
	}

	if err := os.Rename(stage.stagePath, projectPath); err != nil {
		if backupPath != "" {
			_ = os.Rename(backupPath, projectPath)
		}
		return nil, fmt.Errorf("failed to promote staged project directory: %w", err)
	}
	stage.stagePath = ""

	if err := s.db.WithContext(ctx).Model(&models.Project{}).Where("id = ?", project.ID).Updates(map[string]any{
		"service_count":     stage.serviceCount,
		"gitops_managed_by": sync.ID,
		"updated_at":        time.Now(),
	}).Error; err != nil {
		if backupPath != "" {
			_ = os.RemoveAll(projectPath)
			_ = os.Rename(backupPath, projectPath)
		}
		return nil, fmt.Errorf("failed to update project metadata after directory sync: %w", err)
	}

	if backupPath != "" {
		_ = os.RemoveAll(backupPath)
	}

	return project, nil
}

// directorySyncContentsChangedInternal compares the currently managed synced files with
// the newly fetched repo files to decide whether a redeploy is needed.
func directorySyncContentsChangedInternal(project *models.Project, syncFiles []projects.SyncFile, oldSyncedFiles []string, composeFileName string) (bool, error) {
	if project == nil {
		return true, nil
	}

	if info, err := os.Stat(project.Path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return true, nil
		}
		return false, err
	} else if !info.IsDir() {
		return false, fmt.Errorf("project path is not a directory: %s", project.Path)
	}

	newFileSet := make(map[string]struct{}, len(syncFiles))
	for _, file := range syncFiles {
		newFileSet[file.RelativePath] = struct{}{}
		existingContent, err := os.ReadFile(filepath.Join(project.Path, file.RelativePath))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return true, nil
			}
			return false, err
		}
		if !bytes.Equal(existingContent, file.Content) {
			return true, nil
		}
	}

	for _, oldFile := range oldSyncedFiles {
		if _, exists := newFileSet[oldFile]; exists {
			continue
		}
		if _, err := os.Stat(filepath.Join(project.Path, oldFile)); err == nil {
			return true, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
	}

	for _, candidate := range projects.ComposeFileCandidates() {
		if candidate == composeFileName {
			continue
		}
		if _, exists := newFileSet[candidate]; exists {
			continue
		}
		if _, err := os.Stat(filepath.Join(project.Path, candidate)); err == nil {
			return true, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
	}

	return false, nil
}

// removeStaleComposeFilesInternal drops old canonical compose filenames from the staged
// project when the repo now uses a different compose filename.
func removeStaleComposeFilesInternal(projectPath, composeFileName string, syncedFiles []string) error {
	syncedFileSet := make(map[string]struct{}, len(syncedFiles))
	for _, file := range syncedFiles {
		syncedFileSet[file] = struct{}{}
	}

	for _, candidate := range projects.ComposeFileCandidates() {
		if candidate == composeFileName {
			continue
		}
		if _, exists := syncedFileSet[candidate]; exists {
			continue
		}
		if err := os.Remove(filepath.Join(projectPath, candidate)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}

	return nil
}

// copyDirectoryContentsInternal clones the current project tree into the staging
// directory so directory sync updates can preserve non-synced local files.
func copyDirectoryContentsInternal(srcDir, destDir string) error {
	srcRoot, err := os.OpenRoot(srcDir)
	if err != nil {
		return err
	}
	defer func() { _ = srcRoot.Close() }()

	destRoot, err := os.OpenRoot(destDir)
	if err != nil {
		return err
	}
	defer func() { _ = destRoot.Close() }()

	return filepath.WalkDir(srcDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == srcDir {
			return nil
		}

		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}

		if d.IsDir() {
			return destRoot.MkdirAll(relPath, 0o755)
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		content, err := srcRoot.ReadFile(relPath)
		if err != nil {
			return err
		}

		if err := destRoot.MkdirAll(filepath.Dir(relPath), 0o755); err != nil {
			return err
		}

		return destRoot.WriteFile(relPath, content, info.Mode())
	})
}

// updateSyncStatusWithFiles updates sync status including the list of synced files
func (s *GitOpsSyncService) updateSyncStatusWithFiles(ctx context.Context, id, status, errorMsg, commitHash string, syncedFiles []string) {
	now := time.Now()
	updates := map[string]any{
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
