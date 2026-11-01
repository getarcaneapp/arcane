package gitops

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/url"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/italypaleale/francis/builtin/workflow"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/fx"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"kit.getarcane.app"
	"kit.getarcane.app/acfs"
	"kit.getarcane.app/mapping"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/internal/event"
	"github.com/getarcaneapp/arcane/backend/v3/internal/gitops/children/backup"
	gitopssync "github.com/getarcaneapp/arcane/backend/v3/internal/gitops/children/sync"
	"github.com/getarcaneapp/arcane/backend/v3/internal/gitrepo"
	projectpkg "github.com/getarcaneapp/arcane/backend/v3/internal/project"
	"github.com/getarcaneapp/arcane/backend/v3/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v3/internal/swarm"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/gitutil"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/scheduler/entityjobs"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/scheduler/flow"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/scheduler/jobcontext"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/scheduler/runs"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/httpx"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/tracing"
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
		branches map[string]*sync.Mutex
		timers   map[string]*time.Timer
		debounce time.Duration
		mu       sync.Mutex
	}

	GitOpsSyncServiceParams struct {
		fx.In

		DB              *database.DB
		RepoService     *gitrepo.GitRepositoryService
		ProjectService  *projectpkg.ProjectService
		SwarmService    *swarm.SwarmService
		EventService    *event.EventService
		SettingsService *settings.SettingsService
	}

	SyncUpdate struct {
		Submitted map[string]bool
		Actor     types.Actor
		Request   types.GitOpsSync
	}

	SyncRequest struct {
		EnvironmentID string
		SyncID        string
		Actor         types.Actor
		BackupAdopt   bool
	}
)

const (
	defaultGitSyncTimeout      = 5 * time.Minute
	defaultMaxSyncFiles        = 500
	defaultMaxSyncTotalSizeMB  = 50
	defaultMaxSyncBinarySizeMB = 10
	gitOpsSyncAdmissionScope   = "gitops-sync"
)

var syncRuns = func() metric.Int64Counter {
	counter, err := otel.Meter(tracing.InstrumentationName).Int64Counter("arcane.gitops.syncs",
		metric.WithDescription("GitOps sync runs by outcome"), metric.WithUnit("{sync}"))
	if err != nil {
		otel.Handle(err)
	}
	return counter
}()

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
		return common.Classify(common.ErrValidation, &types.FieldError{Field: field, Err: errors.New(message)})
	}

	lifecycleFieldSet := in.scriptPath != nil || in.runnerImage != nil || in.env != nil || in.extraMounts != nil || in.timeoutSec != nil || in.networkMode != nil
	targetTypeChanging := in.targetType != nil && strings.TrimSpace(*in.targetType) != currentTargetType
	// Nothing lifecycle-related changes, or the result has no script to validate.
	if !lifecycleFieldSet && (scriptPath == "" || (in.syncDirectory == nil && !targetTypeChanging)) {
		return nil
	}
	// The kill switch only blocks edits to lifecycle fields, not unrelated edits to a sync that already has a hook.
	if lifecycleFieldSet && !s.settingsService.GetBoolSetting(ctx, "lifecycleEnabled") {
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
	case strings.TrimSpace(kit.FromPtr(cmp.Or(in.runnerImage, currentRunner))) == "" && strings.TrimSpace(s.settingsService.GetStringSetting(ctx, "lifecycleDefaultRunnerImage")) == "":
		return invalid("preDeployRunnerImage", "Runner image is required when a script path is set.")
	case !kit.FromPtr(cmp.Or(in.syncDirectory, &currentSyncDirectory)):
		return invalid("preDeployScriptPath", "Pre-deploy script requires \"Sync entire directory\" so the script is included in the synced files.")
	}

	if in.timeoutSec != nil {
		maxTimeoutSec := s.settingsService.GetIntSetting(ctx, "lifecycleMaxTimeoutSec")
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

func NewGitOpsSyncService(p GitOpsSyncServiceParams) *GitOpsSyncService {
	s := &GitOpsSyncService{
		db:              p.DB,
		repoService:     p.RepoService,
		projectService:  p.ProjectService,
		swarmService:    p.SwarmService,
		eventService:    p.EventService,
		settingsService: p.SettingsService,
		jobs:            entityjobs.New(entityjobs.GitOpsSyncJobPrefix, gitOpsSyncAdmissionScope),
		backups:         &backupRuntime{branches: map[string]*sync.Mutex{}, timers: map[string]*time.Timer{}, debounce: backup.DefaultBackupSaveDebounce},
	}
	logError := func(ctx context.Context, syncRecord *projectpkg.GitOpsSync, actor types.Actor, errorMsg string) {
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
	s.sync = gitopssync.New(gitopssync.Params{
		DB:             p.DB,
		RepoService:    p.RepoService,
		ProjectService: p.ProjectService,
		SwarmService:   p.SwarmService,
		EventService:   p.EventService,
		Limits:         s.getEffectiveSyncLimits,
		LogError:       logError,
		UnregisterJob:  s.jobs.Unregister,
	})
	s.backup = backup.New(backup.Params{
		DB:             p.DB,
		RepoService:    p.RepoService,
		ProjectService: p.ProjectService,
		EventService:   p.EventService,
		Limits:         s.getEffectiveSyncLimits,
		LogError:       logError,
		BranchLock:     s.backups.branchLock,
	})
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
					return types.SchedulerOutcome{Status: types.RunStatusSkipped}, nil
				}
				if err != nil {
					return nil, common.Classify(common.ErrUnavailable, err)
				}
				if !syncRecord.AutoSync {
					return types.SchedulerOutcome{Status: types.RunStatusSkipped}, nil
				}
				if previous, ok := jobcontext.Run(ctx); ok {
					if outcome := jobcontext.ConfirmedTarget(previous, target.SyncID); outcome.Status == types.RunStatusSucceeded {
						return outcome, nil
					}
				}
				if runningErr := jobcontext.Progress(ctx, types.TargetOutcome{ID: target.SyncID, Status: types.RunStatusRunning}); runningErr != nil {
					return nil, runningErr
				}
				result, err := s.PerformSync(ctx, SyncRequest{
					EnvironmentID: target.EnvironmentID,
					SyncID:        target.SyncID,
					Actor:         types.SystemUser,
				})
				if err != nil {
					slog.ErrorContext(ctx, "gitops auto-sync run failed", "syncId", target.SyncID, "error", err)
					return nil, err
				}
				if !result.Success {
					return types.SchedulerOutcome{Status: types.RunStatusSkipped, Message: result.Message}, nil
				}
				if succeededErr := jobcontext.Progress(ctx, types.TargetOutcome{ID: target.SyncID, Status: types.RunStatusSucceeded}); succeededErr != nil {
					return nil, succeededErr
				}
				return types.SchedulerOutcome{Status: types.RunStatusSucceeded, Message: result.Message}, nil
			}), workflow.WithMaxAttempts(3), workflow.WithRetryBackoff(30*time.Second, 2*time.Minute)),
		},
	})
	s.syncWorkflow = syncWorkflow
	return err
}

// SetScheduler injects the scheduler, the admission gate, and the app lifecycle context background syncs run on.
// It must run during bootstrap, before any per-sync job is registered.
func (s *GitOpsSyncService) SetScheduler(ctx context.Context, jobScheduler types.DynamicScheduler, admissionGate *runs.Admission) error {
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
	if _, err := s.jobs.Scheduler().Submit(ctx, types.SchedulerRequest{JobID: s.jobs.JobName(syncID), EnvironmentID: "0", Trigger: trigger}); err != nil {
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
		if overdue || (syncRecord.Mode == types.GitOpsSyncModeBackup && syncRecord.BackupPending) {
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
	envPinned := func(key string) bool {
		_, reason := s.settingsService.IsLocked(key)
		return reason == settings.LockReasonEnv
	}
	return kit.Ternary(envPinned("gitSyncMaxFiles"), maxFiles, syncRecord.MaxSyncFiles),
		kit.Ternary(envPinned("gitSyncMaxTotalSizeMb"), maxTotalSize, syncRecord.MaxSyncTotalSize),
		kit.Ternary(envPinned("gitSyncMaxBinarySizeMb"), maxBinarySize, syncRecord.MaxSyncBinarySize)
}

func (s *GitOpsSyncService) GetSyncsPaginated(ctx context.Context, environmentID string, params pagination.QueryParams) ([]types.GitOpsSync, pagination.Response, types.GitOpsSyncCounts, error) {
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
		q.Session(&gorm.Session{}).Where("mode = ?", types.GitOpsSyncModeBackup).Count(&backups).Error,
	); err != nil {
		return nil, pagination.Response{}, types.GitOpsSyncCounts{}, fmt.Errorf("failed to get sync counts: %w", err)
	}
	counts := types.GitOpsSyncCounts{
		TotalSyncs:      int(total),
		ActiveSyncs:     int(active),
		SuccessfulSyncs: int(successful),
		DeploySyncs:     int(total - backups),
		BackupSyncs:     int(backups),
	}

	paginationResp, err := pagination.PaginateAndSortDB(params, q.Preload("Repository").Preload("Project"), &syncs)
	if err != nil {
		return nil, pagination.Response{}, types.GitOpsSyncCounts{}, fmt.Errorf("failed to paginate gitops syncs: %w", err)
	}

	out, mapErr := mapping.MapSlice[projectpkg.GitOpsSync, types.GitOpsSync](syncs)
	if mapErr != nil {
		return nil, pagination.Response{}, types.GitOpsSyncCounts{}, fmt.Errorf("failed to map syncs: %w", mapErr)
	}

	return out, paginationResp, counts, nil
}

func (s *GitOpsSyncService) AggregateSyncs(ctx context.Context, params pagination.QueryParams) (types.PaginatedWithCounts[types.GitOpsSync, types.GitOpsSyncCounts], error) {
	collection, err := httpx.CollectEnvironments[types.PaginatedWithCounts[types.GitOpsSync, types.GitOpsSyncCounts]](ctx, url.Values{"start": {"0"}, "limit": {"-1"}})
	if err != nil {
		return types.PaginatedWithCounts[types.GitOpsSync, types.GitOpsSyncCounts]{}, err
	}

	pages := make([]pagination.EnvironmentPage[types.GitOpsSync], 0, len(collection.Results))
	var counts types.GitOpsSyncCounts
	for _, result := range collection.Results {
		pages = append(pages, pagination.EnvironmentPage[types.GitOpsSync]{
			Environment:    result.Environment,
			Items:          result.Value.Data,
			TotalAvailable: cmp.Or(result.Value.Pagination.GrandTotalItems, result.Value.Pagination.TotalItems),
		})
		counts = types.GitOpsSyncCounts{
			TotalSyncs:      counts.TotalSyncs + result.Value.Counts.TotalSyncs,
			ActiveSyncs:     counts.ActiveSyncs + result.Value.Counts.ActiveSyncs,
			SuccessfulSyncs: counts.SuccessfulSyncs + result.Value.Counts.SuccessfulSyncs,
			DeploySyncs:     counts.DeploySyncs + result.Value.Counts.DeploySyncs,
			BackupSyncs:     counts.BackupSyncs + result.Value.Counts.BackupSyncs,
		}
	}
	data, resp := pagination.MergeEnvironmentPages(pagination.Config[types.GitOpsSync]{
		SortBindings: []pagination.SortBinding[types.GitOpsSync]{
			{Key: "name", Fn: func(a, b types.GitOpsSync) int { return strings.Compare(a.Name, b.Name) }},
			{Key: "repositoryId", Fn: func(a, b types.GitOpsSync) int { return strings.Compare(a.RepositoryID, b.RepositoryID) }},
			{Key: "branch", Fn: func(a, b types.GitOpsSync) int { return strings.Compare(a.Branch, b.Branch) }},
			{Key: "composePath", Fn: func(a, b types.GitOpsSync) int { return strings.Compare(a.ComposePath, b.ComposePath) }},
			{Key: "projectName", Fn: func(a, b types.GitOpsSync) int { return strings.Compare(a.ProjectName, b.ProjectName) }},
			{Key: "syncInterval", Fn: func(a, b types.GitOpsSync) int { return cmp.Compare(a.SyncInterval, b.SyncInterval) }},
			{Key: "autoSync", Fn: func(a, b types.GitOpsSync) int {
				return cmp.Compare(kit.Ternary(a.AutoSync, 1, 0), kit.Ternary(b.AutoSync, 1, 0))
			}},
			{Key: "lastSyncAt", Fn: func(a, b types.GitOpsSync) int {
				return kit.FromPtr(a.LastSyncAt).Compare(kit.FromPtr(b.LastSyncAt))
			}},
		},
	}, pages, params)

	return types.PaginatedWithCounts[types.GitOpsSync, types.GitOpsSyncCounts]{
		Success:            true,
		Data:               data,
		Counts:             counts,
		Pagination:         handlerutil.PaginationResponse(resp),
		FailedEnvironments: collection.Failures,
	}, nil
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

func (s *GitOpsSyncService) CreateSync(ctx context.Context, environmentID string, change SyncUpdate) (*projectpkg.GitOpsSync, error) {
	slog.InfoContext(ctx, "Creating GitOps sync", "environmentId", environmentID, "name", change.Request.Name, "repositoryId", change.Request.RepositoryID)

	mode := cmp.Or(strings.TrimSpace(change.Request.Mode), types.GitOpsSyncModeDeploy)
	switch {
	case strings.TrimSpace(change.Request.Name) == "":
		return nil, common.Classify(common.ErrValidation, &types.FieldError{Field: "name", Err: errors.New("name is required")})
	case strings.TrimSpace(change.Request.RepositoryID) == "":
		return nil, common.Classify(common.ErrValidation, &types.FieldError{Field: "repositoryId", Err: errors.New("repository is required")})
	case strings.TrimSpace(change.Request.Branch) == "":
		return nil, common.Classify(common.ErrValidation, &types.FieldError{Field: "branch", Err: errors.New("branch is required")})
	case mode != types.GitOpsSyncModeDeploy && mode != types.GitOpsSyncModeBackup:
		return nil, common.Classify(common.ErrValidation, &types.FieldError{Field: "mode", Err: fmt.Errorf("unsupported sync mode %q", change.Request.Mode)})
	case mode == types.GitOpsSyncModeDeploy && (change.Request.BackupDirectory != "" || len(change.Request.BackupPaths) > 0 || change.Submitted["backupOnSave"]):
		return nil, common.Classify(common.ErrValidation, &types.FieldError{Field: "mode", Err: errors.New("backup options require mode \"backup\"")})
	case mode == types.GitOpsSyncModeBackup &&
		(change.Request.TargetType != "" && change.Request.TargetType != "project" ||
			change.Request.SyncDirectory || change.Request.PullImageAfterSync || change.Request.RedeployAfterSync ||
			hasPreDeployConfig(change.Submitted)):
		return nil, common.Classify(common.ErrValidation, &types.FieldError{Field: "mode", Err: errors.New("deployment options cannot be set on a backup sync")})
	case mode == types.GitOpsSyncModeDeploy && strings.TrimSpace(change.Request.ComposePath) == "":
		return nil, common.Classify(common.ErrValidation, &types.FieldError{Field: "composePath", Err: errors.New("compose path is required")})
	}

	repo, err := s.repoService.GetRepositoryByID(ctx, change.Request.RepositoryID)
	if err != nil {
		slog.ErrorContext(ctx, "Repository not found for GitOps sync", "repositoryId", change.Request.RepositoryID, "error", err)
		return nil, fmt.Errorf("repository not found: %w", err)
	}
	slog.InfoContext(ctx, "Found repository for GitOps sync", "repositoryId", change.Request.RepositoryID, "repositoryName", repo.Name)

	if limitsErr := validateSyncLimits(&change.Request.MaxSyncFiles, &change.Request.MaxSyncTotalSize, &change.Request.MaxSyncBinarySize); limitsErr != nil {
		return nil, limitsErr
	}
	lifecycleCfg := lifecycleConfigInput{
		targetType:    &change.Request.TargetType,
		scriptPath:    kit.Ternary(change.Submitted["preDeployScriptPath"], new(kit.FromPtr(change.Request.PreDeployScriptPath)), nil),
		runnerImage:   kit.Ternary(change.Submitted["preDeployRunnerImage"], new(kit.FromPtr(change.Request.PreDeployRunnerImage)), nil),
		env:           kit.Ternary(change.Submitted["preDeployEnv"], new(kit.FromPtr(change.Request.PreDeployEnv)), nil),
		extraMounts:   kit.Ternary(change.Submitted["preDeployExtraMounts"], new(kit.FromPtr(change.Request.PreDeployExtraMounts)), nil),
		timeoutSec:    kit.Ternary(change.Submitted["preDeployTimeoutSec"], &change.Request.PreDeployTimeoutSec, nil),
		networkMode:   kit.Ternary(change.Submitted["preDeployNetworkMode"], &change.Request.PreDeployNetworkMode, nil),
		syncDirectory: kit.Ternary(change.Submitted["syncDirectory"], &change.Request.SyncDirectory, nil),
	}
	if lifecycleErr := s.validateLifecycleConfig(ctx, nil, lifecycleCfg); lifecycleErr != nil {
		return nil, lifecycleErr
	}

	defaultMaxFiles, defaultMaxTotalSize, defaultMaxBinarySize := s.getEnvironmentSyncLimits(ctx)
	syncRecord := projectpkg.GitOpsSync{
		Name:                 change.Request.Name,
		EnvironmentID:        environmentID,
		RepositoryID:         change.Request.RepositoryID,
		Branch:               change.Request.Branch,
		ComposePath:          change.Request.ComposePath,
		TargetType:           change.Request.TargetType,
		ProjectName:          cmp.Or(change.Request.ProjectName, change.Request.Name),
		Mode:                 mode,
		AutoSync:             change.Request.AutoSync,
		SyncInterval:         kit.Ternary(change.Submitted["syncInterval"], change.Request.SyncInterval, 60),
		SyncDirectory:        change.Request.SyncDirectory,
		PullImageAfterSync:   change.Request.PullImageAfterSync,
		RedeployAfterSync:    change.Request.RedeployAfterSync,
		MaxSyncFiles:         kit.Ternary(change.Submitted["maxSyncFiles"], change.Request.MaxSyncFiles, defaultMaxFiles),
		MaxSyncTotalSize:     kit.Ternary(change.Submitted["maxSyncTotalSize"], change.Request.MaxSyncTotalSize, defaultMaxTotalSize),
		MaxSyncBinarySize:    kit.Ternary(change.Submitted["maxSyncBinarySize"], change.Request.MaxSyncBinarySize, defaultMaxBinarySize),
		BackupOnSave:         true,
		PreDeployScriptPath:  nullableTrimmedString(change.Request.PreDeployScriptPath),
		PreDeployRunnerImage: nullableTrimmedString(change.Request.PreDeployRunnerImage),
		PreDeployEnv:         nullableTrimmedString(change.Request.PreDeployEnv),
		PreDeployExtraMounts: nullableTrimmedString(change.Request.PreDeployExtraMounts),
		PreDeployTimeoutSec:  kit.Ternary(change.Submitted["preDeployTimeoutSec"], change.Request.PreDeployTimeoutSec, types.LifecycleDefaultTimeoutSec),
		PreDeployNetworkMode: cmp.Or(strings.TrimSpace(change.Request.PreDeployNetworkMode), "none"),
	}

	adoptedProject, err := s.insertSyncRecord(ctx, &syncRecord, change.Request, change.Submitted, mode)
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
		UserID:        new(change.Actor.ID),
		Username:      new(change.Actor.Username),
		EnvironmentID: new(syncRecord.EnvironmentID),
	})

	// The sync exists even when its first run fails, so that failure can be retried rather than failing creation.
	if _, syncErr := s.PerformSync(ctx, SyncRequest{
		EnvironmentID: syncRecord.EnvironmentID,
		SyncID:        syncRecord.ID,
		Actor:         change.Actor,
	}); syncErr != nil {
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
func (s *GitOpsSyncService) insertSyncRecord(ctx context.Context, syncRecord *projectpkg.GitOpsSync, req types.GitOpsSync, submitted map[string]bool, mode string) (*projectpkg.Project, error) {
	var adoptedProject *projectpkg.Project
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// An adopted project must exist, and must not be deployed from or backed up to Git already.
		if mode == types.GitOpsSyncModeDeploy && strings.TrimSpace(kit.FromPtr(req.ProjectID)) != "" {
			if strings.TrimSpace(req.TargetType) == "swarm_stack" {
				return common.Classify(common.ErrValidation, &types.FieldError{Field: "projectId", Err: errors.New("an existing project cannot be linked to a swarm stack sync")})
			}
			project, err := projectpkg.LockProjectForSync(tx, kit.FromPtr(req.ProjectID))
			if err != nil {
				return err
			}
			if strings.TrimSpace(kit.FromPtr(project.GitOpsManagedBy)) != "" {
				return common.Classify(common.ErrConflict, errors.New("project is already deployed from Git"))
			}
			var backups int64
			if countErr := tx.Model(&projectpkg.GitOpsSync{}).Where("mode = ? AND project_id = ?", types.GitOpsSyncModeBackup, project.ID).Count(&backups).Error; countErr != nil {
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
		if mode == types.GitOpsSyncModeBackup {
			if err := s.backup.ApplyCreate(ctx, tx, req, submitted, syncRecord); err != nil {
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

func (s *GitOpsSyncService) UpdateSync(ctx context.Context, environmentID, id string, change SyncUpdate) (*projectpkg.GitOpsSync, error) {
	syncRecord, err := s.GetSyncByID(ctx, environmentID, id)
	if err != nil {
		return nil, err
	}

	switch {
	case syncRecord.Mode != types.GitOpsSyncModeBackup && (change.Submitted["backupPaths"] || change.Submitted["backupOnSave"]):
		return nil, common.Classify(common.ErrValidation, &types.FieldError{Field: "mode", Err: errors.New("backup options cannot be set on a deployment sync")})
	case syncRecord.Mode == types.GitOpsSyncModeBackup &&
		(change.Request.TargetType != "" && change.Request.TargetType != "project" ||
			change.Submitted["composePath"] ||
			change.Submitted["projectName"] ||
			change.Request.SyncDirectory ||
			change.Request.PullImageAfterSync ||
			change.Request.RedeployAfterSync ||
			hasPreDeployConfig(change.Submitted)):
		return nil, common.Classify(common.ErrValidation, &types.FieldError{Field: "mode", Err: errors.New("deployment options cannot be set on a backup sync")})
	}

	// Captured before the update so the job can follow the new state.
	oldAutoSync := syncRecord.AutoSync
	newAutoSync := kit.Ternary(change.Submitted["autoSync"], change.Request.AutoSync, syncRecord.AutoSync)
	newInterval := kit.Ternary(change.Submitted["syncInterval"], change.Request.SyncInterval, syncRecord.SyncInterval)

	if change.Submitted["repositoryId"] {
		if _, repoErr := s.repoService.GetRepositoryByID(ctx, change.Request.RepositoryID); repoErr != nil {
			return nil, fmt.Errorf("repository not found: %w", repoErr)
		}
	}
	if limitsErr := validateSyncLimits(&change.Request.MaxSyncFiles, &change.Request.MaxSyncTotalSize, &change.Request.MaxSyncBinarySize); limitsErr != nil {
		return nil, limitsErr
	}

	updates := make(map[string]any)
	for field, update := range map[string]struct {
		value  any
		column string
	}{
		"name":               {column: "name", value: change.Request.Name},
		"repositoryId":       {column: "repository_id", value: change.Request.RepositoryID},
		"branch":             {column: "branch", value: change.Request.Branch},
		"composePath":        {column: "compose_path", value: change.Request.ComposePath},
		"targetType":         {column: "target_type", value: change.Request.TargetType},
		"projectName":        {column: "project_name", value: change.Request.ProjectName},
		"autoSync":           {column: "auto_sync", value: change.Request.AutoSync},
		"syncInterval":       {column: "sync_interval", value: change.Request.SyncInterval},
		"syncDirectory":      {column: "sync_directory", value: change.Request.SyncDirectory},
		"pullImageAfterSync": {column: "pull_image_after_sync", value: change.Request.PullImageAfterSync},
		"redeployAfterSync":  {column: "redeploy_after_sync", value: change.Request.RedeployAfterSync},
		"maxSyncFiles":       {column: "max_sync_files", value: change.Request.MaxSyncFiles},
		"maxSyncTotalSize":   {column: "max_sync_total_size", value: change.Request.MaxSyncTotalSize},
		"maxSyncBinarySize":  {column: "max_sync_binary_size", value: change.Request.MaxSyncBinarySize},
	} {
		if change.Submitted[field] {
			updates[update.column] = update.value
		}
	}

	if modeErr := s.backup.ApplyModeUpdates(ctx, syncRecord, change.Request, change.Submitted, updates); modeErr != nil {
		return nil, modeErr
	}

	lifecycleCfg := lifecycleConfigInput{
		targetType:    kit.Ternary(change.Submitted["targetType"], &change.Request.TargetType, nil),
		scriptPath:    kit.Ternary(change.Submitted["preDeployScriptPath"], new(kit.FromPtr(change.Request.PreDeployScriptPath)), nil),
		runnerImage:   kit.Ternary(change.Submitted["preDeployRunnerImage"], new(kit.FromPtr(change.Request.PreDeployRunnerImage)), nil),
		env:           kit.Ternary(change.Submitted["preDeployEnv"], new(kit.FromPtr(change.Request.PreDeployEnv)), nil),
		extraMounts:   kit.Ternary(change.Submitted["preDeployExtraMounts"], new(kit.FromPtr(change.Request.PreDeployExtraMounts)), nil),
		timeoutSec:    kit.Ternary(change.Submitted["preDeployTimeoutSec"], &change.Request.PreDeployTimeoutSec, nil),
		networkMode:   kit.Ternary(change.Submitted["preDeployNetworkMode"], &change.Request.PreDeployNetworkMode, nil),
		syncDirectory: kit.Ternary(change.Submitted["syncDirectory"], &change.Request.SyncDirectory, nil),
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
			UserID:        new(change.Actor.ID),
			Username:      new(change.Actor.Username),
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

func (s *GitOpsSyncService) DeleteSync(ctx context.Context, environmentID, id string, actor types.Actor) error {
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

func (s *GitOpsSyncService) PerformSync(ctx context.Context, req SyncRequest) (syncResult *types.GitOpsSyncResult, syncErr error) {
	ctx, span := otel.Tracer(tracing.InstrumentationName).Start(ctx, "gitops.sync", trace.WithAttributes(
		attribute.String("arcane.gitops.sync_id", req.SyncID),
		attribute.String("arcane.environment.id", req.EnvironmentID),
	))
	counted := false
	defer func() {
		success := syncErr == nil && syncResult != nil && syncResult.Success
		if counted {
			outcome := attribute.String("arcane.gitops.outcome", kit.Ternary(success, "success", "failure"))
			span.SetAttributes(outcome)
			syncRuns.Add(context.WithoutCancel(ctx), 1, metric.WithAttributes(outcome))
		}
		tracing.End(span, syncErr)
	}()

	// Overlapping runs of one sync (schedule, kick, manual, webhook) must not race the clone and redeploy.
	admitCtx, cancelAdmit := context.WithTimeout(ctx, defaultGitSyncTimeout)
	lease, err := s.jobs.Acquire(admitCtx, req.SyncID)
	cancelAdmit()
	if err != nil {
		return nil, fmt.Errorf("failed to admit GitOps sync: %w", err)
	}
	defer lease.Release(ctx)
	counted = true

	syncCtx, cancel := context.WithTimeout(ctx, defaultGitSyncTimeout)
	defer cancel()

	syncRecord, err := s.GetSyncByID(syncCtx, req.EnvironmentID, req.SyncID)
	if err != nil {
		return nil, err
	}
	span.SetAttributes(
		attribute.String("arcane.git.branch", syncRecord.Branch),
		attribute.String("arcane.gitops.mode", syncRecord.Mode),
		attribute.String("arcane.gitops.target_type", syncRecord.TargetType),
	)
	if syncRecord.Repository != nil {
		span.SetAttributes(git.RepositoryAttribute(syncRecord.Repository.URL))
	}

	result := &types.GitOpsSyncResult{
		Success:  false,
		SyncedAt: time.Now(),
	}

	if syncRecord.Mode == types.GitOpsSyncModeBackup {
		return s.backup.Perform(syncCtx, syncRecord, req.Actor, result, req.BackupAdopt)
	}

	return s.sync.Run(ctx, defaultGitSyncTimeout, syncRecord, req.Actor, result)
}

func (s *GitOpsSyncService) GetSyncStatus(ctx context.Context, environmentID, id string) (*types.GitOpsSyncStatus, error) {
	syncRecord, err := s.GetSyncByID(ctx, environmentID, id)
	if err != nil {
		return nil, err
	}

	return &types.GitOpsSyncStatus{
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
		NextSyncAt: func() *time.Time {
			if !syncRecord.AutoSync || syncRecord.LastSyncAt == nil {
				return nil
			}
			return new(syncRecord.LastSyncAt.Add(time.Duration(syncRecord.SyncInterval) * time.Minute))
		}(),
	}, nil
}

// CleanupLeakedScratchDirsOnStartup removes GitOps scratch directories an interrupted sync left in the projects
// directory, which discovery would import as phantom projects. It runs before any sync job is registered.
func (s *GitOpsSyncService) CleanupLeakedScratchDirsOnStartup(ctx context.Context) error {
	projectsDir, err := s.projectService.GetProjectsDirectory(ctx)
	if err != nil {
		return fmt.Errorf("failed to resolve projects directory for gitops scratch cleanup: %w", err)
	}

	entries, err := acfs.Root{Path: projectsDir}.List(ctx, "/")
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
		if rmErr := (acfs.Root{Path: projectsDir}).RemoveAll(ctx, entry.Path); rmErr != nil {
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

func (s *GitOpsSyncService) BrowseFiles(ctx context.Context, environmentID, id, localPath string) (*types.GitOpsBrowseResponse, error) {
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

	return &types.GitOpsBrowseResponse{
		Path:  localPath,
		Files: files,
	}, nil
}

func (s *GitOpsSyncService) ImportSyncs(ctx context.Context, environmentID string, req []types.ImportGitOpsSyncRequest, actor types.Actor) (*types.ImportGitOpsSyncResponse, error) {
	response := &types.ImportGitOpsSyncResponse{
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

		createReq := types.GitOpsSync{
			Name:                 importItem.SyncName,
			RepositoryID:         repo.ID,
			Branch:               importItem.Branch,
			ComposePath:          importItem.DockerComposePath,
			ProjectName:          importItem.ProjectName,
			AutoSync:             importItem.AutoSync,
			SyncInterval:         importItem.SyncInterval,
			SyncDirectory:        kit.FromPtr(importItem.SyncDirectory),
			PullImageAfterSync:   kit.FromPtr(importItem.PullImageAfterSync),
			RedeployAfterSync:    kit.FromPtr(importItem.RedeployAfterSync),
			MaxSyncFiles:         kit.FromPtr(importItem.MaxSyncFiles),
			MaxSyncTotalSize:     kit.FromPtr(importItem.MaxSyncTotalSize),
			MaxSyncBinarySize:    kit.FromPtr(importItem.MaxSyncBinarySize),
			PreDeployScriptPath:  importItem.PreDeployScriptPath,
			PreDeployRunnerImage: importItem.PreDeployRunnerImage,
			PreDeployEnv:         importItem.PreDeployEnv,
			PreDeployExtraMounts: importItem.PreDeployExtraMounts,
			PreDeployTimeoutSec:  kit.FromPtr(importItem.PreDeployTimeoutSec),
			PreDeployNetworkMode: kit.FromPtr(importItem.PreDeployNetworkMode),
		}
		submitted := map[string]bool{
			"autoSync":             true,
			"syncInterval":         true,
			"syncDirectory":        importItem.SyncDirectory != nil,
			"maxSyncFiles":         importItem.MaxSyncFiles != nil,
			"maxSyncTotalSize":     importItem.MaxSyncTotalSize != nil,
			"maxSyncBinarySize":    importItem.MaxSyncBinarySize != nil,
			"preDeployScriptPath":  importItem.PreDeployScriptPath != nil,
			"preDeployRunnerImage": importItem.PreDeployRunnerImage != nil,
			"preDeployEnv":         importItem.PreDeployEnv != nil,
			"preDeployExtraMounts": importItem.PreDeployExtraMounts != nil,
			"preDeployTimeoutSec":  importItem.PreDeployTimeoutSec != nil,
			"preDeployNetworkMode": importItem.PreDeployNetworkMode != nil,
		}

		_, err = s.CreateSync(ctx, environmentID, SyncUpdate{
			Request:   createReq,
			Submitted: submitted,
			Actor:     actor,
		})
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
		defer utils.RecoverToError(nil, "git backup debounce", "syncId", syncID)
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
func (s *GitOpsSyncService) ResolveBackupConflict(ctx context.Context, environmentID, id string, req types.GitOpsResolveBackupConflictRequest, actor types.Actor) (*types.GitOpsSyncResult, error) {
	if req.Strategy != types.GitOpsBackupConflictUseArcane {
		return nil, common.Classify(common.ErrValidation, &types.FieldError{Field: "strategy", Err: fmt.Errorf("unsupported strategy %q", req.Strategy)})
	}
	syncRecord, err := s.getBackupSync(ctx, environmentID, id)
	if err != nil {
		return nil, err
	}
	return s.PerformSync(ctx, SyncRequest{
		EnvironmentID: syncRecord.EnvironmentID,
		SyncID:        syncRecord.ID,
		Actor:         actor,
		BackupAdopt:   true,
	})
}

// SubscribeProjectFileChanges marks backups pending on save and runs them after a debounce when auto backup is on.
func (s *GitOpsSyncService) SubscribeProjectFileChanges(ctx context.Context) {
	runCtx := s.jobs.Context(ctx)
	s.projectService.FilesChanged.Subscribe(func(projectID string) {
		var syncs []projectpkg.GitOpsSync
		if err := s.db.WithContext(runCtx).
			Where("mode = ? AND project_id = ?", types.GitOpsSyncModeBackup, projectID).
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
					defer utils.RecoverToError(nil, "git backup after save", "syncId", syncID, "projectId", projectID)
					if _, err := s.PerformSync(runCtx, SyncRequest{
						EnvironmentID: environmentID,
						SyncID:        syncID,
						Actor:         types.SystemUser,
					}); err != nil {
						slog.ErrorContext(runCtx, "git backup after save failed", "syncId", syncID, "error", err)
					}
				}()
			})
		}
	})
}

// PreviewBackup reports what the next backup run would commit or why it needs attention.
func (s *GitOpsSyncService) PreviewBackup(ctx context.Context, environmentID, id string) (*types.GitOpsBackupPreview, error) {
	previewCtx, cancel := context.WithTimeout(ctx, defaultGitSyncTimeout)
	defer cancel()

	syncRecord, err := s.getBackupSync(previewCtx, environmentID, id)
	if err != nil {
		return nil, err
	}
	return s.backup.Preview(previewCtx, syncRecord)
}

// GetBackupHistory lists revisions that touched the backup directory.
func (s *GitOpsSyncService) GetBackupHistory(ctx context.Context, environmentID, id string, limit int) (*types.GitOpsBackupHistoryResponse, error) {
	historyCtx, cancel := context.WithTimeout(ctx, defaultGitSyncTimeout)
	defer cancel()

	syncRecord, err := s.getBackupSync(historyCtx, environmentID, id)
	if err != nil {
		return nil, err
	}
	return s.backup.History(historyCtx, syncRecord, limit)
}

// GetBackupRevision returns one revision with per-file diffs inside the backup directory.
func (s *GitOpsSyncService) GetBackupRevision(ctx context.Context, environmentID, id, commit string) (*types.GitOpsBackupRevision, error) {
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
	query := s.db.WithContext(ctx).Model(&projectpkg.GitOpsSync{}).Where("mode = ? AND last_sync_status = ?", types.GitOpsSyncModeBackup, backup.BackupStatusRunning)
	if len(protectedIDs) > 0 {
		query = query.Where("id NOT IN ?", protectedIDs)
	}
	err := query.Updates(map[string]any{
		"last_sync_status":      "failed",
		"last_sync_error":       "backup was interrupted by a restart",
		"backup_failure_reason": types.GitOpsBackupFailureRepository,
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
	if syncRecord.Mode != types.GitOpsSyncModeBackup {
		return nil, common.Classify(common.ErrBadRequest, errors.New("sync does not back up to Git"))
	}
	if syncRecord.Repository == nil {
		return nil, common.Classify(common.ErrNotFound, errors.New("repository not found"))
	}
	return syncRecord, nil
}
