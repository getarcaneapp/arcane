package gitops

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/gitops"
	"github.com/getarcaneapp/arcane/types/v2/lifecycle"
	"github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/getarcaneapp/arcane/types/v2/user"
	"github.com/italypaleale/francis/builtin/workflow"
	"go.getarcane.app/acfs"
	"go.getarcane.app/kit/pkg"
	"go.getarcane.app/kit/pkg/mapping"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/gitops/children/backup"
	gitopssync "github.com/getarcaneapp/arcane/backend/v2/internal/gitops/children/sync"
	"github.com/getarcaneapp/arcane/backend/v2/internal/gitrepo"
	projectpkg "github.com/getarcaneapp/arcane/backend/v2/internal/project"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/internal/swarm"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/entityjobs"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/flow"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/jobcontext"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/runs"
)

const (
	defaultGitSyncTimeout = 5 * time.Minute

	defaultMaxSyncFiles        = 500
	defaultMaxSyncTotalSizeMB  = 50
	defaultMaxSyncBinarySizeMB = 10

	gitOpsSyncAdmissionScope = "gitops-sync"
)

type (
	GitOpsSyncService struct {
		db              *database.DB
		repoService     *gitrepo.GitRepositoryService
		projectService  *projectpkg.ProjectService
		swarmService    *swarm.SwarmService
		eventService    *event.EventService
		settingsService *settings.SettingsService

		// jobs carries the scheduler and app lifecycle context, injected after construction by SetScheduler.
		jobs         *entityjobs.Registry
		engine       *flow.Engine
		syncWorkflow *flow.Workflow

		backups *backupRuntime
		sync    *gitopssync.Service
		backup  *backup.Service
	}

	// syncTarget is the gitops-sync workflow input: the sync one scheduled job runs.
	syncTarget struct {
		EnvironmentID string `json:"environmentId"`
		SyncID        string `json:"syncId"`
	}

	// lifecycleConfigInput is the pre-deploy hook slice of a create or update request.
	lifecycleConfigInput struct {
		targetType    *string
		scriptPath    *string
		runnerImage   *string
		env           *string
		extraMounts   *string
		timeoutSec    *int
		networkMode   *string
		syncDirectory *bool
	}

	// backupRuntime holds the in-memory coordination for backup syncs.
	backupRuntime struct {
		mu       sync.Mutex
		branches map[string]*sync.Mutex
		timers   map[string]*time.Timer
		debounce time.Duration
	}
)

func (s *GitOpsSyncService) validateLifecycleConfig(ctx context.Context, current *projectpkg.GitOpsSync, in lifecycleConfigInput) error {
	var currentScript, currentRunner *string
	currentTargetType, currentSyncDirectory := "", false
	if current != nil {
		currentScript, currentRunner = current.PreDeployScriptPath, current.PreDeployRunnerImage
		currentTargetType, currentSyncDirectory = strings.TrimSpace(current.TargetType), current.SyncDirectory
	}
	currentTargetType = cmp.Or(currentTargetType, "project")
	targetType := cmp.Or(strings.TrimSpace(kit.FromPtr(in.targetType)), currentTargetType)
	scriptPath := strings.TrimSpace(kit.FromPtr(cmp.Or(in.scriptPath, currentScript)))
	invalid := func(field, message string) error {
		return common.Classify(common.ErrValidation, &base.FieldError{Field: field, Err: errors.New(message)})
	}

	lifecycleFieldSet := in.scriptPath != nil || in.runnerImage != nil || in.env != nil || in.extraMounts != nil || in.timeoutSec != nil || in.networkMode != nil
	targetTypeChanging := in.targetType != nil && strings.TrimSpace(*in.targetType) != currentTargetType
	// Nothing lifecycle-related changes, or the result has no script to validate.
	if !lifecycleFieldSet && (scriptPath == "" || (in.syncDirectory == nil && !targetTypeChanging)) {
		return nil
	}
	// The kill switch only blocks edits to lifecycle fields, not unrelated edits to a sync that already has a hook.
	if lifecycleFieldSet && !s.settingsService.GetBoolSetting(ctx, "lifecycleEnabled", false) {
		return invalid("preDeployScriptPath", "Pre-deploy lifecycle hooks are disabled. An admin must enable lifecycleEnabled in settings before they can be configured.")
	}

	// scriptPath is a POSIX repo path, so path.IsAbs behaves the same on Windows.
	cleanedScript := filepath.ToSlash(filepath.Clean(scriptPath))
	switch {
	case scriptPath == "":
	case targetType == "swarm_stack":
		return invalid("preDeployScriptPath", "Pre-deploy lifecycle hooks are only supported for project syncs.")
	case len(scriptPath) > 256:
		return invalid("preDeployScriptPath", "Script path must be 256 characters or fewer.")
	case path.IsAbs(filepath.ToSlash(scriptPath)):
		return invalid("preDeployScriptPath", "Script path must be relative to the project directory.")
	case cleanedScript == ".." || strings.HasPrefix(cleanedScript, "../"):
		return invalid("preDeployScriptPath", "Script path must not escape the project directory.")
	case strings.TrimSpace(kit.FromPtr(cmp.Or(in.runnerImage, currentRunner))) == "" && strings.TrimSpace(s.settingsService.GetStringSetting(ctx, "lifecycleDefaultRunnerImage", "")) == "":
		return invalid("preDeployRunnerImage", "Runner image is required when a script path is set.")
	case !kit.FromPtr(cmp.Or(in.syncDirectory, &currentSyncDirectory)):
		return invalid("preDeployScriptPath", "Pre-deploy script requires \"Sync entire directory\" so the script is included in the synced files.")
	}

	if in.timeoutSec != nil {
		maxTimeoutSec := s.settingsService.GetIntSetting(ctx, "lifecycleMaxTimeoutSec", lifecycle.DefaultMaxTimeoutSec)
		if *in.timeoutSec < 1 {
			return invalid("preDeployTimeoutSec", "Timeout must be at least 1 second.")
		}
		if maxTimeoutSec > 0 && *in.timeoutSec > maxTimeoutSec {
			return invalid("preDeployTimeoutSec", fmt.Sprintf("Timeout %ds exceeds the lifecycleMaxTimeoutSec setting (%ds).", *in.timeoutSec, maxTimeoutSec))
		}
	}
	if _, err := projectpkg.ParseEnvText(in.env); err != nil {
		return invalid("preDeployEnv", err.Error())
	}
	if _, err := projectpkg.ParseExtraMountsText(in.extraMounts); err != nil {
		return invalid("preDeployExtraMounts", err.Error())
	}
	return nil
}

func NewGitOpsSyncService(
	db *database.DB,
	repoService *gitrepo.GitRepositoryService,
	projectService *projectpkg.ProjectService,
	swarmService *swarm.SwarmService,
	eventService *event.EventService,
	settingsService *settings.SettingsService,
) *GitOpsSyncService {
	s := &GitOpsSyncService{
		db:              db,
		repoService:     repoService,
		projectService:  projectService,
		swarmService:    swarmService,
		eventService:    eventService,
		settingsService: settingsService,
		jobs:            entityjobs.New(entityjobs.GitOpsSyncJobPrefix, gitOpsSyncAdmissionScope),
		backups:         &backupRuntime{branches: map[string]*sync.Mutex{}, timers: map[string]*time.Timer{}, debounce: backup.DefaultBackupSaveDebounce},
	}
	logError := func(ctx context.Context, syncRecord *projectpkg.GitOpsSync, actor user.Actor, errorMsg string) {
		_, _ = s.eventService.CreateEvent(ctx, event.CreateEventRequest{
			Type:          event.EventTypeGitSyncError,
			Severity:      event.EventSeverityError,
			Title:         "Git sync failed",
			Description:   fmt.Sprintf("Failed to sync '%s': %s", syncRecord.Name, errorMsg),
			ResourceType:  new("git_sync"),
			ResourceID:    new(syncRecord.ID),
			ResourceName:  new(syncRecord.Name),
			UserID:        new(actor.ID),
			Username:      new(actor.Username),
			EnvironmentID: new(syncRecord.EnvironmentID),
		})
	}
	s.sync = gitopssync.New(db, repoService, projectService, swarmService, eventService, s.getEffectiveSyncLimits, logError, s.jobs.Unregister)
	s.backup = backup.New(db, repoService, projectService, eventService, s.getEffectiveSyncLimits, logError, s.backups.branchLock)
	return s
}

// RegisterWorkflows defines the gitops-sync workflow each scheduled sync runs. A sync step retries transient
// clone failures; a retry replays the run's pinned revision.
func (s *GitOpsSyncService) RegisterWorkflows(engine *flow.Engine) error {
	s.engine = engine
	syncWorkflow, err := engine.Define(flow.Definition{
		Name:        "gitops-sync",
		Version:     1,
		Fingerprint: "f693aa29ca10c373e1b9d39ab25dee6e396ced7cea2bcfef76f41dba571d576a",
		Concurrency: 16,
		Timeout:     time.Hour,
		Steps: []workflow.StepSpec{
			workflow.Step("sync", engine.Handler(func(ctx context.Context, t flow.Task) (any, error) {
				var target syncTarget
				if err := t.Payload(&target); err != nil {
					return nil, err
				}
				// The row is re-read on every run, so a deleted sync unregisters itself and a disabled one skips.
				syncRecord, err := s.getSyncByID(ctx, target.EnvironmentID, target.SyncID, false)
				if errors.Is(err, common.ErrNotFound) {
					slog.InfoContext(ctx, "gitops auto-sync job unregistering; sync no longer exists", "syncId", target.SyncID)
					s.jobs.Unregister(ctx, target.SyncID)
					return scheduler.Outcome{Status: scheduler.Skipped}, nil
				}
				if err != nil {
					return nil, common.Classify(common.ErrUnavailable, err)
				}
				if !syncRecord.AutoSync {
					return scheduler.Outcome{Status: scheduler.Skipped}, nil
				}
				if previous, ok := jobcontext.Run(ctx); ok {
					if outcome := jobcontext.ConfirmedTarget(previous, target.SyncID); outcome.Status == scheduler.Succeeded {
						return outcome, nil
					}
				}
				if runningErr := jobcontext.Progress(ctx, scheduler.TargetOutcome{ID: target.SyncID, Status: scheduler.Running}); runningErr != nil {
					return nil, runningErr
				}
				result, err := s.PerformSync(ctx, target.EnvironmentID, target.SyncID, user.SystemUser)
				if err != nil {
					slog.ErrorContext(ctx, "gitops auto-sync run failed", "syncId", target.SyncID, "error", err)
					return nil, err
				}
				if !result.Success {
					return scheduler.Outcome{Status: scheduler.Skipped, Message: result.Message}, nil
				}
				if succeededErr := jobcontext.Progress(ctx, scheduler.TargetOutcome{ID: target.SyncID, Status: scheduler.Succeeded}); succeededErr != nil {
					return nil, succeededErr
				}
				return scheduler.Outcome{Status: scheduler.Succeeded, Message: result.Message}, nil
			}), workflow.WithMaxAttempts(3), workflow.WithRetryBackoff(30*time.Second, 2*time.Minute)),
		},
	})
	s.syncWorkflow = syncWorkflow
	return err
}

// SetScheduler injects the scheduler, the admission gate, and the app lifecycle context background syncs run on.
// It must run during bootstrap, before any per-sync job is registered.
func (s *GitOpsSyncService) SetScheduler(ctx context.Context, jobScheduler scheduler.DynamicScheduler, admissionGate *runs.Admission) error {
	return s.jobs.SetScheduler(ctx, jobScheduler, admissionGate)
}

// registerSyncJob schedules one sync every intervalMinutes as a gitops-sync workflow run.
func (s *GitOpsSyncService) registerSyncJob(ctx context.Context, syncID, environmentID string, intervalMinutes int) {
	schedule := fmt.Sprintf("@every %dm", max(intervalMinutes, 1))
	s.jobs.Add(ctx, &flow.Job{
		Engine:     s.engine,
		Workflow:   s.syncWorkflow,
		JobName:    s.jobs.JobName(syncID),
		Payload:    syncTarget{EnvironmentID: environmentID, SyncID: syncID},
		ScheduleFn: func(context.Context) string { return schedule },
	})
}

// kickSync submits one run of a sync's job now, so it does not wait for its next interval.
func (s *GitOpsSyncService) kickSync(ctx context.Context, syncID, trigger string) {
	if !s.jobs.Enabled() {
		return
	}
	if _, err := s.jobs.Scheduler().Submit(ctx, scheduler.Request{JobID: s.jobs.JobName(syncID), EnvironmentID: "0", Trigger: trigger}); err != nil {
		slog.ErrorContext(ctx, "gitops sync admission failed", "syncId", syncID, "trigger", trigger, "error", err)
	}
}

// RegisterAutoSyncJobsOnStartup registers a job for every auto-sync and kicks the overdue ones, and backups
// with pending changes, right away.
func (s *GitOpsSyncService) RegisterAutoSyncJobsOnStartup(ctx context.Context) {
	if !s.jobs.Enabled() {
		return
	}
	var syncs []projectpkg.GitOpsSync
	if err := s.db.WithContext(ctx).
		Where("auto_sync = ? AND environment_id IN (SELECT id FROM environments)", true).
		Find(&syncs).Error; err != nil {
		slog.ErrorContext(ctx, "Failed to load auto-sync jobs on startup", "error", err)
		return
	}
	for _, syncRecord := range syncs {
		s.registerSyncJob(ctx, syncRecord.ID, syncRecord.EnvironmentID, syncRecord.SyncInterval)
		overdue := syncRecord.LastSyncAt == nil || time.Since(*syncRecord.LastSyncAt) > time.Duration(max(syncRecord.SyncInterval, 1))*time.Minute
		if overdue || (syncRecord.Mode == gitops.SyncModeBackup && syncRecord.BackupPending) {
			s.kickSync(ctx, syncRecord.ID, "startup")
		}
	}
	slog.InfoContext(ctx, "Registered gitops auto-sync jobs on startup", "count", len(syncs))
}

func (s *GitOpsSyncService) getEnvironmentSyncLimits(ctx context.Context) (int, int64, int64) {
	cfg := s.settingsService.GetSettingsOrDefaults(ctx)
	maxFiles := normalizeSyncLimitSetting(kit.ParseOrDefault(cfg.GitSyncMaxFiles.Value, defaultMaxSyncFiles, strconv.Atoi), defaultMaxSyncFiles)
	maxTotalSizeMB := normalizeSyncLimitSetting(kit.ParseOrDefault(cfg.GitSyncMaxTotalSizeMb.Value, defaultMaxSyncTotalSizeMB, strconv.Atoi), defaultMaxSyncTotalSizeMB)
	maxBinarySizeMB := normalizeSyncLimitSetting(kit.ParseOrDefault(cfg.GitSyncMaxBinarySizeMb.Value, defaultMaxSyncBinarySizeMB, strconv.Atoi), defaultMaxSyncBinarySizeMB)
	return maxFiles, int64(maxTotalSizeMB) << 20, int64(maxBinarySizeMB) << 20
}

// getEffectiveSyncLimits returns the sync's own limits, except where an environment override pins the setting.
func (s *GitOpsSyncService) getEffectiveSyncLimits(ctx context.Context, syncRecord *projectpkg.GitOpsSync) (int, int64, int64) {
	maxFiles, maxTotalSize, maxBinarySize := s.getEnvironmentSyncLimits(ctx)
	if syncRecord == nil {
		return maxFiles, maxTotalSize, maxBinarySize
	}
	return kit.Ternary(s.settingsService.IsEnvOverrideActive("gitSyncMaxFiles"), maxFiles, syncRecord.MaxSyncFiles),
		kit.Ternary(s.settingsService.IsEnvOverrideActive("gitSyncMaxTotalSizeMb"), maxTotalSize, syncRecord.MaxSyncTotalSize),
		kit.Ternary(s.settingsService.IsEnvOverrideActive("gitSyncMaxBinarySizeMb"), maxBinarySize, syncRecord.MaxSyncBinarySize)
}

func (s *GitOpsSyncService) GetSyncsPaginated(ctx context.Context, environmentID string, params pagination.QueryParams) ([]gitops.GitOpsSync, pagination.Response, gitops.SyncCounts, error) {
	var syncs []projectpkg.GitOpsSync
	q := s.db.WithContext(ctx).Model(&projectpkg.GitOpsSync{}).
		Where("environment_id = ?", environmentID)

	if term := strings.TrimSpace(params.Search); term != "" {
		searchPattern := "%" + term + "%"
		q = q.Where(
			"name LIKE ? OR branch LIKE ? OR compose_path LIKE ? OR backup_directory LIKE ?",
			searchPattern, searchPattern, searchPattern, searchPattern,
		)
	}

	q = pagination.ApplyBooleanFilter(q, "auto_sync", params.Filters["autoSync"])
	q = pagination.ApplyFilter(q, "mode", params.Filters["mode"])

	q = pagination.ApplyFilter(q, "repository_id", params.Filters["repositoryId"])
	q = pagination.ApplyFilter(q, "project_id", params.Filters["projectId"])

	var total, active, successful, backups int64
	if err := errors.Join(
		q.Session(&gorm.Session{}).Count(&total).Error,
		q.Session(&gorm.Session{}).Where("auto_sync = ?", true).Count(&active).Error,
		q.Session(&gorm.Session{}).Where("last_sync_status = ?", "success").Count(&successful).Error,
		q.Session(&gorm.Session{}).Where("mode = ?", gitops.SyncModeBackup).Count(&backups).Error,
	); err != nil {
		return nil, pagination.Response{}, gitops.SyncCounts{}, fmt.Errorf("failed to get sync counts: %w", err)
	}
	counts := gitops.SyncCounts{
		TotalSyncs:      int(total),
		ActiveSyncs:     int(active),
		SuccessfulSyncs: int(successful),
		DeploySyncs:     int(total - backups),
		BackupSyncs:     int(backups),
	}

	paginationResp, err := pagination.PaginateAndSortDB(params, q.Preload("Repository").Preload("Project"), &syncs)
	if err != nil {
		return nil, pagination.Response{}, gitops.SyncCounts{}, fmt.Errorf("failed to paginate gitops syncs: %w", err)
	}

	out, mapErr := mapping.MapSlice[projectpkg.GitOpsSync, gitops.GitOpsSync](syncs)
	if mapErr != nil {
		return nil, pagination.Response{}, gitops.SyncCounts{}, fmt.Errorf("failed to map syncs: %w", mapErr)
	}

	return out, paginationResp, counts, nil
}

func (s *GitOpsSyncService) GetSyncByID(ctx context.Context, environmentID, id string) (*projectpkg.GitOpsSync, error) {
	syncRecord, err := s.getSyncByID(ctx, environmentID, id, true)
	if err != nil {
		if errors.Is(err, common.ErrNotFound) {
			slog.WarnContext(ctx, "GitOps sync not found", "syncId", id, "environmentId", environmentID)
			return nil, err
		}
		slog.ErrorContext(ctx, "Failed to get GitOps sync", "syncId", id, "environmentId", environmentID, "error", err)
		return nil, err
	}
	return syncRecord, nil
}

func (s *GitOpsSyncService) getSyncByID(ctx context.Context, environmentID, id string, preloadAssociations bool) (*projectpkg.GitOpsSync, error) {
	var syncRecord projectpkg.GitOpsSync
	q := s.db.WithContext(ctx).Where("id = ?", id)
	if preloadAssociations {
		q = q.Preload("Repository").Preload("Project")
	}
	if environmentID != "" {
		q = q.Where("environment_id = ?", environmentID)
	}
	if err := q.First(&syncRecord).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.Classify(common.ErrNotFound, errors.New("GitOps sync not found"))
		}
		return nil, fmt.Errorf("failed to get sync: %w", err)
	}
	return &syncRecord, nil
}

func (s *GitOpsSyncService) CreateSync(ctx context.Context, environmentID string, req gitops.CreateSyncRequest, actor user.Actor) (*projectpkg.GitOpsSync, error) {
	slog.InfoContext(ctx, "Creating GitOps sync", "environmentId", environmentID, "name", req.Name, "repositoryId", req.RepositoryID)

	mode := cmp.Or(strings.TrimSpace(req.Mode), gitops.SyncModeDeploy)
	switch {
	case mode != gitops.SyncModeDeploy && mode != gitops.SyncModeBackup:
		return nil, common.Classify(common.ErrValidation, &base.FieldError{Field: "mode", Err: fmt.Errorf("unsupported sync mode %q", req.Mode)})
	case mode == gitops.SyncModeDeploy && req.HasBackupOptions():
		return nil, common.Classify(common.ErrValidation, &base.FieldError{Field: "mode", Err: errors.New("backup options require mode \"backup\"")})
	case mode == gitops.SyncModeDeploy && strings.TrimSpace(req.ComposePath) == "":
		return nil, common.Classify(common.ErrValidation, &base.FieldError{Field: "composePath", Err: errors.New("compose path is required")})
	}

	repo, err := s.repoService.GetRepositoryByID(ctx, req.RepositoryID)
	if err != nil {
		slog.ErrorContext(ctx, "Repository not found for GitOps sync", "repositoryId", req.RepositoryID, "error", err)
		return nil, fmt.Errorf("repository not found: %w", err)
	}
	slog.InfoContext(ctx, "Found repository for GitOps sync", "repositoryId", req.RepositoryID, "repositoryName", repo.Name)

	if limitsErr := validateSyncLimits(req.MaxSyncFiles, req.MaxSyncTotalSize, req.MaxSyncBinarySize); limitsErr != nil {
		return nil, limitsErr
	}
	lifecycleCfg := lifecycleConfigInput{
		targetType:    &req.TargetType,
		scriptPath:    req.PreDeployScriptPath,
		runnerImage:   req.PreDeployRunnerImage,
		env:           req.PreDeployEnv,
		extraMounts:   req.PreDeployExtraMounts,
		timeoutSec:    req.PreDeployTimeoutSec,
		networkMode:   req.PreDeployNetworkMode,
		syncDirectory: req.SyncDirectory,
	}
	if lifecycleErr := s.validateLifecycleConfig(ctx, nil, lifecycleCfg); lifecycleErr != nil {
		return nil, lifecycleErr
	}

	defaultMaxFiles, defaultMaxTotalSize, defaultMaxBinarySize := s.getEnvironmentSyncLimits(ctx)
	syncRecord := projectpkg.GitOpsSync{
		Name:                 req.Name,
		EnvironmentID:        environmentID,
		RepositoryID:         req.RepositoryID,
		Branch:               req.Branch,
		ComposePath:          req.ComposePath,
		TargetType:           req.TargetType,
		ProjectName:          cmp.Or(req.ProjectName, req.Name),
		Mode:                 mode,
		AutoSync:             kit.FromPtr(req.AutoSync),
		SyncInterval:         *cmp.Or(req.SyncInterval, new(60)),
		SyncDirectory:        kit.FromPtr(req.SyncDirectory),
		PullImageAfterSync:   kit.FromPtr(req.PullImageAfterSync),
		RedeployAfterSync:    kit.FromPtr(req.RedeployAfterSync),
		MaxSyncFiles:         *cmp.Or(req.MaxSyncFiles, &defaultMaxFiles),
		MaxSyncTotalSize:     *cmp.Or(req.MaxSyncTotalSize, &defaultMaxTotalSize),
		MaxSyncBinarySize:    *cmp.Or(req.MaxSyncBinarySize, &defaultMaxBinarySize),
		BackupOnSave:         true,
		PreDeployScriptPath:  nullableTrimmedString(req.PreDeployScriptPath),
		PreDeployRunnerImage: nullableTrimmedString(req.PreDeployRunnerImage),
		PreDeployEnv:         nullableTrimmedString(req.PreDeployEnv),
		PreDeployExtraMounts: nullableTrimmedString(req.PreDeployExtraMounts),
		PreDeployTimeoutSec:  *cmp.Or(req.PreDeployTimeoutSec, new(lifecycle.DefaultTimeoutSec)),
		PreDeployNetworkMode: cmp.Or(strings.TrimSpace(kit.FromPtr(req.PreDeployNetworkMode)), "none"),
	}

	adoptedProject, err := s.insertSyncRecord(ctx, &syncRecord, req, mode)
	if err != nil {
		return nil, err
	}
	slog.InfoContext(ctx, "GitOps sync created successfully", "syncId", syncRecord.ID, "name", syncRecord.Name)
	if adoptedProject != nil {
		adoptedProject.GitOpsManagedBy = &syncRecord.ID
		if linkErr := s.projectService.EnsureGitOpsProjectLinked(ctx, &syncRecord, adoptedProject); linkErr != nil {
			return nil, fmt.Errorf("failed to link existing project: %w", linkErr)
		}
	}

	_, _ = s.eventService.CreateEvent(ctx, event.CreateEventRequest{
		Type:          event.EventTypeGitSyncCreate,
		Severity:      event.EventSeveritySuccess,
		Title:         "Git sync created",
		Description:   fmt.Sprintf("Created git sync configuration '%s'", syncRecord.Name),
		ResourceType:  new("git_sync"),
		ResourceID:    new(syncRecord.ID),
		ResourceName:  new(syncRecord.Name),
		UserID:        new(actor.ID),
		Username:      new(actor.Username),
		EnvironmentID: new(syncRecord.EnvironmentID),
	})

	// The sync exists even when its first run fails, so that failure can be retried rather than failing creation.
	if _, syncErr := s.PerformSync(ctx, syncRecord.EnvironmentID, syncRecord.ID, actor); syncErr != nil {
		slog.ErrorContext(ctx, "Failed to perform initial sync after creation", "syncId", syncRecord.ID, "error", syncErr)
	}
	// The initial sync above already ran, so the job needs no kick.
	if syncRecord.AutoSync {
		s.registerSyncJob(ctx, syncRecord.ID, syncRecord.EnvironmentID, syncRecord.SyncInterval)
	}
	return s.GetSyncByID(ctx, "", syncRecord.ID)
}

// insertSyncRecord inserts the sync in one transaction with any linked project locked, so a project never
// ends up with two Git relationships. It returns the project a deploy sync adopts.
func (s *GitOpsSyncService) insertSyncRecord(ctx context.Context, syncRecord *projectpkg.GitOpsSync, req gitops.CreateSyncRequest, mode string) (*projectpkg.Project, error) {
	var adoptedProject *projectpkg.Project
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// An adopted project must exist, and must not be deployed from or backed up to Git already.
		if mode == gitops.SyncModeDeploy && strings.TrimSpace(req.ProjectID) != "" {
			if strings.TrimSpace(req.TargetType) == "swarm_stack" {
				return common.Classify(common.ErrValidation, &base.FieldError{Field: "projectId", Err: errors.New("an existing project cannot be linked to a swarm stack sync")})
			}
			project, err := projectpkg.LockProjectForSync(tx, req.ProjectID)
			if err != nil {
				return err
			}
			if strings.TrimSpace(kit.FromPtr(project.GitOpsManagedBy)) != "" {
				return common.Classify(common.ErrConflict, errors.New("project is already deployed from Git"))
			}
			var backups int64
			if countErr := tx.Model(&projectpkg.GitOpsSync{}).Where("mode = ? AND project_id = ?", gitops.SyncModeBackup, project.ID).Count(&backups).Error; countErr != nil {
				return fmt.Errorf("failed to check existing backups: %w", countErr)
			}
			if backups > 0 {
				return common.Classify(common.ErrConflict, errors.New("project is backed up to Git; disconnect that backup before deploying it from Git"))
			}
			if rootErr := s.projectService.EnsureProjectPathUnderRoot(ctx, project, false); rootErr != nil {
				return rootErr
			}
			adoptedProject = project
			syncRecord.ProjectID = &project.ID
			syncRecord.ProjectName = project.Name
		}
		if mode == gitops.SyncModeBackup {
			if err := s.backup.ApplyCreate(ctx, tx, req, syncRecord); err != nil {
				return err
			}
		}

		// Select("*") persists explicit zero values, such as unlimited sync limits, instead of column defaults.
		if err := tx.Select("*").Omit("Environment", "Repository", "Project").Create(syncRecord).Error; err != nil { //nolint:unqueryvet // intentional Select("*"); see comment above
			message := strings.ToLower(err.Error())
			if errors.Is(err, gorm.ErrDuplicatedKey) || strings.Contains(message, "unique constraint") || strings.Contains(message, "duplicate key") {
				return common.Classify(common.ErrConflict, errors.New("project already has a Git backup; disconnect it first"))
			}
			slog.ErrorContext(ctx, "Failed to create GitOps sync in database", "name", req.Name, "repositoryId", req.RepositoryID, "environmentId", syncRecord.EnvironmentID, "error", err)
			return fmt.Errorf("failed to create sync: %w", err)
		}
		if adoptedProject == nil {
			return nil
		}
		linked := tx.Model(&projectpkg.Project{}).
			Where("id = ? AND (gitops_managed_by IS NULL OR gitops_managed_by = '')", adoptedProject.ID).
			Update("gitops_managed_by", syncRecord.ID)
		if linked.Error != nil {
			return fmt.Errorf("failed to link existing project: %w", linked.Error)
		}
		if linked.RowsAffected != 1 {
			return common.Classify(common.ErrConflict, errors.New("project is already deployed from Git"))
		}
		return nil
	})
	return adoptedProject, err
}

func (s *GitOpsSyncService) UpdateSync(ctx context.Context, environmentID, id string, req gitops.UpdateSyncRequest, actor user.Actor) (*projectpkg.GitOpsSync, error) {
	syncRecord, err := s.GetSyncByID(ctx, environmentID, id)
	if err != nil {
		return nil, err
	}

	// Captured before the update so the job can follow the new state.
	oldAutoSync := syncRecord.AutoSync
	newAutoSync := *cmp.Or(req.AutoSync, &syncRecord.AutoSync)
	newInterval := *cmp.Or(req.SyncInterval, &syncRecord.SyncInterval)

	updates := make(map[string]any)

	if req.Name != nil {
		updates["name"] = *req.Name
	}
	if req.RepositoryID != nil {
		if _, repoErr := s.repoService.GetRepositoryByID(ctx, *req.RepositoryID); repoErr != nil {
			return nil, fmt.Errorf("repository not found: %w", repoErr)
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
	if req.PullImageAfterSync != nil {
		updates["pull_image_after_sync"] = *req.PullImageAfterSync
	}
	if req.RedeployAfterSync != nil {
		updates["redeploy_after_sync"] = *req.RedeployAfterSync
	}
	if limitsErr := validateSyncLimits(req.MaxSyncFiles, req.MaxSyncTotalSize, req.MaxSyncBinarySize); limitsErr != nil {
		return nil, limitsErr
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

	if modeErr := s.backup.ApplyModeUpdates(ctx, syncRecord, req, updates); modeErr != nil {
		return nil, modeErr
	}

	lifecycleCfg := lifecycleConfigInput{
		targetType:    req.TargetType,
		scriptPath:    req.PreDeployScriptPath,
		runnerImage:   req.PreDeployRunnerImage,
		env:           req.PreDeployEnv,
		extraMounts:   req.PreDeployExtraMounts,
		timeoutSec:    req.PreDeployTimeoutSec,
		networkMode:   req.PreDeployNetworkMode,
		syncDirectory: req.SyncDirectory,
	}
	if lifecycleErr := s.validateLifecycleConfig(ctx, syncRecord, lifecycleCfg); lifecycleErr != nil {
		return nil, lifecycleErr
	}
	// A nil field stays unchanged; an empty one clears the column, or resets the network mode to "none".
	for column, value := range map[string]*string{
		"pre_deploy_script_path":  lifecycleCfg.scriptPath,
		"pre_deploy_runner_image": lifecycleCfg.runnerImage,
		"pre_deploy_env":          lifecycleCfg.env,
		"pre_deploy_extra_mounts": lifecycleCfg.extraMounts,
	} {
		if value != nil {
			updates[column] = nullableTrimmedString(value)
		}
	}
	if lifecycleCfg.timeoutSec != nil {
		updates["pre_deploy_timeout_sec"] = *lifecycleCfg.timeoutSec
	}
	if lifecycleCfg.networkMode != nil {
		updates["pre_deploy_network_mode"] = cmp.Or(strings.TrimSpace(*lifecycleCfg.networkMode), "none")
	}

	if len(updates) > 0 {
		// Loaded associations must not overwrite explicitly updated foreign keys.
		if updateErr := s.db.WithContext(ctx).Model(syncRecord).Omit(clause.Associations).Updates(updates).Error; updateErr != nil {
			return nil, fmt.Errorf("failed to update sync: %w", updateErr)
		}
		_, _ = s.eventService.CreateEvent(ctx, event.CreateEventRequest{
			Type:          event.EventTypeGitSyncUpdate,
			Severity:      event.EventSeveritySuccess,
			Title:         "Git sync updated",
			Description:   fmt.Sprintf("Updated git sync configuration '%s'", syncRecord.Name),
			ResourceType:  new("git_sync"),
			ResourceID:    new(syncRecord.ID),
			ResourceName:  new(syncRecord.Name),
			UserID:        new(actor.ID),
			Username:      new(actor.Username),
			EnvironmentID: new(syncRecord.EnvironmentID),
		})
	}

	if !newAutoSync {
		s.jobs.Unregister(ctx, syncRecord.ID)
		return s.GetSyncByID(ctx, environmentID, id)
	}
	s.registerSyncJob(ctx, syncRecord.ID, syncRecord.EnvironmentID, newInterval)
	if !oldAutoSync {
		// A freshly enabled sync runs now instead of waiting a full interval.
		s.kickSync(ctx, syncRecord.ID, "startup")
	}

	return s.GetSyncByID(ctx, environmentID, id)
}

func (s *GitOpsSyncService) DeleteSync(ctx context.Context, environmentID, id string, actor user.Actor) error {
	// Even a row that can no longer be loaded must stop firing; an in-flight run re-reads it and skips.
	s.jobs.Unregister(ctx, id)
	s.backups.cancel(id)

	// The row is loaded only for the audit event, so a corrupt or environment-mismatched row is still deletable.
	syncRecord, loadErr := s.getSyncByID(ctx, environmentID, id, false)

	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Keyed on the sync ID, so orphaned managed flags clear even when the row could not be loaded.
		if err := tx.Model(&projectpkg.Project{}).
			Where("gitops_managed_by = ?", id).
			Update("gitops_managed_by", nil).Error; err != nil {
			return fmt.Errorf("failed to clear gitops_managed_by: %w", err)
		}
		// The handler enforced the delete permission, so the delete is not environment-scoped, and deleting nothing succeeds.
		if err := tx.Where("id = ?", id).Delete(&projectpkg.GitOpsSync{}).Error; err != nil {
			return fmt.Errorf("failed to delete sync: %w", err)
		}
		return nil
	}); err != nil {
		if loadErr == nil && syncRecord.AutoSync {
			s.registerSyncJob(ctx, syncRecord.ID, syncRecord.EnvironmentID, syncRecord.SyncInterval)
		}
		return err
	}

	if loadErr != nil {
		slog.WarnContext(ctx, "Deleted GitOps sync whose record could not be loaded", "syncId", id, "loadError", loadErr)
		return nil
	}

	_, _ = s.eventService.CreateEvent(ctx, event.CreateEventRequest{
		Type:          event.EventTypeGitSyncDelete,
		Severity:      event.EventSeverityInfo,
		Title:         "Git sync deleted",
		Description:   fmt.Sprintf("Deleted git sync configuration '%s'", syncRecord.Name),
		ResourceType:  new("git_sync"),
		ResourceID:    new(syncRecord.ID),
		ResourceName:  new(syncRecord.Name),
		UserID:        new(actor.ID),
		Username:      new(actor.Username),
		EnvironmentID: new(syncRecord.EnvironmentID),
	})

	return nil
}

func (s *GitOpsSyncService) PerformSync(ctx context.Context, environmentID, id string, actor user.Actor) (*gitops.SyncResult, error) {
	return s.performSyncAdmitted(ctx, environmentID, id, actor, false)
}

// performSyncAdmitted runs one sync under its admission lease and dispatches by mode. backupAdopt makes a
// backup replace whatever the remote holds in its backup directory.
func (s *GitOpsSyncService) performSyncAdmitted(ctx context.Context, environmentID, id string, actor user.Actor, backupAdopt bool) (*gitops.SyncResult, error) {
	// Overlapping runs of one sync (schedule, kick, manual, webhook) must not race the clone and redeploy.
	lease, admitted, err := s.jobs.TryAcquire(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to admit GitOps sync: %w", err)
	}
	if !admitted {
		slog.InfoContext(ctx, "GitOps sync already in progress; skipping", "syncId", id)
		return &gitops.SyncResult{Success: false, Message: "sync already in progress", SyncedAt: time.Now()}, nil
	}
	defer lease.Release(ctx)

	syncCtx, cancel := context.WithTimeout(ctx, defaultGitSyncTimeout)
	defer cancel()

	syncRecord, err := s.GetSyncByID(syncCtx, environmentID, id)
	if err != nil {
		return nil, err
	}

	result := &gitops.SyncResult{
		Success:  false,
		SyncedAt: time.Now(),
	}

	if syncRecord.Mode == gitops.SyncModeBackup {
		return s.backup.Perform(syncCtx, syncRecord, actor, result, backupAdopt)
	}

	return s.sync.Run(syncCtx, syncRecord, actor, result)
}

func (s *GitOpsSyncService) GetSyncStatus(ctx context.Context, environmentID, id string) (*gitops.SyncStatus, error) {
	syncRecord, err := s.GetSyncByID(ctx, environmentID, id)
	if err != nil {
		return nil, err
	}

	status := &gitops.SyncStatus{
		ID:                  syncRecord.ID,
		AutoSync:            syncRecord.AutoSync,
		LastSyncAt:          syncRecord.LastSyncAt,
		LastSyncStatus:      syncRecord.LastSyncStatus,
		LastSyncError:       syncRecord.LastSyncError,
		LastSyncCommit:      syncRecord.LastSyncCommit,
		Mode:                syncRecord.Mode,
		BackupState:         syncRecord.BackupState(),
		BackupPending:       syncRecord.BackupPending,
		BackupFailureReason: syncRecord.BackupFailureReason,
		LastBackupAt:        syncRecord.LastBackupAt,
	}

	// Calculate next sync time
	if syncRecord.AutoSync && syncRecord.LastSyncAt != nil {
		status.NextSyncAt = new(syncRecord.LastSyncAt.Add(time.Duration(syncRecord.SyncInterval) * time.Minute))
	}

	return status, nil
}

// CleanupLeakedScratchDirsOnStartup removes GitOps scratch directories an interrupted sync left in the projects
// directory, which discovery would import as phantom projects. It runs before any sync job is registered.
func (s *GitOpsSyncService) CleanupLeakedScratchDirsOnStartup(ctx context.Context) error {
	projectsDir, err := s.projectService.GetProjectsDirectory(ctx)
	if err != nil {
		return fmt.Errorf("failed to resolve projects directory for gitops scratch cleanup: %w", err)
	}

	entries, err := acfs.List(ctx, projectsDir, "/")
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("failed to list projects directory %s for gitops scratch cleanup: %w", projectsDir, err)
	}

	removed := 0
	for _, entry := range entries {
		if !entry.IsDirectory || !projects.IsGitOpsScratchDirName(entry.Name) {
			continue
		}
		scratchPath := filepath.Join(projectsDir, entry.Name)
		if rmErr := acfs.RemoveAll(ctx, projectsDir, entry.Path); rmErr != nil {
			slog.WarnContext(ctx, "Failed to remove leaked GitOps scratch directory on startup", "path", scratchPath, "error", rmErr)
			continue
		}
		removed++
		slog.InfoContext(ctx, "Removed leaked GitOps scratch directory on startup", "path", scratchPath)
	}
	if removed > 0 {
		slog.InfoContext(ctx, "Cleaned up leaked GitOps scratch directories on startup", "count", removed)
	}
	return nil
}

// CleanupLeakedCloneDirsOnStartup removes leaked git clone scratch dirs
// ("gitops-*") under the git work dir; safe because no sync holds a clone yet.
func (s *GitOpsSyncService) CleanupLeakedCloneDirsOnStartup(ctx context.Context) error {
	if s.repoService == nil || s.repoService.Client == nil {
		return nil
	}

	removed, err := s.repoService.PurgeScratchDirs(ctx, 0)
	if err != nil {
		return fmt.Errorf("failed to purge leaked git clone scratch directories: %w", err)
	}
	if removed > 0 {
		slog.InfoContext(ctx, "Cleaned up leaked git clone scratch directories on startup", "count", removed)
	}
	return nil
}

func (s *GitOpsSyncService) CleanupOrphanedSyncsOnStartup(ctx context.Context) error {
	var syncIDs []string
	if err := s.db.WithContext(ctx).Model(&projectpkg.GitOpsSync{}).
		Where("environment_id NOT IN (SELECT id FROM environments)").
		Pluck("id", &syncIDs).Error; err != nil {
		return fmt.Errorf("failed to list orphaned gitops syncs: %w", err)
	}
	if len(syncIDs) == 0 {
		return nil
	}

	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&projectpkg.Project{}).
			Where("gitops_managed_by IN ?", syncIDs).
			Update("gitops_managed_by", nil).Error; err != nil {
			return fmt.Errorf("failed to clear orphaned gitops project references: %w", err)
		}
		if err := tx.Where("id IN ?", syncIDs).Delete(&projectpkg.GitOpsSync{}).Error; err != nil {
			return fmt.Errorf("failed to delete orphaned gitops syncs: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}

	slog.InfoContext(ctx, "Cleaned up orphaned GitOps syncs on startup", "syncIds", syncIDs)
	return nil
}

func (s *GitOpsSyncService) ReconcileDirectorySyncProjectsOnStartup(ctx context.Context) error {
	var syncs []projectpkg.GitOpsSync
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

		project, err := s.sync.DirectoryProject(ctx, &syncs[i])
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

func (s *GitOpsSyncService) BrowseFiles(ctx context.Context, environmentID, id, localPath string) (*gitops.BrowseResponse, error) {
	browseCtx, cancel := context.WithTimeout(ctx, defaultGitSyncTimeout)
	defer cancel()

	syncRecord, err := s.GetSyncByID(browseCtx, environmentID, id)
	if err != nil {
		return nil, err
	}

	repository := syncRecord.Repository
	if repository == nil {
		return nil, errors.New("repository not found")
	}

	authConfig, err := s.repoService.GetAuthConfig(browseCtx, repository)
	if err != nil {
		return nil, err
	}

	// Clone the repository
	repoPath, err := s.repoService.Clone(browseCtx, repository.URL, syncRecord.Branch, authConfig, 1)
	if err != nil {
		return nil, fmt.Errorf("failed to clone repository: %w", err)
	}
	defer s.repoService.Discard(browseCtx, repoPath)

	// Browse the tree
	files, err := s.repoService.BrowseTree(browseCtx, repoPath, localPath)
	if err != nil {
		return nil, err
	}

	return &gitops.BrowseResponse{
		Path:  localPath,
		Files: files,
	}, nil
}

func (s *GitOpsSyncService) ImportSyncs(ctx context.Context, environmentID string, req []gitops.ImportGitOpsSyncRequest, actor user.Actor) (*gitops.ImportGitOpsSyncResponse, error) {
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
			response.Errors = append(response.Errors, fmt.Sprintf("Sync '%s': Repository '%s' not found (%v)", importItem.SyncName, importItem.GitRepo, err))
			continue
		}

		createReq := gitops.CreateSyncRequest{
			Name:                   importItem.SyncName,
			RepositoryID:           repo.ID,
			Branch:                 importItem.Branch,
			ComposePath:            importItem.DockerComposePath,
			ProjectName:            importItem.ProjectName,
			AutoSync:               new(importItem.AutoSync),
			SyncInterval:           new(importItem.SyncInterval),
			SyncDirectory:          importItem.SyncDirectory,
			PullImageAfterSync:     importItem.PullImageAfterSync,
			RedeployAfterSync:      importItem.RedeployAfterSync,
			MaxSyncFiles:           importItem.MaxSyncFiles,
			MaxSyncTotalSize:       importItem.MaxSyncTotalSize,
			MaxSyncBinarySize:      importItem.MaxSyncBinarySize,
			PreDeployConfigRequest: importItem.PreDeployConfigRequest,
		}

		_, err = s.CreateSync(ctx, environmentID, createReq, actor)
		if err != nil {
			response.FailedCount++
			response.Errors = append(response.Errors, fmt.Sprintf("Sync '%s': %v", importItem.SyncName, err))
		} else {
			response.SuccessCount++
		}
	}

	return response, nil
}

func (r *backupRuntime) branchLock(repositoryID, branch string) *sync.Mutex {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := repositoryID + "\x00" + branch
	lock, ok := r.branches[key]
	if !ok {
		lock = &sync.Mutex{}
		r.branches[key] = lock
	}
	return lock
}

func (r *backupRuntime) schedule(syncID string, run func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if timer, ok := r.timers[syncID]; ok {
		timer.Stop()
	}
	r.timers[syncID] = time.AfterFunc(r.debounce, func() {
		r.mu.Lock()
		delete(r.timers, syncID)
		r.mu.Unlock()
		run()
	})
}

func (r *backupRuntime) cancel(syncID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if timer, ok := r.timers[syncID]; ok {
		timer.Stop()
		delete(r.timers, syncID)
	}
}

// ResolveBackupConflict applies the chosen strategy to a backup that needs attention.
func (s *GitOpsSyncService) ResolveBackupConflict(ctx context.Context, environmentID, id string, req gitops.ResolveBackupConflictRequest, actor user.Actor) (*gitops.SyncResult, error) {
	if req.Strategy != gitops.BackupConflictUseArcane {
		return nil, common.Classify(common.ErrValidation, &base.FieldError{Field: "strategy", Err: fmt.Errorf("unsupported strategy %q", req.Strategy)})
	}
	syncRecord, err := s.getBackupSync(ctx, environmentID, id)
	if err != nil {
		return nil, err
	}
	return s.performSyncAdmitted(ctx, syncRecord.EnvironmentID, syncRecord.ID, actor, true)
}

// SubscribeProjectFileChanges marks backups pending on save and runs them after a debounce when auto backup is on.
func (s *GitOpsSyncService) SubscribeProjectFileChanges(ctx context.Context) {
	if s.projectService == nil {
		return
	}
	runCtx := s.jobs.Context(ctx)
	s.projectService.FilesChanged.Subscribe(func(projectID string) {
		var syncs []projectpkg.GitOpsSync
		if err := s.db.WithContext(runCtx).
			Where("mode = ? AND project_id = ?", gitops.SyncModeBackup, projectID).
			Find(&syncs).Error; err != nil {
			slog.ErrorContext(runCtx, "Failed to load git backups for changed project", "projectId", projectID, "error", err)
			return
		}
		now := time.Now()
		for _, syncRecord := range syncs {
			if err := s.db.WithContext(runCtx).Model(&projectpkg.GitOpsSync{}).Where("id = ?", syncRecord.ID).
				Updates(map[string]any{"backup_pending": true, "backup_pending_since": now}).Error; err != nil {
				slog.ErrorContext(runCtx, "Failed to mark git backup pending", "syncId", syncRecord.ID, "error", err)
				continue
			}
			if !syncRecord.AutoSync || !syncRecord.BackupOnSave {
				continue
			}
			syncID, environmentID := syncRecord.ID, syncRecord.EnvironmentID
			s.backups.schedule(syncID, func() {
				if s.jobs.Enabled() {
					s.kickSync(runCtx, syncID, "save")
					return
				}
				go func() {
					if _, err := s.PerformSync(runCtx, environmentID, syncID, user.SystemUser); err != nil {
						slog.ErrorContext(runCtx, "git backup after save failed", "syncId", syncID, "error", err)
					}
				}()
			})
		}
	})
}

// PreviewBackup reports what the next backup run would commit or why it needs attention.
func (s *GitOpsSyncService) PreviewBackup(ctx context.Context, environmentID, id string) (*gitops.BackupPreview, error) {
	previewCtx, cancel := context.WithTimeout(ctx, defaultGitSyncTimeout)
	defer cancel()

	syncRecord, err := s.getBackupSync(previewCtx, environmentID, id)
	if err != nil {
		return nil, err
	}
	return s.backup.Preview(previewCtx, syncRecord)
}

// GetBackupHistory lists revisions that touched the backup directory.
func (s *GitOpsSyncService) GetBackupHistory(ctx context.Context, environmentID, id string, limit int) (*gitops.BackupHistoryResponse, error) {
	historyCtx, cancel := context.WithTimeout(ctx, defaultGitSyncTimeout)
	defer cancel()

	syncRecord, err := s.getBackupSync(historyCtx, environmentID, id)
	if err != nil {
		return nil, err
	}
	return s.backup.History(historyCtx, syncRecord, limit)
}

// GetBackupRevision returns one revision with per-file diffs inside the backup directory.
func (s *GitOpsSyncService) GetBackupRevision(ctx context.Context, environmentID, id, commit string) (*gitops.BackupRevision, error) {
	revisionCtx, cancel := context.WithTimeout(ctx, defaultGitSyncTimeout)
	defer cancel()

	syncRecord, err := s.getBackupSync(revisionCtx, environmentID, id)
	if err != nil {
		return nil, err
	}
	return s.backup.Revision(revisionCtx, syncRecord, commit)
}

// ReconcileInterruptedBackupsOnStartup turns backups left running by a restart into pending failures.
func (s *GitOpsSyncService) ReconcileInterruptedBackupsOnStartup(ctx context.Context, protectedIDs ...string) error {
	query := s.db.WithContext(ctx).Model(&projectpkg.GitOpsSync{}).Where("mode = ? AND last_sync_status = ?", gitops.SyncModeBackup, backup.BackupStatusRunning)
	if len(protectedIDs) > 0 {
		query = query.Where("id NOT IN ?", protectedIDs)
	}
	err := query.Updates(map[string]any{
		"last_sync_status":      "failed",
		"last_sync_error":       "backup was interrupted by a restart",
		"backup_failure_reason": gitops.BackupFailureRepository,
		"backup_pending":        true,
		"backup_pending_since":  time.Now(),
	}).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("failed to reconcile interrupted git backups: %w", err)
	}
	return nil
}

func (s *GitOpsSyncService) getBackupSync(ctx context.Context, environmentID, id string) (*projectpkg.GitOpsSync, error) {
	syncRecord, err := s.GetSyncByID(ctx, environmentID, id)
	if err != nil {
		return nil, err
	}
	if syncRecord.Mode != gitops.SyncModeBackup {
		return nil, common.Classify(common.ErrBadRequest, errors.New("sync does not back up to Git"))
	}
	if syncRecord.Repository == nil {
		return nil, common.Classify(common.ErrNotFound, errors.New("repository not found"))
	}
	return syncRecord, nil
}
