package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/mapper"
	"github.com/getarcaneapp/arcane/types/v2/gitops"
	projecttypes "github.com/getarcaneapp/arcane/types/v2/project"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/getarcaneapp/arcane/types/v2/swarm"
	"gorm.io/gorm"
)

// DynamicScheduler is the subset of the job scheduler used by services that
// register per-entity jobs at runtime (GitOps syncs, environment health). It is a
// consumer-side interface satisfied by *pkg/scheduler.JobScheduler; the scheduler
// is injected post-construction via SetScheduler because it is created after the
// service graph is built (and pkg/scheduler imports this package, so it cannot be
// a wire input here).
type DynamicScheduler interface {
	AddJob(ctx context.Context, job schedulertypes.Job) error
	RemoveJob(ctx context.Context, name string)
	HasJob(name string) bool
}

type GitOpsSyncService struct {
	db              *database.DB
	repoService     *GitRepositoryService
	projectService  *ProjectService
	swarmService    *SwarmService
	eventService    *EventService
	settingsService *SettingsService

	// scheduler and lifecycleCtx are injected post-construction via SetScheduler.
	scheduler    DynamicScheduler
	lifecycleCtx context.Context
	// runningSyncs guards against overlapping PerformSync runs for the same sync.
	// The previous single global job serialized syncs implicitly; with independent
	// per-sync jobs (plus manual/webhook/startup triggers) a sync could otherwise
	// run concurrently with itself and race the clone/redeploy of one project.
	runningSyncs sync.Map
}

const defaultGitSyncTimeout = 5 * time.Minute

const (
	defaultMaxSyncFiles        = 500
	defaultMaxSyncTotalSizeMB  = 50
	defaultMaxSyncBinarySizeMB = 10
	defaultMaxSyncTotalSize    = defaultMaxSyncTotalSizeMB * 1024 * 1024
	defaultMaxSyncBinarySize   = defaultMaxSyncBinarySizeMB * 1024 * 1024
)

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
	// copySkipped holds project-relative paths that could not be read when the
	// live project directory was copied into the stage (e.g. foreign-owned
	// bind-mount data). They are absent from the stage, so promotion must
	// preserve rather than prune them.
	copySkipped []string
}

func validateSyncLimits(maxFiles *int, maxTotalSize, maxBinarySize *int64) error {
	if maxFiles != nil && *maxFiles < 0 {
		return errors.New("maxSyncFiles must be non-negative")
	}
	if maxTotalSize != nil && *maxTotalSize < 0 {
		return errors.New("maxSyncTotalSize must be non-negative")
	}
	if maxBinarySize != nil && *maxBinarySize < 0 {
		return errors.New("maxSyncBinarySize must be non-negative")
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

func hasEstablishedProjectBindingInternal(sync *models.GitOpsSync) bool {
	return sync != nil && sync.ProjectID != nil && strings.TrimSpace(*sync.ProjectID) != ""
}

func NewGitOpsSyncService(db *database.DB, repoService *GitRepositoryService, projectService *ProjectService, swarmService *SwarmService, eventService *EventService, settingsService *SettingsService) *GitOpsSyncService {
	return &GitOpsSyncService{
		db:              db,
		repoService:     repoService,
		projectService:  projectService,
		swarmService:    swarmService,
		eventService:    eventService,
		settingsService: settingsService,
	}
}

const gitOpsSyncJobPrefix = "gitops-sync:"

func gitOpsSyncJobNameInternal(syncID string) string { return gitOpsSyncJobPrefix + syncID }

// SetScheduler injects the job scheduler and the app lifecycle context. It must be
// called during bootstrap (after the service graph is built) before any per-sync
// jobs are registered. The lifecycle context is used for background sync kicks so
// they outlive the request/bootstrap goroutine that triggered them.
func (s *GitOpsSyncService) SetScheduler(ctx context.Context, scheduler DynamicScheduler) { //nolint:contextcheck // background sync kicks must capture the app lifecycle context, not request contexts
	if ctx == nil {
		ctx = context.Background()
	}
	s.lifecycleCtx = ctx
	s.scheduler = scheduler
}

func (s *GitOpsSyncService) schedulerCtxInternal(ctx context.Context) context.Context {
	if s.lifecycleCtx != nil {
		return s.lifecycleCtx
	}
	if ctx != nil {
		return context.WithoutCancel(ctx)
	}
	return context.Background()
}

// buildSyncJobInternal returns the dynamic job for a single sync. The schedule is a
// fixed "@every Nm" interval; the run body re-reads the sync each fire (so a row
// deleted or toggled to AutoSync=false self-cancels cleanly) and delegates to
// PerformSync, which owns its own timeout and the per-sync in-flight guard.
func (s *GitOpsSyncService) buildSyncJobInternal(syncID, environmentID string, intervalMinutes int) *schedulertypes.GenericJob {
	interval := max(intervalMinutes, 1)
	schedule := fmt.Sprintf("@every %dm", interval)
	return &schedulertypes.GenericJob{
		JobName: gitOpsSyncJobNameInternal(syncID),
		ScheduleFn: func(_ context.Context) string {
			return schedule
		},
		RunFn: func(ctx context.Context) {
			sync, err := s.getSyncRecordByIDInternal(ctx, environmentID, syncID)
			if err != nil {
				slog.DebugContext(ctx, "gitops auto-sync skipped; sync not found", "syncId", syncID, "error", err)
				return
			}
			if !sync.AutoSync {
				return
			}
			if _, err := s.PerformSync(ctx, environmentID, syncID, systemUser); err != nil {
				slog.ErrorContext(ctx, "gitops auto-sync run failed", "syncId", syncID, "error", err)
			}
		},
	}
}

func (s *GitOpsSyncService) registerSyncJobInternal(ctx context.Context, syncID, environmentID string, intervalMinutes int) {
	if s.scheduler == nil {
		return
	}
	job := s.buildSyncJobInternal(syncID, environmentID, intervalMinutes)
	schedulerCtx := s.schedulerCtxInternal(ctx)
	if err := s.scheduler.AddJob(schedulerCtx, job); err != nil {
		slog.ErrorContext(schedulerCtx, "Failed to register gitops sync job", "syncId", syncID, "error", err)
	}
}

func (s *GitOpsSyncService) unregisterSyncJobInternal(ctx context.Context, syncID string) {
	if s.scheduler == nil {
		return
	}
	s.scheduler.RemoveJob(s.schedulerCtxInternal(ctx), gitOpsSyncJobNameInternal(syncID))
}

// kickSyncInternal runs a sync once in the background on the app lifecycle context.
// Used when auto-sync is freshly enabled or when a sync is overdue at startup, so
// the first run does not wait a full interval.
func (s *GitOpsSyncService) kickSyncInternal(ctx context.Context, syncID, environmentID string) {
	ctx = s.schedulerCtxInternal(ctx)
	go func() {
		if _, err := s.PerformSync(ctx, environmentID, syncID, systemUser); err != nil {
			slog.ErrorContext(ctx, "gitops immediate sync kick failed", "syncId", syncID, "error", err)
		}
	}()
}

// RegisterAutoSyncJobsOnStartup registers a dynamic job for every auto-sync-enabled
// sync and kicks an immediate run for any that are overdue. This replaces the old
// global polling job so existing syncs keep running after upgrade.
func (s *GitOpsSyncService) RegisterAutoSyncJobsOnStartup(ctx context.Context) {
	if s.scheduler == nil {
		return
	}
	var syncs []models.GitOpsSync
	if err := s.db.WithContext(ctx).
		Where("auto_sync = ?", true).
		Find(&syncs).Error; err != nil {
		slog.ErrorContext(ctx, "Failed to load auto-sync jobs on startup", "error", err)
		return
	}
	for i := range syncs {
		sync := syncs[i]
		s.registerSyncJobInternal(ctx, sync.ID, sync.EnvironmentID, sync.SyncInterval)
		if isGitOpsSyncOverdueInternal(&sync) {
			s.kickSyncInternal(ctx, sync.ID, sync.EnvironmentID)
		}
	}
	slog.InfoContext(ctx, "Registered gitops auto-sync jobs on startup", "count", len(syncs))
}

func isGitOpsSyncOverdueInternal(sync *models.GitOpsSync) bool {
	if sync.LastSyncAt == nil {
		return true
	}
	interval := max(sync.SyncInterval, 1)
	return time.Now().After(sync.LastSyncAt.Add(time.Duration(interval) * time.Minute))
}

// acquireSyncLockInternal returns a release func and true when no sync is currently
// running for the given id; otherwise it returns false and the caller should skip.
func (s *GitOpsSyncService) acquireSyncLockInternal(syncID string) (func(), bool) {
	if _, loaded := s.runningSyncs.LoadOrStore(syncID, struct{}{}); loaded {
		return nil, false
	}
	return func() { s.runningSyncs.Delete(syncID) }, true
}

func (s *GitOpsSyncService) getEnvironmentSyncLimits(ctx context.Context) (int, int64, int64) {
	if s.settingsService == nil {
		return defaultMaxSyncFiles, defaultMaxSyncTotalSize, defaultMaxSyncBinarySize
	}

	cfg := s.settingsService.GetSettingsOrDefaults(ctx)
	maxFiles := normalizeSyncLimitSetting(utils.IntOrDefault(cfg.GitSyncMaxFiles.Value, defaultMaxSyncFiles), defaultMaxSyncFiles)
	maxTotalSizeMB := normalizeSyncLimitSetting(utils.IntOrDefault(cfg.GitSyncMaxTotalSizeMb.Value, defaultMaxSyncTotalSizeMB), defaultMaxSyncTotalSizeMB)
	maxBinarySizeMB := normalizeSyncLimitSetting(utils.IntOrDefault(cfg.GitSyncMaxBinarySizeMb.Value, defaultMaxSyncBinarySizeMB), defaultMaxSyncBinarySizeMB)

	return maxFiles, megabytesToBytes(maxTotalSizeMB), megabytesToBytes(maxBinarySizeMB)
}

func (s *GitOpsSyncService) getEffectiveSyncLimits(ctx context.Context, sync *models.GitOpsSync) (int, int64, int64) {
	environmentMaxFiles, environmentMaxTotalSize, environmentMaxBinarySize := s.getEnvironmentSyncLimits(ctx)
	if sync == nil {
		return environmentMaxFiles, environmentMaxTotalSize, environmentMaxBinarySize
	}

	maxFiles := sync.MaxSyncFiles
	maxTotalSize := sync.MaxSyncTotalSize
	maxBinarySize := sync.MaxSyncBinarySize

	if s.gitSyncLimitEnvOverrideActiveInternal("gitSyncMaxFiles") {
		maxFiles = environmentMaxFiles
	}
	if s.gitSyncLimitEnvOverrideActiveInternal("gitSyncMaxTotalSizeMb") {
		maxTotalSize = environmentMaxTotalSize
	}
	if s.gitSyncLimitEnvOverrideActiveInternal("gitSyncMaxBinarySizeMb") {
		maxBinarySize = environmentMaxBinarySize
	}

	return maxFiles, maxTotalSize, maxBinarySize
}

func (s *GitOpsSyncService) gitSyncLimitEnvOverrideActiveInternal(key string) bool {
	return s.settingsService != nil && s.settingsService.isEnvOverrideActiveInternal(key)
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
	sync, err := s.getSyncByIDInternal(ctx, environmentID, id, true)
	if err != nil {
		var notFound *models.NotFoundError
		if errors.As(err, &notFound) {
			slog.WarnContext(ctx, "GitOps sync not found", "syncID", id, "environmentID", environmentID)
			return nil, err
		}
		slog.ErrorContext(ctx, "Failed to get GitOps sync", "syncID", id, "environmentID", environmentID, "error", err)
		return nil, err
	}
	return sync, nil
}

func (s *GitOpsSyncService) getSyncByIDInternal(ctx context.Context, environmentID, id string, preloadAssociations bool) (*models.GitOpsSync, error) {
	var sync models.GitOpsSync
	q := s.db.WithContext(ctx).Where("id = ?", id)
	if preloadAssociations {
		q = q.Preload("Repository").Preload("Project")
	}
	if environmentID != "" {
		q = q.Where("environment_id = ?", environmentID)
	}
	if err := q.First(&sync).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, &models.NotFoundError{Message: "GitOps sync not found"}
		}
		return nil, fmt.Errorf("failed to get sync: %w", err)
	}
	return &sync, nil
}

func (s *GitOpsSyncService) getSyncRecordByIDInternal(ctx context.Context, environmentID, id string) (*models.GitOpsSync, error) {
	return s.getSyncByIDInternal(ctx, environmentID, id, false)
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
		TargetType:        req.TargetType,
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

	if err := s.db.WithContext(ctx).Omit("Environment", "Repository", "Project").Create(&sync).Error; err != nil {
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

	// Register the recurring job if auto-sync is on. The initial sync above already
	// ran once, so no extra kick is needed here.
	if sync.AutoSync {
		s.registerSyncJobInternal(ctx, sync.ID, sync.EnvironmentID, sync.SyncInterval)
	}

	return s.GetSyncByID(ctx, "", sync.ID)
}

func (s *GitOpsSyncService) UpdateSync(ctx context.Context, environmentID, id string, req gitops.UpdateSyncRequest, actor models.User) (*models.GitOpsSync, error) {
	sync, err := s.GetSyncByID(ctx, environmentID, id)
	if err != nil {
		return nil, err
	}

	// Capture state needed to reconcile the dynamic job after the update.
	oldAutoSync := sync.AutoSync
	newAutoSync := sync.AutoSync
	if req.AutoSync != nil {
		newAutoSync = *req.AutoSync
	}
	newInterval := sync.SyncInterval
	if req.SyncInterval != nil {
		newInterval = *req.SyncInterval
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
	if req.TargetType != nil {
		updates["target_type"] = *req.TargetType
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

	// Reconcile the dynamic job to match the new state.
	switch {
	case newAutoSync:
		s.registerSyncJobInternal(ctx, sync.ID, sync.EnvironmentID, newInterval)
		if !oldAutoSync {
			// Freshly enabled — kick a run now so it doesn't wait a full interval.
			s.kickSyncInternal(ctx, sync.ID, sync.EnvironmentID)
		}
	default:
		s.unregisterSyncJobInternal(ctx, sync.ID)
	}

	return s.GetSyncByID(ctx, environmentID, id)
}

func (s *GitOpsSyncService) DeleteSync(ctx context.Context, environmentID, id string, actor models.User) error {
	// Get sync info before deleting
	sync, err := s.getSyncRecordByIDInternal(ctx, environmentID, id)
	if err != nil {
		return err
	}

	// Stop the recurring job before the row goes away. Any in-flight run re-reads
	// the sync and self-cancels once the row is gone.
	s.unregisterSyncJobInternal(ctx, id)

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
		if sync.AutoSync {
			s.registerSyncJobInternal(ctx, sync.ID, sync.EnvironmentID, sync.SyncInterval)
		}
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
	// Coalesce overlapping runs for the same sync (scheduled fire, startup/enable
	// kick, manual trigger, webhook) so they don't race the clone/redeploy.
	release, ok := s.acquireSyncLockInternal(id)
	if !ok {
		slog.InfoContext(ctx, "GitOps sync already in progress; skipping", "syncId", id)
		return &gitops.SyncResult{Success: false, Message: "sync already in progress", SyncedAt: time.Now()}, nil
	}
	defer release()

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

	if sync.TargetType == "swarm_stack" {
		return s.performSwarmStackSyncInternal(syncCtx, sync, id, actor, result, source)
	}

	if sync.SyncDirectory {
		return s.performDirectorySync(syncCtx, sync, id, actor, result, source)
	}

	return s.performSingleFileSyncInternal(syncCtx, sync, id, actor, result, source)
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
		errMsg := "compose file not found: " + sync.ComposePath
		return &preparedSyncSource{repoPath: repoPath, commitHash: commitHash}, s.failSync(ctx, sync.ID, result, sync, actor, "Compose file not found at "+sync.ComposePath, errMsg)
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

	syncFiles, err := s.walkAndParseSyncDirectory(ctx, sync, source.repoPath)
	if err != nil {
		return result, s.failSync(ctx, id, result, sync, actor, "Failed to walk directory", err.Error())
	}

	project, syncedFiles, _, contentsChanged, err := s.syncProjectDirectoryInternal(ctx, sync, syncFiles, actor)
	if err != nil {
		var bindingErr *common.GitOpsSyncProjectBindingBrokenError
		if errors.As(err, &bindingErr) {
			errMsg := bindingErr.Error()
			result.Message = "GitOps project binding broken"
			result.Error = new(errMsg)
			return result, bindingErr
		}
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

// performSingleFileSyncInternal preserves the legacy compose-only Git sync behavior.
func (s *GitOpsSyncService) performSingleFileSyncInternal(ctx context.Context, sync *models.GitOpsSync, id string, actor models.User, result *gitops.SyncResult, source *preparedSyncSource) (*gitops.SyncResult, error) {
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

// performSwarmStackSyncInternal executes a single file sync targeted at a Swarm Stack
func (s *GitOpsSyncService) performSwarmStackSyncInternal(ctx context.Context, sync *models.GitOpsSync, id string, actor models.User, result *gitops.SyncResult, source *preparedSyncSource) (*gitops.SyncResult, error) {
	slog.InfoContext(ctx, "Deploying Swarm Stack from GitOps sync", "syncId", id, "stackName", sync.ProjectName)

	if s.swarmService == nil {
		return result, s.failSync(ctx, id, result, sync, actor, "Swarm service is unavailable", "swarm service is unavailable")
	}

	envContent := ""
	if source.envContent != nil {
		envContent = *source.envContent
	}

	var syncFiles []projects.SyncFile
	if sync.SyncDirectory {
		files, err := s.walkAndParseSyncDirectory(ctx, sync, source.repoPath)
		if err != nil {
			return result, s.failSync(ctx, id, result, sync, actor, "Failed to walk directory", err.Error())
		}
		syncFiles = files
	}

	swarmFiles := make([]swarm.SyncFile, len(syncFiles))
	syncedFiles := make([]string, 0, len(syncFiles))
	for i, f := range syncFiles {
		swarmFiles[i] = swarm.SyncFile{
			RelativePath: f.RelativePath,
			Content:      f.Content,
		}
		syncedFiles = append(syncedFiles, f.RelativePath)
	}

	req := swarm.StackDeployRequest{
		Name:           sync.ProjectName,
		ComposeContent: source.composeContent,
		EnvContent:     envContent,
		Files:          swarmFiles,
		Prune:          true,
		WorkingDir:     filepath.Dir(filepath.Join(source.repoPath, sync.ComposePath)),
	}

	if _, err := s.swarmService.DeployStack(ctx, sync.EnvironmentID, req); err != nil {
		return result, s.failSync(ctx, id, result, sync, actor, "Failed to deploy swarm stack", err.Error())
	}

	if len(syncedFiles) == 0 {
		syncedFiles = []string{filepath.Base(sync.ComposePath)}
	}
	s.updateSyncStatusWithFiles(ctx, id, "success", "", source.commitHash, syncedFiles)
	result.Success = true
	result.Message = fmt.Sprintf("Successfully deployed swarm stack %s from %s", sync.ProjectName, sync.ComposePath)

	// Log event
	_, _ = s.eventService.CreateEvent(ctx, CreateEventRequest{
		Type:          models.EventTypeGitSyncRun,
		Severity:      models.EventSeveritySuccess,
		Title:         "Git sync completed for stack",
		Description:   fmt.Sprintf("Successfully synced '%s' to swarm stack '%s'", sync.Name, sync.ProjectName),
		ResourceType:  new("git_sync"),
		ResourceID:    new(sync.ID),
		ResourceName:  new(sync.Name),
		UserID:        new(actor.ID),
		Username:      new(actor.Username),
		EnvironmentID: new(sync.EnvironmentID),
	})

	slog.InfoContext(ctx, "GitOps swarm stack sync completed", "syncId", id, "stack", sync.ProjectName)
	return result, nil
}

// redeployIfRunningAfterSync redeploys a project only when it is already
// running and the latest sync actually changed managed content.
func (s *GitOpsSyncService) redeployIfRunningAfterSync(ctx context.Context, project *models.Project, actor models.User, syncMode string) {
	details, err := s.projectService.GetProjectDetails(ctx, project.ID, projecttypes.AllDetails())
	if err != nil {
		return
	}
	if details.Status != string(models.ProjectStatusRunning) && details.Status != string(models.ProjectStatusPartiallyRunning) {
		return
	}

	slog.InfoContext(ctx, "Redeploying project due to content change from Git sync", "syncMode", syncMode, "projectName", project.Name, "projectId", project.ID)
	if err := s.projectService.RedeployProject(ctx, project.ID, actor, nil); err != nil {
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

func (s *GitOpsSyncService) ReconcileDirectorySyncProjectsOnStartup(ctx context.Context) error {
	var syncs []models.GitOpsSync
	if err := s.db.WithContext(ctx).
		Where("sync_directory = ?", true).
		Find(&syncs).Error; err != nil {
		return fmt.Errorf("failed to list directory syncs for startup reconciliation: %w", err)
	}

	for i := range syncs {
		originalProjectID := ""
		if syncs[i].ProjectID != nil {
			originalProjectID = *syncs[i].ProjectID
		}

		project, err := s.getDirectorySyncProjectInternal(ctx, &syncs[i])
		if err != nil {
			slog.WarnContext(ctx, "Failed to reconcile directory GitOps sync on startup", "syncId", syncs[i].ID, "error", err)
			continue
		}
		if project == nil {
			continue
		}

		if originalProjectID != project.ID {
			slog.InfoContext(ctx, "Reconciled directory GitOps sync on startup", "syncId", syncs[i].ID, "projectId", project.ID)
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
		return nil, errors.New("repository not found")
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

func (s *GitOpsSyncService) disableAutoSyncForBrokenBindingInternal(ctx context.Context, sync *models.GitOpsSync) {
	if sync == nil || sync.ID == "" {
		return
	}
	if err := s.db.WithContext(ctx).
		Model(&models.GitOpsSync{}).
		Where("id = ?", sync.ID).
		Update("auto_sync", false).Error; err != nil {
		slog.ErrorContext(ctx, "Failed to disable GitOps auto-sync after broken project binding", "syncId", sync.ID, "error", err)
	}
	sync.AutoSync = false
	s.unregisterSyncJobInternal(ctx, sync.ID)
}

func (s *GitOpsSyncService) failSyncAndDisableAutoSyncInternal(ctx context.Context, id string, result *gitops.SyncResult, sync *models.GitOpsSync, actor models.User, message string, failure error) error {
	errMsg := failure.Error()
	_ = s.failSync(ctx, id, result, sync, actor, message, errMsg)
	s.disableAutoSyncForBrokenBindingInternal(ctx, sync)
	return failure
}

func (s *GitOpsSyncService) recordBrokenProjectBindingInternal(ctx context.Context, sync *models.GitOpsSync, actor models.User, err error) {
	var bindingErr *common.GitOpsSyncProjectBindingBrokenError
	if !errors.As(err, &bindingErr) || sync == nil {
		return
	}
	errMsg := bindingErr.Error()
	s.updateSyncStatus(ctx, sync.ID, "failed", errMsg, "")
	s.logSyncError(ctx, sync, actor, errMsg)
	s.disableAutoSyncForBrokenBindingInternal(ctx, sync)
}

func (s *GitOpsSyncService) createProjectForSyncInternal(ctx context.Context, sync *models.GitOpsSync, id string, composeContent string, envContent *string, result *gitops.SyncResult, actor models.User) (*models.Project, error) {
	project, err := s.projectService.CreateProject(ctx, sync.ProjectName, composeContent, envContent, nil, actor)
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

	if sync.ProjectID != nil && *sync.ProjectID != "" {
		var found bool
		var lookupErr error
		project, found, lookupErr = s.lookupProjectByIDInternal(ctx, *sync.ProjectID)
		if lookupErr != nil {
			return nil, s.failSync(ctx, id, result, sync, actor, "Failed to load existing project", lookupErr.Error())
		}
		if !found {
			err := &common.GitOpsSyncProjectBindingBrokenError{
				Err: fmt.Errorf("sync %s references missing project %s", sync.ID, *sync.ProjectID),
			}
			slog.WarnContext(ctx, "Existing project not found; GitOps project binding is broken", "projectId", *sync.ProjectID, "syncId", sync.ID)
			return nil, s.failSyncAndDisableAutoSyncInternal(ctx, id, result, sync, actor, "GitOps project binding broken", err)
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
		details, err := s.projectService.GetProjectDetails(ctx, project.ID, projecttypes.AllDetails())
		if err == nil && (details.Status == string(models.ProjectStatusRunning) || details.Status == string(models.ProjectStatusPartiallyRunning)) {
			slog.InfoContext(ctx, "Redeploying project due to content change from Git sync", "projectName", project.Name, "projectId", project.ID)
			if err := s.projectService.RedeployProject(ctx, project.ID, actor, nil); err != nil {
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
// Returns the list of SyncFile entries and an error if any; it fails if the compose file is missing.
func (s *GitOpsSyncService) walkAndParseSyncDirectory(ctx context.Context, sync *models.GitOpsSync, repoPath string) ([]projects.SyncFile, error) {
	slog.InfoContext(ctx, "Starting directory walk", "syncId", sync.ID, "composePath", sync.ComposePath)

	// Walk the directory to get all files
	maxFiles, maxTotalSize, maxBinarySize := s.getEffectiveSyncLimits(ctx, sync)

	walkResult, err := s.repoService.gitClient.WalkDirectory(ctx, repoPath, sync.ComposePath, maxFiles, maxTotalSize, maxBinarySize)
	if err != nil {
		return nil, fmt.Errorf("failed to walk directory: %w", err)
	}

	slog.InfoContext(ctx, "Directory walk complete",
		"syncId", sync.ID,
		"totalFiles", walkResult.TotalFiles,
		"totalSize", walkResult.TotalSize,
		"skippedBinaries", walkResult.SkippedBinaries)

	// WalkDirectory roots the walk at filepath.Dir(sync.ComposePath), so the
	// compose file is always emitted at the top level as filepath.Base(sync.ComposePath).
	composeFileName := filepath.Base(sync.ComposePath)
	composeFound := false

	// Convert walked files to SyncFile format
	syncFiles := make([]projects.SyncFile, len(walkResult.Files))
	for i, f := range walkResult.Files {
		syncFiles[i] = projects.SyncFile{
			RelativePath: f.RelativePath,
			Content:      f.Content,
		}
		if f.RelativePath == composeFileName {
			composeFound = true
		}
	}

	if !composeFound {
		return nil, fmt.Errorf("compose file %s not found in walked directory", composeFileName)
	}

	return syncFiles, nil
}

// syncProjectDirectoryInternal runs the new directory-sync path end to end:
// stage files, validate the staged tree, then create or update the project.
func (s *GitOpsSyncService) syncProjectDirectoryInternal(ctx context.Context, sync *models.GitOpsSync, syncFiles []projects.SyncFile, actor models.User) (*models.Project, []string, bool, bool, error) {
	stage, err := s.stageDirectorySyncInternal(ctx, sync, syncFiles)
	if err != nil {
		s.recordBrokenProjectBindingInternal(ctx, sync, actor, err)
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

	var copySkipped []string
	if project != nil {
		// Tolerate files Arcane cannot read (e.g. foreign-owned files a container
		// wrote into the project directory through a relative bind mount). Skipping
		// them keeps an unrelated unreadable file from aborting the whole sync; the
		// skipped paths are preserved (not pruned) when the staged tree is mirrored
		// back over the live project during promotion.
		copySkipped, err = projects.CopyDirectoryContentsTolerant(project.Path, stagePath)
		if err != nil {
			_ = os.RemoveAll(stagePath)
			return nil, fmt.Errorf("failed to stage current project files: %w", err)
		}
		if len(copySkipped) > 0 {
			slog.WarnContext(ctx, "skipped unreadable files while staging project sync; they will be left untouched on promotion", "projectPath", project.Path, "skipped", copySkipped)
		}
	} else if err := s.seedStageEnvFromCandidateDirInternal(ctx, sync, projectsDir, stagePath); err != nil {
		_ = os.RemoveAll(stagePath)
		return nil, err
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
	if err := projects.RemoveStaleComposeFiles(stagePath, composeFileName, syncedFiles); err != nil {
		_ = os.RemoveAll(stagePath)
		return nil, fmt.Errorf("failed to remove stale compose files: %w", err)
	}

	contentsChanged := true
	if project != nil {
		contentsChanged, err = projects.DirectorySyncContentsChanged(project.Path, syncFiles, oldSyncedFiles, composeFileName)
		if err != nil {
			_ = os.RemoveAll(stagePath)
			return nil, fmt.Errorf("failed to compare staged directory changes: %w", err)
		}
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
		copySkipped:     copySkipped,
	}, nil
}

// seedStageEnvFromCandidateDirInternal copies env files from a pre-existing project
// directory at the conventional path (projectsDir/<sanitized-sync-name>/) into the
// staging directory before initial-sync validation. This lets users pre-seed
// ${VAR} substitutions via a server-side .env when the compose file in git
// expects values that the git repo intentionally does not provide. Only env
// files are touched — other files would conflict with what WriteSyncedDirectory
// is about to lay down from git.
func (s *GitOpsSyncService) seedStageEnvFromCandidateDirInternal(ctx context.Context, sync *models.GitOpsSync, projectsDir, stagePath string) error {
	candidatePath := filepath.Join(projectsDir, projects.SanitizeProjectName(sync.ProjectName))
	info, err := os.Stat(candidatePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("inspect pre-existing project directory %s: %w", candidatePath, err)
	}
	if !info.IsDir() {
		return nil
	}

	readOptional := func(name string) (string, bool, error) {
		content, readErr := os.ReadFile(filepath.Join(candidatePath, name))
		if readErr == nil {
			return string(content), true, nil
		}
		if errors.Is(readErr, os.ErrNotExist) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("read %s from %s: %w", name, candidatePath, readErr)
	}

	effective, hasEffective, err := readOptional(projects.EffectiveEnvFileName)
	if err != nil {
		return err
	}
	gitSource, hasGit, err := readOptional(projects.GitSourceEnvFileName)
	if err != nil {
		return err
	}
	override, hasOverride, err := readOptional(projects.OverrideEnvFileName)
	if err != nil {
		return err
	}
	if !hasEffective && !hasGit && !hasOverride {
		return nil
	}

	if hasGit {
		if err := projects.WriteProjectFile(projectsDir, stagePath, projects.GitSourceEnvFileName, gitSource); err != nil {
			return fmt.Errorf("seed stage .env.git: %w", err)
		}
	}
	if hasOverride {
		if err := projects.WriteProjectFile(projectsDir, stagePath, projects.OverrideEnvFileName, override); err != nil {
			return fmt.Errorf("seed stage project.env: %w", err)
		}
	}

	switch {
	case hasEffective:
		if err := projects.WriteEnvFile(projectsDir, stagePath, effective); err != nil {
			return fmt.Errorf("seed stage .env: %w", err)
		}
	case hasGit || hasOverride:
		merged, mergeErr := projects.BuildEffectiveEnvContent(gitSource, override)
		if mergeErr != nil {
			return fmt.Errorf("build effective env from pre-existing project: %w", mergeErr)
		}
		if err := projects.WriteEnvFile(projectsDir, stagePath, merged); err != nil {
			return fmt.Errorf("seed stage .env: %w", err)
		}
	}

	slog.DebugContext(ctx, "Seeded GitOps stage with pre-existing project env files",
		"candidatePath", candidatePath,
		"hasEffective", hasEffective,
		"hasGit", hasGit,
		"hasOverride", hasOverride,
	)
	return nil
}

func (s *GitOpsSyncService) lookupProjectByIDInternal(ctx context.Context, projectID string) (*models.Project, bool, error) {
	var project models.Project
	if err := s.db.WithContext(ctx).Where("id = ?", projectID).First(&project).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("failed to get project %s: %w", projectID, err)
	}

	return &project, true, nil
}

func (s *GitOpsSyncService) lookupProjectByPathInternal(ctx context.Context, projectPath string) (*models.Project, bool, error) {
	var project models.Project
	if err := s.db.WithContext(ctx).Where("path = ?", projectPath).First(&project).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("failed to get project by path %s: %w", projectPath, err)
	}

	return &project, true, nil
}

func (s *GitOpsSyncService) ensureDirectorySyncProjectLinkedInternal(ctx context.Context, sync *models.GitOpsSync, project *models.Project) error {
	if sync == nil || project == nil {
		return nil
	}

	if project.GitOpsManagedBy != nil && *project.GitOpsManagedBy != "" && *project.GitOpsManagedBy != sync.ID {
		return fmt.Errorf("project %s is already managed by a different GitOps sync", project.ID)
	}

	if sync.ProjectID != nil && *sync.ProjectID == project.ID && project.GitOpsManagedBy != nil && *project.GitOpsManagedBy == sync.ID {
		s.projectService.cacheComposeProjectIDInternal(normalizeComposeProjectName(project.Name), project.ID)
		return nil
	}

	updatesSync := map[string]any{}
	updatesProject := map[string]any{}
	if sync.ProjectID == nil || *sync.ProjectID != project.ID {
		updatesSync["project_id"] = project.ID
	}
	if project.GitOpsManagedBy == nil || *project.GitOpsManagedBy != sync.ID {
		updatesProject["gitops_managed_by"] = sync.ID
	}

	if len(updatesSync) == 0 && len(updatesProject) == 0 {
		s.projectService.cacheComposeProjectIDInternal(normalizeComposeProjectName(project.Name), project.ID)
		return nil
	}

	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if len(updatesSync) > 0 {
			if err := tx.Model(&models.GitOpsSync{}).Where("id = ?", sync.ID).Updates(updatesSync).Error; err != nil {
				return fmt.Errorf("failed to relink GitOps sync %s: %w", sync.ID, err)
			}
		}
		if len(updatesProject) > 0 {
			if err := tx.Model(&models.Project{}).Where("id = ?", project.ID).Updates(updatesProject).Error; err != nil {
				return fmt.Errorf("failed to relink project %s to GitOps sync %s: %w", project.ID, sync.ID, err)
			}
		}
		return nil
	}); err != nil {
		return err
	}

	sync.ProjectID = &project.ID
	project.GitOpsManagedBy = &sync.ID
	s.projectService.cacheComposeProjectIDInternal(normalizeComposeProjectName(project.Name), project.ID)

	return nil
}

func (s *GitOpsSyncService) findRecoverableManagedProjectInternal(ctx context.Context, sync *models.GitOpsSync) (*models.Project, error) {
	var managedProjects []models.Project
	if err := s.db.WithContext(ctx).
		Where("gitops_managed_by = ?", sync.ID).
		Find(&managedProjects).Error; err != nil {
		return nil, fmt.Errorf("failed to list GitOps-managed projects for sync %s: %w", sync.ID, err)
	}

	matches := make([]models.Project, 0, len(managedProjects))
	for i := range managedProjects {
		project := managedProjects[i]
		if err := s.projectService.ensureProjectPathUnderRoot(ctx, &project, true); err != nil {
			return nil, err
		}
		if _, err := s.projectService.resolveProjectComposeFileInternal(ctx, &project); err != nil {
			if _, ok := errors.AsType[*common.ProjectComposeFileNotFoundError](err); ok {
				continue
			}
			return nil, err
		}
		matches = append(matches, project)
	}

	switch len(matches) {
	case 0:
		return nil, nil
	case 1:
		return &matches[0], nil
	default:
		return nil, fmt.Errorf("multiple GitOps-managed projects match sync %s; refusing automatic relink", sync.ID)
	}
}

func (s *GitOpsSyncService) findUniqueProjectDirectoryCandidateInternal(ctx context.Context, sync *models.GitOpsSync) (string, error) {
	projectsDir, err := s.projectService.getProjectsDirectoryInternal(ctx)
	if err != nil {
		return "", err
	}

	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return "", fmt.Errorf("failed to list projects directory %s: %w", projectsDir, err)
	}

	composeFileName := strings.TrimSpace(filepath.Base(sync.ComposePath))
	if composeFileName == "" || composeFileName == "." {
		return "", nil
	}

	prefix := projects.SanitizeProjectName(sync.ProjectName)
	matches := make([]string, 0, 1)
	for _, entry := range entries {
		candidatePath := filepath.Join(projectsDir, entry.Name())
		if !projects.IsProjectDirectoryEntry(entry, candidatePath, false) {
			continue
		}
		if prefix != "" && entry.Name() != prefix && !strings.HasPrefix(entry.Name(), prefix+"-") {
			continue
		}

		composePath := filepath.Join(candidatePath, composeFileName)
		if info, statErr := os.Stat(composePath); statErr == nil {
			if !info.IsDir() {
				matches = append(matches, candidatePath)
			}
		} else if !os.IsNotExist(statErr) {
			return "", fmt.Errorf("failed to inspect recovery candidate %s: %w", composePath, statErr)
		}
	}

	switch len(matches) {
	case 0:
		return "", nil
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("multiple candidate project directories match sync %s; refusing automatic relink", sync.ID)
	}
}

func (s *GitOpsSyncService) createRecoveredProjectFromDirectoryInternal(ctx context.Context, sync *models.GitOpsSync, projectPath string) (*models.Project, error) {
	project := &models.Project{
		Name:            sync.ProjectName,
		DirName:         new(filepath.Base(projectPath)),
		Path:            projectPath,
		Status:          models.ProjectStatusUnknown,
		StatusReason:    new("Project recovered from existing GitOps-managed directory"),
		ServiceCount:    0,
		RunningCount:    0,
		GitOpsManagedBy: &sync.ID,
	}

	if serviceCount, err := s.projectService.countServicesFromCompose(ctx, *project); err == nil {
		project.ServiceCount = serviceCount
	} else {
		slog.WarnContext(ctx, "Failed to count services while recovering GitOps project", "syncId", sync.ID, "path", projectPath, "error", err)
	}

	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(project).Error; err != nil {
			return fmt.Errorf("failed to create recovered project for sync %s: %w", sync.ID, err)
		}

		if err := tx.Model(&models.GitOpsSync{}).Where("id = ?", sync.ID).Update("project_id", project.ID).Error; err != nil {
			return fmt.Errorf("failed to relink sync %s to recovered project %s: %w", sync.ID, project.ID, err)
		}

		return nil
	}); err != nil {
		return nil, err
	}

	sync.ProjectID = &project.ID
	s.projectService.cacheComposeProjectIDInternal(normalizeComposeProjectName(project.Name), project.ID)

	return project, nil
}

func (s *GitOpsSyncService) recoverProjectFromDirectoryCandidateInternal(ctx context.Context, sync *models.GitOpsSync) (*models.Project, error) {
	projectPath, err := s.findUniqueProjectDirectoryCandidateInternal(ctx, sync)
	if err != nil || projectPath == "" {
		return nil, err
	}

	project, found, err := s.lookupProjectByPathInternal(ctx, projectPath)
	if err != nil {
		return nil, err
	}
	if found {
		if err := s.projectService.ensureProjectPathUnderRoot(ctx, project, true); err != nil {
			return nil, err
		}
		if err := s.ensureDirectorySyncProjectLinkedInternal(ctx, sync, project); err != nil {
			return nil, err
		}
		return project, nil
	}

	return s.createRecoveredProjectFromDirectoryInternal(ctx, sync, projectPath)
}

// getDirectorySyncProjectInternal resolves the linked project for a sync when one
// exists, while tolerating deleted/stale project references.
func (s *GitOpsSyncService) getDirectorySyncProjectInternal(ctx context.Context, sync *models.GitOpsSync) (*models.Project, error) {
	if sync == nil {
		return nil, nil
	}

	establishedBinding := hasEstablishedProjectBindingInternal(sync)
	if establishedBinding {
		project, found, err := s.lookupProjectByIDInternal(ctx, *sync.ProjectID)
		if err != nil {
			return nil, err
		}
		if found {
			if err := s.projectService.ensureProjectPathUnderRoot(ctx, project, true); err != nil {
				return nil, err
			}
			if err := s.ensureDirectorySyncProjectLinkedInternal(ctx, sync, project); err != nil {
				return nil, err
			}
			return project, nil
		}

		slog.WarnContext(ctx, "Existing project not found, attempting recovery", "projectId", *sync.ProjectID, "syncId", sync.ID)
	}

	project, err := s.findRecoverableManagedProjectInternal(ctx, sync)
	if err != nil {
		if establishedBinding {
			return nil, &common.GitOpsSyncProjectBindingBrokenError{
				Err: fmt.Errorf("sync %s references missing project %s: %w", sync.ID, *sync.ProjectID, err),
			}
		}
		return nil, err
	}
	if project != nil {
		if err := s.ensureDirectorySyncProjectLinkedInternal(ctx, sync, project); err != nil {
			return nil, err
		}
		return project, nil
	}

	project, err = s.recoverProjectFromDirectoryCandidateInternal(ctx, sync)
	if err != nil {
		if establishedBinding {
			return nil, &common.GitOpsSyncProjectBindingBrokenError{
				Err: fmt.Errorf("sync %s references missing project %s: %w", sync.ID, *sync.ProjectID, err),
			}
		}
		return nil, err
	}
	if project != nil {
		return project, nil
	}

	if establishedBinding {
		return nil, &common.GitOpsSyncProjectBindingBrokenError{
			Err: fmt.Errorf("sync %s references missing project %s: no unique recovery candidate was found", sync.ID, *sync.ProjectID),
		}
	}

	return nil, nil
}

// validateDirectorySyncStageInternal loads the staged compose project using the real
// synced compose filename so include/env_file resolution happens against the
// fully copied directory contents.
func (s *GitOpsSyncService) validateDirectorySyncStageInternal(ctx context.Context, projectName, stagePath, composeFileName string) (int, error) {
	projectsDir, err := s.projectService.getProjectsDirectoryInternal(ctx)
	if err != nil {
		return 0, err
	}

	pathMapper := s.projectService.getPathMapperInternal(ctx)

	autoInjectEnv := s.settingsService.GetBoolSetting(ctx, "autoInjectEnv", false)
	project, err := projects.LoadComposeProjectLenient(
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

// updateDirectorySyncProjectInternal mirrors a validated staged tree into the
// existing project path in place so running containers keep their bind-mount
// inodes; a temporary backup copy allows rollback if promotion fails.
func (s *GitOpsSyncService) updateDirectorySyncProjectInternal(ctx context.Context, sync *models.GitOpsSync, stage *stagedDirectorySync) (*models.Project, error) {
	project := stage.project
	projectPath := filepath.Clean(project.Path)
	backupPath := ""
	existed := true

	if info, err := os.Stat(projectPath); err == nil {
		if !info.IsDir() {
			return nil, fmt.Errorf("project path is not a directory: %s", projectPath)
		}
	} else if errors.Is(err, os.ErrNotExist) {
		existed = false
		if err := os.MkdirAll(projectPath, 0o755); err != nil {
			return nil, fmt.Errorf("failed to recreate project directory: %w", err)
		}
	} else {
		return nil, fmt.Errorf("failed to inspect current project directory: %w", err)
	}

	var backupSkipped []string
	if existed {
		projectsDir, err := s.projectService.getProjectsDirectoryInternal(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get projects directory: %w", err)
		}
		backupPath, err = os.MkdirTemp(projectsDir, ".gitops-backup-*")
		if err != nil {
			return nil, fmt.Errorf("failed to create backup directory: %w", err)
		}
		defer func() { _ = os.RemoveAll(backupPath) }()
		// Tolerate unreadable files (e.g. foreign-owned bind-mount data) so an
		// unrelated file can't block the backup; the skipped paths are absent from
		// the backup and must be preserved, not pruned, when restoring.
		backupSkipped, err = projects.CopyDirectoryContentsTolerant(projectPath, backupPath)
		if err != nil {
			return nil, fmt.Errorf("failed to back up current project directory: %w", err)
		}
		if len(backupSkipped) > 0 {
			slog.WarnContext(ctx, "skipped unreadable files while backing up project for sync; they will be left untouched on rollback", "projectPath", projectPath, "skipped", backupSkipped)
		}
	}

	restore := func() {
		var restoreErr error
		if existed {
			restoreErr = projects.MirrorDirectoryContentsPreserving(backupPath, projectPath, backupSkipped)
		} else {
			restoreErr = os.RemoveAll(projectPath)
		}
		if restoreErr != nil {
			slog.ErrorContext(ctx, "Failed to restore project directory after sync promotion failure; directory may be in a mixed state", "projectPath", projectPath, "backupPath", backupPath, "error", restoreErr)
		}
	}

	// Preserve files skipped while staging: they remain in the live project but are
	// absent from the stage, so a plain mirror would prune them.
	if err := projects.MirrorDirectoryContentsPreserving(stage.stagePath, projectPath, stage.copySkipped); err != nil {
		restore()
		return nil, fmt.Errorf("failed to promote staged project directory: %w", err)
	}

	if err := s.db.WithContext(ctx).Model(&models.Project{}).Where("id = ?", project.ID).Updates(map[string]any{
		"service_count":     stage.serviceCount,
		"gitops_managed_by": sync.ID,
		"updated_at":        time.Now(),
	}).Error; err != nil {
		restore()
		return nil, fmt.Errorf("failed to update project metadata after directory sync: %w", err)
	}

	return project, nil
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
