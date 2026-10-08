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
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/jobcontext"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/runs"
)

type GitOpsSyncService struct {
	db              *database.DB
	repoService     *gitrepo.GitRepositoryService
	projectService  *projectpkg.ProjectService
	swarmService    *swarm.SwarmService
	eventService    *event.EventService
	settingsService *settings.SettingsService

	// jobs carries the scheduler and app lifecycle context, injected
	// post-construction via SetScheduler.
	jobs *entityjobs.Registry

	backups *backupRuntimeInternal
	sync    *gitopssync.Service
	backup  *backup.Service
}

const (
	defaultGitSyncTimeout = 5 * time.Minute

	defaultMaxSyncFiles        = 500
	defaultMaxSyncTotalSizeMB  = 50
	defaultMaxSyncBinarySizeMB = 10
	defaultMaxSyncTotalSize    = defaultMaxSyncTotalSizeMB * 1024 * 1024
	defaultMaxSyncBinarySize   = defaultMaxSyncBinarySizeMB * 1024 * 1024

	gitOpsSyncAdmissionScopeInternal = "gitops-sync"
)

// lifecycleConfigInputInternal is the slice of CreateSyncRequest /
// UpdateSyncRequest fields relevant to the pre-deploy lifecycle hook. Both
// request types collapse into the same shape so a single validator handles
// both flows.
type lifecycleConfigInputInternal struct {
	targetType    *string
	scriptPath    *string
	runnerImage   *string
	env           *string
	extraMounts   *string
	timeoutSec    *int
	networkMode   *string
	syncDirectory *bool
}

func (s *GitOpsSyncService) validateLifecycleConfigInternal(ctx context.Context, current *projectpkg.GitOpsSync, in lifecycleConfigInputInternal) error {
	lifecycleFieldSet := in.scriptPath != nil || in.runnerImage != nil || in.env != nil || in.extraMounts != nil || in.timeoutSec != nil || in.networkMode != nil
	syncDirectoryChanging := in.syncDirectory != nil
	targetTypeChanging := in.targetType != nil && strings.TrimSpace(*in.targetType) != resolveLifecycleEffectiveTargetType(current, nil)
	effectiveScriptPath := resolveLifecycleEffectiveString(currentString(current, func(c *projectpkg.GitOpsSync) *string { return c.PreDeployScriptPath }), in.scriptPath)

	// If nothing lifecycle-related is being touched and the resulting state
	// has no script configured, there's nothing to validate.
	if !lifecycleFieldSet && !syncDirectoryChanging && !targetTypeChanging {
		return nil
	}
	if !lifecycleFieldSet && effectiveScriptPath == "" {
		return nil
	}

	// Kill switch only blocks updates that actively touch lifecycle fields;
	// toggling unrelated fields on an existing config shouldn't be rejected
	// just because the global setting was turned off afterwards.
	if lifecycleFieldSet && !s.settingsService.GetBoolSetting(ctx, "lifecycleEnabled", false) {
		return common.Classify(
			common.ErrValidation,
			&base.FieldError{
				Field: "preDeployScriptPath",
				//nolint:staticcheck // Preserve the existing error message.
				Err: errors.New(
					"Pre-deploy lifecycle hooks are disabled. An admin must enable lifecycleEnabled in settings before they can be configured.",
				),
			},
		)
	}

	if effectiveScriptPath != "" {
		if err := s.validateLifecycleScriptConfigInternal(ctx, current, in, effectiveScriptPath); err != nil {
			return err
		}
	}

	if in.timeoutSec != nil {
		if *in.timeoutSec < 1 {
			return common.Classify(
				common.ErrValidation,
				&base.FieldError{
					Field: "preDeployTimeoutSec",
					//nolint:staticcheck // Preserve the existing error message.
					Err: errors.New(
						"Timeout must be at least 1 second.",
					),
				},
			)
		}
		maxTimeoutSec := s.settingsService.GetIntSetting(ctx, "lifecycleMaxTimeoutSec", lifecycle.DefaultMaxTimeoutSec)
		if maxTimeoutSec > 0 && *in.timeoutSec > maxTimeoutSec {
			return common.Classify(
				common.ErrValidation,
				&base.FieldError{
					Field: "preDeployTimeoutSec",
					//nolint:staticcheck // Preserve the existing error message.
					Err: fmt.Errorf(
						"Timeout %ds exceeds the lifecycleMaxTimeoutSec setting (%ds).",
						*in.timeoutSec,
						maxTimeoutSec,
					),
				},
			)
		}
	}

	if _, err := projectpkg.ParseEnvText(in.env); err != nil {
		return common.Classify(common.ErrValidation, &base.FieldError{Field: "preDeployEnv", Err: errors.New(err.Error())})
	}
	if _, err := projectpkg.ParseExtraMountsText(in.extraMounts); err != nil {
		return common.Classify(common.ErrValidation, &base.FieldError{Field: "preDeployExtraMounts", Err: errors.New(err.Error())})
	}

	return nil
}

func (s *GitOpsSyncService) validateLifecycleScriptConfigInternal(ctx context.Context, current *projectpkg.GitOpsSync, in lifecycleConfigInputInternal, scriptPath string) error {
	if resolveLifecycleEffectiveTargetType(current, in.targetType) == "swarm_stack" {
		return common.Classify(
			common.ErrValidation,
			&base.FieldError{
				Field: "preDeployScriptPath",
				//nolint:staticcheck // Preserve the existing error message.
				Err: errors.New(
					"Pre-deploy lifecycle hooks are only supported for project syncs.",
				),
			},
		)
	}

	if len(scriptPath) > 256 {
		return common.Classify(
			common.ErrValidation,
			&base.FieldError{
				Field: "preDeployScriptPath",
				//nolint:staticcheck // Preserve the existing error message.
				Err: errors.New(
					"Script path must be 256 characters or fewer.",
				),
			},
		)
	}

	// scriptPath is a POSIX repo path, not a host path; use path.IsAbs so the
	// check behaves the same on Windows-based contributor machines.
	if path.IsAbs(filepath.ToSlash(scriptPath)) {
		return common.Classify(
			common.ErrValidation,
			&base.FieldError{
				Field: "preDeployScriptPath",
				//nolint:staticcheck // Preserve the existing error message.
				Err: errors.New(
					"Script path must be relative to the project directory.",
				),
			},
		)
	}
	cleaned := filepath.ToSlash(filepath.Clean(scriptPath))
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return common.Classify(
			common.ErrValidation,
			&base.FieldError{
				Field: "preDeployScriptPath",
				//nolint:staticcheck // Preserve the existing error message.
				Err: errors.New(
					"Script path must not escape the project directory.",
				),
			},
		)
	}

	effectiveRunnerImage := resolveLifecycleEffectiveString(currentString(current, func(c *projectpkg.GitOpsSync) *string { return c.PreDeployRunnerImage }), in.runnerImage)
	defaultRunnerImage := strings.TrimSpace(s.settingsService.GetStringSetting(ctx, "lifecycleDefaultRunnerImage", ""))
	if effectiveRunnerImage == "" && defaultRunnerImage == "" {
		return common.Classify(
			common.ErrValidation,
			&base.FieldError{
				Field: "preDeployRunnerImage",
				//nolint:staticcheck // Preserve the existing error message.
				Err: errors.New(
					"Runner image is required when a script path is set.",
				),
			},
		)
	}

	if !resolveEffectiveSyncDirectory(current, in.syncDirectory) {
		return common.Classify(
			common.ErrValidation,
			&base.FieldError{
				Field: "preDeployScriptPath",
				//nolint:staticcheck // Preserve the existing error message.
				Err: errors.New(
					"Pre-deploy script requires \"Sync entire directory\" so the script is included in the synced files.",
				),
			},
		)
	}

	return nil
}

// applyLifecycleFieldsToSyncInternal copies lifecycle config from a Create
// request into a new GitOpsSync row before insert. Strings are trimmed;
// empty strings remain unset (nil pointer) except for fields that have a
// non-null default at the DB level.
func applyLifecycleFieldsToSyncInternal(syncRecord *projectpkg.GitOpsSync, in lifecycleConfigInputInternal) {
	syncRecord.PreDeployScriptPath = nullableTrimmedString(in.scriptPath)
	syncRecord.PreDeployRunnerImage = nullableTrimmedString(in.runnerImage)
	syncRecord.PreDeployEnv = nullableTrimmedString(in.env)
	syncRecord.PreDeployExtraMounts = nullableTrimmedString(in.extraMounts)
	if in.timeoutSec != nil {
		syncRecord.PreDeployTimeoutSec = *in.timeoutSec
	} else {
		syncRecord.PreDeployTimeoutSec = lifecycle.DefaultTimeoutSec
	}
	if mode := normalizeLifecycleNetworkMode(in.networkMode); mode != "" {
		syncRecord.PreDeployNetworkMode = mode
	}
}

// addLifecycleUpdatesInternal appends lifecycle field updates to the GORM
// updates map. nil pointers leave the field unchanged; non-nil empty strings
// clear nullable fields to NULL and reset fixed-default fields to their
// default value.
func addLifecycleUpdatesInternal(updates map[string]any, in lifecycleConfigInputInternal) {
	if in.scriptPath != nil {
		updates["pre_deploy_script_path"] = nullableUpdateStringValue(in.scriptPath)
	}
	if in.runnerImage != nil {
		updates["pre_deploy_runner_image"] = nullableUpdateStringValue(in.runnerImage)
	}
	if in.env != nil {
		updates["pre_deploy_env"] = nullableUpdateStringValue(in.env)
	}
	if in.extraMounts != nil {
		updates["pre_deploy_extra_mounts"] = nullableUpdateStringValue(in.extraMounts)
	}
	if in.timeoutSec != nil {
		updates["pre_deploy_timeout_sec"] = *in.timeoutSec
	}
	if in.networkMode != nil {
		updates["pre_deploy_network_mode"] = normalizeLifecycleNetworkMode(in.networkMode)
	}
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
		jobs:            entityjobs.New(entityjobs.GitOpsSyncJobPrefix, gitOpsSyncAdmissionScopeInternal),
		backups:         newBackupRuntimeInternal(),
	}
	s.sync = gitopssync.New(db, repoService, projectService, swarmService, eventService, s.getEffectiveSyncLimits, s.logSyncError, s.unregisterSyncJobInternal)
	s.backup = backup.New(db, repoService, projectService, eventService, s.getEffectiveSyncLimits, s.logSyncError, s.backups.branchLock)
	return s
}

// SetScheduler injects the job scheduler and the app lifecycle context. It must be
// called during bootstrap (after the service graph is built) before any per-sync
// jobs are registered. The lifecycle context is used for background sync kicks so
// they outlive the request/bootstrap goroutine that triggered them.
func (s *GitOpsSyncService) SetScheduler(ctx context.Context, jobScheduler scheduler.DynamicScheduler, admissionGate *runs.Admission) error {
	return s.jobs.SetScheduler(ctx, jobScheduler, admissionGate)
}

// runScheduledSyncInternal is the body of a scheduled sync fire. It re-reads the
// sync each run so a row toggled to AutoSync=false self-cancels. If the row is gone
// (e.g. deleted out-of-band via raw SQL), the job unregisters itself instead of
// firing forever; transient load errors are skipped and retried next tick.
func (s *GitOpsSyncService) runScheduledSyncInternal(ctx context.Context, environmentID, syncID string) (scheduler.Outcome, error) {
	syncRecord, err := s.getSyncRecordByIDInternal(ctx, environmentID, syncID)
	if err != nil {
		if errors.Is(err, common.ErrNotFound) {
			slog.InfoContext(ctx, "gitops auto-sync job unregistering; sync no longer exists", "syncId", syncID)
			s.unregisterSyncJobInternal(ctx, syncID)
			return scheduler.Outcome{Status: scheduler.Skipped}, nil
		}
		slog.DebugContext(ctx, "gitops auto-sync skipped; failed to load sync", "syncId", syncID, "error", err)
		return scheduler.Outcome{}, err
	}
	if !syncRecord.AutoSync {
		return scheduler.Outcome{Status: scheduler.Skipped}, nil
	}
	if previous, ok := jobcontext.Run(ctx); ok {
		outcome := jobcontext.ConfirmedTarget(previous, syncID)
		if outcome.Status == scheduler.Succeeded {
			return outcome, nil
		}
	}
	if progressErr := jobcontext.Progress(ctx, scheduler.TargetOutcome{ID: syncID, Status: scheduler.Running}); progressErr != nil {
		return scheduler.Outcome{}, progressErr
	}
	result, err := s.PerformSync(ctx, environmentID, syncID, user.SystemUser)
	if err != nil {
		slog.ErrorContext(ctx, "gitops auto-sync run failed", "syncId", syncID, "error", err)
		return scheduler.Outcome{}, err
	}
	if !result.Success {
		return scheduler.Outcome{Status: scheduler.Skipped, Message: result.Message}, nil
	}
	if completeProgressErr := jobcontext.Progress(ctx, scheduler.TargetOutcome{ID: syncID, Status: scheduler.Succeeded}); completeProgressErr != nil {
		return scheduler.Outcome{}, completeProgressErr
	}
	return scheduler.Outcome{Status: scheduler.Succeeded, Message: result.Message}, nil
}

// registerSyncJobInternal schedules a single sync on a fixed "@every Nm"
// interval. The run body re-reads the sync each fire, so a row deleted or
// toggled to AutoSync=false self-cancels cleanly, and delegates to PerformSync,
// which owns its own timeout and the per-sync in-flight guard.
func (s *GitOpsSyncService) registerSyncJobInternal(ctx context.Context, syncID, environmentID string, intervalMinutes int) {
	schedule := fmt.Sprintf("@every %dm", max(intervalMinutes, 1))
	s.jobs.Register(ctx, syncID,
		func(_ context.Context) string { return schedule },
		func(ctx context.Context) (scheduler.Outcome, error) {
			return s.runScheduledSyncInternal(ctx, environmentID, syncID)
		},
		func(ctx context.Context, previous scheduler.Run) (scheduler.Outcome, error) {
			outcome := jobcontext.ConfirmedTarget(previous, syncID)
			if outcome.Status == scheduler.Succeeded {
				return outcome, nil
			}
			record, err := s.getSyncRecordByIDInternal(ctx, environmentID, syncID)
			if err != nil {
				return outcome, err
			}
			return gitopssync.ReconcileRevision(ctx, previous, record, outcome)
		},
	)
}

func (s *GitOpsSyncService) unregisterSyncJobInternal(ctx context.Context, syncID string) {
	s.jobs.Unregister(ctx, syncID)
}

// kickSyncInternal runs a sync once in the background on the app lifecycle context.
// Used when auto-sync is freshly enabled or when a sync is overdue at startup, so
// the first run does not wait a full interval.
func (s *GitOpsSyncService) kickSyncInternal(ctx context.Context, syncID string) {
	if !s.jobs.Enabled() {
		return
	}
	if _, err := s.jobs.Scheduler().Submit(ctx, scheduler.Request{JobID: s.jobs.JobName(syncID), EnvironmentID: "0", Trigger: "startup"}); err != nil {
		slog.ErrorContext(ctx, "gitops immediate sync admission failed", "syncId", syncID, "error", err)
	}
}

// RegisterAutoSyncJobsOnStartup registers a dynamic job for every auto-sync-enabled
// sync and kicks an immediate run for any that are overdue. This replaces the old
// global polling job so existing syncs keep running after upgrade.
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
	for i := range syncs {
		syncRecord := syncs[i]
		s.registerSyncJobInternal(ctx, syncRecord.ID, syncRecord.EnvironmentID, syncRecord.SyncInterval)
		if isGitOpsSyncOverdue(&syncRecord) || (syncRecord.Mode == gitops.SyncModeBackup && syncRecord.BackupPending) {
			s.kickSyncInternal(ctx, syncRecord.ID)
		}
	}
	slog.InfoContext(ctx, "Registered gitops auto-sync jobs on startup", "count", len(syncs))
}

func (s *GitOpsSyncService) getEnvironmentSyncLimits(ctx context.Context) (int, int64, int64) {
	if s.settingsService == nil {
		return defaultMaxSyncFiles, defaultMaxSyncTotalSize, defaultMaxSyncBinarySize
	}

	cfg := s.settingsService.GetSettingsOrDefaults(ctx)
	maxFiles := normalizeSyncLimitSetting(kit.ParseOrDefault(cfg.GitSyncMaxFiles.Value, defaultMaxSyncFiles, strconv.Atoi), defaultMaxSyncFiles)
	maxTotalSizeMB := normalizeSyncLimitSetting(kit.ParseOrDefault(cfg.GitSyncMaxTotalSizeMb.Value, defaultMaxSyncTotalSizeMB, strconv.Atoi), defaultMaxSyncTotalSizeMB)
	maxBinarySizeMB := normalizeSyncLimitSetting(kit.ParseOrDefault(cfg.GitSyncMaxBinarySizeMb.Value, defaultMaxSyncBinarySizeMB, strconv.Atoi), defaultMaxSyncBinarySizeMB)

	return maxFiles, megabytesToBytes(maxTotalSizeMB), megabytesToBytes(maxBinarySizeMB)
}

func (s *GitOpsSyncService) getEffectiveSyncLimits(ctx context.Context, syncRecord *projectpkg.GitOpsSync) (int, int64, int64) {
	environmentMaxFiles, environmentMaxTotalSize, environmentMaxBinarySize := s.getEnvironmentSyncLimits(ctx)
	if syncRecord == nil {
		return environmentMaxFiles, environmentMaxTotalSize, environmentMaxBinarySize
	}

	maxFiles := syncRecord.MaxSyncFiles
	maxTotalSize := syncRecord.MaxSyncTotalSize
	maxBinarySize := syncRecord.MaxSyncBinarySize

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
	return s.settingsService != nil && s.settingsService.IsEnvOverrideActive(key)
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

	counts, err := s.getFilteredSyncCounts(q)
	if err != nil {
		return nil, pagination.Response{}, gitops.SyncCounts{}, fmt.Errorf("failed to get sync counts: %w", err)
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

	var backupSyncs int64
	if err := query.Session(&gorm.Session{}).Where("mode = ?", gitops.SyncModeBackup).Count(&backupSyncs).Error; err != nil {
		return gitops.SyncCounts{}, err
	}

	return gitops.SyncCounts{
		TotalSyncs:      int(totalSyncs),
		ActiveSyncs:     int(activeSyncs),
		SuccessfulSyncs: int(successfulSyncs),
		DeploySyncs:     int(totalSyncs - backupSyncs),
		BackupSyncs:     int(backupSyncs),
	}, nil
}

func (s *GitOpsSyncService) GetSyncByID(ctx context.Context, environmentID, id string) (*projectpkg.GitOpsSync, error) {
	syncRecord, err := s.getSyncByIDInternal(ctx, environmentID, id, true)
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

func (s *GitOpsSyncService) getSyncByIDInternal(ctx context.Context, environmentID, id string, preloadAssociations bool) (*projectpkg.GitOpsSync, error) {
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

func (s *GitOpsSyncService) getSyncRecordByIDInternal(ctx context.Context, environmentID, id string) (*projectpkg.GitOpsSync, error) {
	return s.getSyncByIDInternal(ctx, environmentID, id, false)
}

func (s *GitOpsSyncService) CreateSync(ctx context.Context, environmentID string, req gitops.CreateSyncRequest, actor user.Actor) (*projectpkg.GitOpsSync, error) {
	slog.InfoContext(ctx, "Creating GitOps sync", "environmentId", environmentID, "name", req.Name, "repositoryId", req.RepositoryID)

	mode, err := normalizeSyncMode(req.Mode)
	if err != nil {
		return nil, err
	}
	if mode == gitops.SyncModeDeploy && req.HasBackupOptions() {
		return nil, common.Classify(common.ErrValidation, &base.FieldError{Field: "mode", Err: errors.New("backup options require mode \"backup\"")})
	}
	if mode == gitops.SyncModeDeploy && strings.TrimSpace(req.ComposePath) == "" {
		return nil, common.Classify(common.ErrValidation, &base.FieldError{Field: "composePath", Err: errors.New("compose path is required")})
	}

	repo, err := s.repoService.GetRepositoryByID(ctx, req.RepositoryID)
	if err != nil {
		slog.ErrorContext(ctx, "Repository not found for GitOps sync", "repositoryId", req.RepositoryID, "error", err)
		return nil, fmt.Errorf("repository not found: %w", err)
	}
	slog.InfoContext(ctx, "Found repository for GitOps sync", "repositoryId", req.RepositoryID, "repositoryName", repo.Name)

	// Store the project name - use sync name if project name not provided
	projectName := cmp.Or(req.ProjectName, req.Name)

	defaultMaxFiles, defaultMaxTotalSize, defaultMaxBinarySize := s.getEnvironmentSyncLimits(ctx)

	syncRecord := projectpkg.GitOpsSync{
		Name:              req.Name,
		EnvironmentID:     environmentID,
		RepositoryID:      req.RepositoryID,
		Branch:            req.Branch,
		ComposePath:       req.ComposePath,
		TargetType:        req.TargetType,
		ProjectName:       projectName,
		ProjectID:         nil, // Will be set during first sync
		Mode:              mode,
		AutoSync:          false,
		SyncInterval:      60,
		SyncDirectory:     false, // Default to single-file sync
		MaxSyncFiles:      defaultMaxFiles,
		MaxSyncTotalSize:  defaultMaxTotalSize,
		MaxSyncBinarySize: defaultMaxBinarySize,
		BackupOnSave:      true,
	}

	linkProjectID := strings.TrimSpace(req.ProjectID)

	if req.AutoSync != nil {
		syncRecord.AutoSync = *req.AutoSync
	}
	if req.SyncInterval != nil {
		syncRecord.SyncInterval = *req.SyncInterval
	}
	if req.SyncDirectory != nil {
		syncRecord.SyncDirectory = *req.SyncDirectory
	}
	if req.PullImageAfterSync != nil {
		syncRecord.PullImageAfterSync = *req.PullImageAfterSync
	}
	if req.RedeployAfterSync != nil {
		syncRecord.RedeployAfterSync = *req.RedeployAfterSync
	}
	if validateSyncLimitsErr := validateSyncLimits(req.MaxSyncFiles, req.MaxSyncTotalSize, req.MaxSyncBinarySize); validateSyncLimitsErr != nil {
		return nil, validateSyncLimitsErr
	}
	if req.MaxSyncFiles != nil {
		syncRecord.MaxSyncFiles = *req.MaxSyncFiles
	}
	if req.MaxSyncTotalSize != nil {
		syncRecord.MaxSyncTotalSize = *req.MaxSyncTotalSize
	}
	if req.MaxSyncBinarySize != nil {
		syncRecord.MaxSyncBinarySize = *req.MaxSyncBinarySize
	}

	lifecycleCfg := lifecycleConfigInputInternal{
		targetType:    &req.TargetType,
		scriptPath:    req.PreDeployScriptPath,
		runnerImage:   req.PreDeployRunnerImage,
		env:           req.PreDeployEnv,
		extraMounts:   req.PreDeployExtraMounts,
		timeoutSec:    req.PreDeployTimeoutSec,
		networkMode:   req.PreDeployNetworkMode,
		syncDirectory: req.SyncDirectory,
	}
	if validateLifecycleConfigErr := s.validateLifecycleConfigInternal(ctx, nil, lifecycleCfg); validateLifecycleConfigErr != nil {
		return nil, validateLifecycleConfigErr
	}
	applyLifecycleFieldsToSyncInternal(&syncRecord, lifecycleCfg)

	adoptedProject, err := s.insertSyncRecordInternal(ctx, &syncRecord, req, mode, linkProjectID)
	if err != nil {
		return nil, err
	}
	slog.InfoContext(ctx, "GitOps sync created successfully", "syncId", syncRecord.ID, "name", syncRecord.Name)

	if adoptedProject != nil {
		adoptedProject.GitOpsManagedBy = &syncRecord.ID
		if ensureGitOpsProjectLinkedErr := s.projectService.EnsureGitOpsProjectLinked(ctx, &syncRecord, adoptedProject); ensureGitOpsProjectLinkedErr != nil {
			return nil, fmt.Errorf("failed to link existing project: %w", ensureGitOpsProjectLinkedErr)
		}
	}

	// Log event
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

	if _, performSyncErr := s.PerformSync(ctx, syncRecord.EnvironmentID, syncRecord.ID, actor); performSyncErr != nil {
		slog.ErrorContext(ctx, "Failed to perform initial sync after creation", "syncId", syncRecord.ID, "error", performSyncErr)
		// Don't fail the entire creation - the sync config exists and can be retried
	}

	// Register the recurring job if auto-sync is on. The initial sync above already
	// ran once, so no extra kick is needed here.
	if syncRecord.AutoSync {
		s.registerSyncJobInternal(ctx, syncRecord.ID, syncRecord.EnvironmentID, syncRecord.SyncInterval)
	}

	return s.GetSyncByID(ctx, "", syncRecord.ID)
}

// prepareDeployProjectLinkInternal validates that an existing project can be
// adopted by a deploy sync: it must exist, not be deployed from Git already,
// and not be backed up to Git.
func (s *GitOpsSyncService) prepareDeployProjectLinkInternal(ctx context.Context, tx *gorm.DB, req gitops.CreateSyncRequest) (*projectpkg.Project, error) {
	if strings.TrimSpace(req.TargetType) == "swarm_stack" {
		return nil, common.Classify(common.ErrValidation, &base.FieldError{Field: "projectId", Err: errors.New("an existing project cannot be linked to a swarm stack sync")})
	}
	project, err := projectpkg.LockProjectForSync(tx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	if project.GitOpsManagedBy != nil && strings.TrimSpace(*project.GitOpsManagedBy) != "" {
		return nil, common.Classify(common.ErrConflict, errors.New("project is already deployed from Git"))
	}
	var backups int64
	if countBackupsErr := tx.Model(&projectpkg.GitOpsSync{}).
		Where("mode = ? AND project_id = ?", gitops.SyncModeBackup, project.ID).
		Count(&backups).Error; countBackupsErr != nil {
		return nil, fmt.Errorf("failed to check existing backups: %w", countBackupsErr)
	}
	if backups > 0 {
		return nil, common.Classify(common.ErrConflict, errors.New("project is backed up to Git; disconnect that backup before deploying it from Git"))
	}
	if ensureProjectPathUnderRootErr := s.projectService.EnsureProjectPathUnderRoot(ctx, project, false); ensureProjectPathUnderRootErr != nil {
		return nil, ensureProjectPathUnderRootErr
	}
	return project, nil
}

// insertSyncRecordInternal validates the project link and inserts the sync in
// one transaction with the project row locked, so a project cannot end up with
// two Git relationships. It returns the adopted project for deploy links.
func (s *GitOpsSyncService) insertSyncRecordInternal(ctx context.Context, syncRecord *projectpkg.GitOpsSync, req gitops.CreateSyncRequest, mode, linkProjectID string) (*projectpkg.Project, error) {
	var adoptedProject *projectpkg.Project
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if mode == gitops.SyncModeDeploy && linkProjectID != "" {
			project, err := s.prepareDeployProjectLinkInternal(ctx, tx, req)
			if err != nil {
				return err
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

		// Select("*") forces explicit zero values (e.g. "0 = unlimited" sync limits and
		// unset pre-deploy fields) to persist instead of GORM substituting column defaults.
		if err := tx.Select("*").Omit("Environment", "Repository", "Project").Create(syncRecord).Error; err != nil { //nolint:unqueryvet // intentional Select("*"); see comment above
			if isUniqueViolation(err) {
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
	if err != nil {
		return nil, err
	}
	return adoptedProject, nil
}

func (s *GitOpsSyncService) UpdateSync(ctx context.Context, environmentID, id string, req gitops.UpdateSyncRequest, actor user.Actor) (*projectpkg.GitOpsSync, error) {
	syncRecord, err := s.GetSyncByID(ctx, environmentID, id)
	if err != nil {
		return nil, err
	}

	// Capture state needed to reconcile the dynamic job after the update.
	oldAutoSync := syncRecord.AutoSync
	newAutoSync := syncRecord.AutoSync
	if req.AutoSync != nil {
		newAutoSync = *req.AutoSync
	}
	newInterval := syncRecord.SyncInterval
	if req.SyncInterval != nil {
		newInterval = *req.SyncInterval
	}

	updates := make(map[string]any)

	if req.Name != nil {
		updates["name"] = *req.Name
	}
	if req.RepositoryID != nil {
		// Validate repository exists
		_, getRepositoryByIDErr := s.repoService.GetRepositoryByID(ctx, *req.RepositoryID)
		if getRepositoryByIDErr != nil {
			return nil, fmt.Errorf("repository not found: %w", getRepositoryByIDErr)
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
	if validateSyncLimitsErr := validateSyncLimits(req.MaxSyncFiles, req.MaxSyncTotalSize, req.MaxSyncBinarySize); validateSyncLimitsErr != nil {
		return nil, validateSyncLimitsErr
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

	if applyModeUpdatesErr := s.backup.ApplyModeUpdates(ctx, syncRecord, req, updates); applyModeUpdatesErr != nil {
		return nil, applyModeUpdatesErr
	}

	lifecycleCfg := lifecycleConfigInputInternal{
		targetType:    req.TargetType,
		scriptPath:    req.PreDeployScriptPath,
		runnerImage:   req.PreDeployRunnerImage,
		env:           req.PreDeployEnv,
		extraMounts:   req.PreDeployExtraMounts,
		timeoutSec:    req.PreDeployTimeoutSec,
		networkMode:   req.PreDeployNetworkMode,
		syncDirectory: req.SyncDirectory,
	}
	if validateLifecycleConfigErr := s.validateLifecycleConfigInternal(ctx, syncRecord, lifecycleCfg); validateLifecycleConfigErr != nil {
		return nil, validateLifecycleConfigErr
	}
	addLifecycleUpdatesInternal(updates, lifecycleCfg)

	if len(updates) > 0 {
		// Loaded associations must not overwrite explicitly updated foreign keys.
		if updateSyncErr := s.db.WithContext(ctx).Model(syncRecord).Omit(clause.Associations).Updates(updates).Error; updateSyncErr != nil {
			return nil, fmt.Errorf("failed to update sync: %w", updateSyncErr)
		}

		// Log event
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

	// Reconcile the dynamic job to match the new state.
	switch {
	case newAutoSync:
		s.registerSyncJobInternal(ctx, syncRecord.ID, syncRecord.EnvironmentID, newInterval)
		if !oldAutoSync {
			// Freshly enabled — kick a run now so it doesn't wait a full interval.
			s.kickSyncInternal(ctx, syncRecord.ID)
		}
	default:
		s.unregisterSyncJobInternal(ctx, syncRecord.ID)
	}

	return s.GetSyncByID(ctx, environmentID, id)
}

func (s *GitOpsSyncService) DeleteSync(ctx context.Context, environmentID, id string, actor user.Actor) error {
	// Stop the recurring job first, unconditionally. Even a sync whose row can no
	// longer be loaded (corrupt or environment-mismatched) must stop firing; any
	// in-flight run re-reads the row and self-cancels once it is gone.
	s.unregisterSyncJobInternal(ctx, id)
	s.backups.cancel(id)

	// Best-effort load for the audit-event metadata. A corrupt or env-mismatched row
	// must still be deletable, so a load failure falls through to the direct delete
	// below instead of aborting.
	syncRecord, loadErr := s.getSyncRecordByIDInternal(ctx, environmentID, id)

	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Clear gitops_managed_by for any project still pointing at this sync, keyed
		// on the sync id so orphaned managed flags are cleared even when the sync row
		// (and its ProjectID) could not be loaded.
		if err := tx.Model(&projectpkg.Project{}).
			Where("gitops_managed_by = ?", id).
			Update("gitops_managed_by", nil).Error; err != nil {
			return fmt.Errorf("failed to clear gitops_managed_by: %w", err)
		}
		if err :=

			// Delete by id only (no environment scoping). The handler already enforced the
			// delete permission; env scoping is precisely what made env-mismatched corrupt
			// rows undeletable. A zero-row delete is treated as success (idempotent).

			tx.Where("id = ?", id).Delete(&projectpkg.GitOpsSync{}).Error; err != nil {
			return fmt.Errorf("failed to delete sync: %w", err)
		}
		return nil
	}); err != nil {
		// Re-register the recurring job only when we actually loaded an auto-sync row.
		if loadErr == nil && syncRecord.AutoSync {
			s.registerSyncJobInternal(ctx, syncRecord.ID, syncRecord.EnvironmentID, syncRecord.SyncInterval)
		}
		return err
	}

	if loadErr != nil {
		slog.WarnContext(ctx, "Deleted GitOps sync whose record could not be loaded", "syncId", id, "loadError", loadErr)
		return nil
	}

	// Log event
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
	return s.performSyncAdmittedInternal(ctx, environmentID, id, actor, false)
}

// performSyncAdmittedInternal runs one sync under the per-sync admission lease
// and dispatches by mode. backupAdopt makes a backup run replace whatever the
// remote holds in its backup directory.
func (s *GitOpsSyncService) performSyncAdmittedInternal(ctx context.Context, environmentID, id string, actor user.Actor, backupAdopt bool) (*gitops.SyncResult, error) {
	// Coalesce overlapping runs for the same sync (scheduled fire, startup/enable
	// kick, manual trigger, webhook) so they don't race the clone/redeploy.
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

// CleanupLeakedScratchDirsOnStartup removes orphaned GitOps scratch directories
// (.gitops-sync-stage-*, .gitops-backup-*, and the legacy "<name>.gitops-backup-<digits>"
// form) left in the projects directory by a crash or container restart that interrupted
// a sync mid-flight. These are Arcane-internal working dirs; left in place the filesystem
// discovery imports them as phantom projects. It is safe to run at startup because it
// executes before any sync job is registered, so nothing is mid-stage.
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

func (s *GitOpsSyncService) logSyncError(ctx context.Context, syncRecord *projectpkg.GitOpsSync, actor user.Actor, errorMsg string) {
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

// backupRuntimeInternal holds the in-memory coordination for backup syncs
type backupRuntimeInternal struct {
	mu       sync.Mutex
	branches map[string]*sync.Mutex
	timers   map[string]*time.Timer
	debounce time.Duration
}

func newBackupRuntimeInternal() *backupRuntimeInternal {
	return &backupRuntimeInternal{
		branches: make(map[string]*sync.Mutex),
		timers:   make(map[string]*time.Timer),
		debounce: backup.DefaultBackupSaveDebounce,
	}
}

func (r *backupRuntimeInternal) branchLock(repositoryID, branch string) *sync.Mutex {
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

func (r *backupRuntimeInternal) schedule(syncID string, run func()) {
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

func (r *backupRuntimeInternal) cancel(syncID string) {
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
	syncRecord, err := s.getBackupSyncInternal(ctx, environmentID, id)
	if err != nil {
		return nil, err
	}
	return s.performSyncAdmittedInternal(ctx, syncRecord.EnvironmentID, syncRecord.ID, actor, true)
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
					if _, err := s.jobs.Scheduler().Submit(runCtx, scheduler.Request{JobID: s.jobs.JobName(syncID), EnvironmentID: "0", Trigger: "save"}); err != nil {
						slog.ErrorContext(runCtx, "git backup admission after save failed", "syncId", syncID, "error", err)
					}
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

	syncRecord, err := s.getBackupSyncInternal(previewCtx, environmentID, id)
	if err != nil {
		return nil, err
	}
	return s.backup.Preview(previewCtx, syncRecord)
}

// GetBackupHistory lists revisions that touched the backup directory.
func (s *GitOpsSyncService) GetBackupHistory(ctx context.Context, environmentID, id string, limit int) (*gitops.BackupHistoryResponse, error) {
	historyCtx, cancel := context.WithTimeout(ctx, defaultGitSyncTimeout)
	defer cancel()

	syncRecord, err := s.getBackupSyncInternal(historyCtx, environmentID, id)
	if err != nil {
		return nil, err
	}
	return s.backup.History(historyCtx, syncRecord, limit)
}

// GetBackupRevision returns one revision with per-file diffs inside the backup directory.
func (s *GitOpsSyncService) GetBackupRevision(ctx context.Context, environmentID, id, commit string) (*gitops.BackupRevision, error) {
	revisionCtx, cancel := context.WithTimeout(ctx, defaultGitSyncTimeout)
	defer cancel()

	syncRecord, err := s.getBackupSyncInternal(revisionCtx, environmentID, id)
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

func (s *GitOpsSyncService) getBackupSyncInternal(ctx context.Context, environmentID, id string) (*projectpkg.GitOpsSync, error) {
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
