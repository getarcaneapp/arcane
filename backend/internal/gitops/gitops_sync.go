package gitops

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/actors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/gitrepo"
	projectpkg "github.com/getarcaneapp/arcane/backend/v2/internal/project"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/internal/swarm"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/entityjobs"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/mapper"
	"github.com/getarcaneapp/arcane/types/v2/gitops"
	projecttypes "github.com/getarcaneapp/arcane/types/v2/project"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
	swarmtypes "github.com/getarcaneapp/arcane/types/v2/swarm"
	"go.getarcane.app/acfs"
	acfstypes "go.getarcane.app/acfs/types"
	"gorm.io/gorm"
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
	repoPath         string
	commitHash       string
	composeContent   string
	envContent       *string
	overrideContent  *string
	overrideFileName string
}

// stagedDirectorySync holds the fully prepared directory-sync result before it
// is promoted into the live project path.
type stagedDirectorySync struct {
	stagePath string
	// stageLogical is stagePath expressed relative to the projects directory,
	// which is the confinement root every staging mutation runs against.
	stageLogical    string
	projectsDir     string
	composeFileName string
	project         *projectpkg.Project
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
	targetTypeChanging := in.targetType != nil && strings.TrimSpace(*in.targetType) != resolveLifecycleEffectiveTargetTypeInternal(current, nil)
	effectiveScriptPath := resolveLifecycleEffectiveStringInternal(currentStringInternal(current, func(c *projectpkg.GitOpsSync) *string { return c.PreDeployScriptPath }), in.scriptPath)

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
		//nolint:revive // Preserve the existing API validation message.
		return common.Classify(common.ErrValidation, errors.WithDetails(errors.New("Pre-deploy lifecycle hooks are disabled. An admin must enable lifecycleEnabled in settings before they can be configured."), "field", "preDeployScriptPath"))
	}

	if effectiveScriptPath != "" {
		if err := s.validateLifecycleScriptConfigInternal(ctx, current, in, effectiveScriptPath); err != nil {
			return err
		}
	}

	if in.timeoutSec != nil {
		if *in.timeoutSec < 1 {
			//nolint:revive // Preserve the existing API validation message.
			return common.Classify(common.ErrValidation, errors.WithDetails(errors.New("Timeout must be at least 1 second."), "field", "preDeployTimeoutSec"))
		}
		maxTimeoutSec := s.settingsService.GetIntSetting(ctx, "lifecycleMaxTimeoutSec", projectpkg.DefaultMaxTimeoutSec)
		if maxTimeoutSec > 0 && *in.timeoutSec > maxTimeoutSec {
			//nolint:revive // Preserve the existing API validation message.
			return common.Classify(common.ErrValidation, errors.WithDetails(errors.Errorf("Timeout %ds exceeds the lifecycleMaxTimeoutSec setting (%ds).", *in.timeoutSec, maxTimeoutSec), "field", "preDeployTimeoutSec"))
		}
	}

	if _, err := projectpkg.ParseEnvText(in.env); err != nil {
		return common.Classify(common.ErrValidation, errors.WithDetails(errors.New(err.Error()), "field", "preDeployEnv"))
	}
	if _, err := projectpkg.ParseExtraMountsText(in.extraMounts); err != nil {
		return common.Classify(common.ErrValidation, errors.WithDetails(errors.New(err.Error()), "field", "preDeployExtraMounts"))
	}

	return nil
}

func (s *GitOpsSyncService) validateLifecycleScriptConfigInternal(ctx context.Context, current *projectpkg.GitOpsSync, in lifecycleConfigInputInternal, scriptPath string) error {
	if resolveLifecycleEffectiveTargetTypeInternal(current, in.targetType) == "swarm_stack" {
		//nolint:revive // Preserve the existing API validation message.
		return common.Classify(common.ErrValidation, errors.WithDetails(errors.New("Pre-deploy lifecycle hooks are only supported for project syncs."), "field", "preDeployScriptPath"))
	}

	if len(scriptPath) > 256 {
		//nolint:revive // Preserve the existing API validation message.
		return common.Classify(common.ErrValidation, errors.WithDetails(errors.New("Script path must be 256 characters or fewer."), "field", "preDeployScriptPath"))
	}
	// scriptPath is a POSIX repo path, not a host path; use path.IsAbs so the
	// check behaves the same on Windows-based contributor machines.
	if path.IsAbs(filepath.ToSlash(scriptPath)) {
		//nolint:revive // Preserve the existing API validation message.
		return common.Classify(common.ErrValidation, errors.WithDetails(errors.New("Script path must be relative to the project directory."), "field", "preDeployScriptPath"))
	}
	cleaned := filepath.ToSlash(filepath.Clean(scriptPath))
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		//nolint:revive // Preserve the existing API validation message.
		return common.Classify(common.ErrValidation, errors.WithDetails(errors.New("Script path must not escape the project directory."), "field", "preDeployScriptPath"))
	}

	effectiveRunnerImage := resolveLifecycleEffectiveStringInternal(currentStringInternal(current, func(c *projectpkg.GitOpsSync) *string { return c.PreDeployRunnerImage }), in.runnerImage)
	defaultRunnerImage := strings.TrimSpace(s.settingsService.GetStringSetting(ctx, "lifecycleDefaultRunnerImage", ""))
	if effectiveRunnerImage == "" && defaultRunnerImage == "" {
		//nolint:revive // Preserve the existing API validation message.
		return common.Classify(common.ErrValidation, errors.WithDetails(errors.New("Runner image is required when a script path is set."), "field", "preDeployRunnerImage"))
	}

	if !resolveEffectiveSyncDirectoryInternal(current, in.syncDirectory) {
		//nolint:revive // Preserve the existing API validation message.
		return common.Classify(common.ErrValidation, errors.WithDetails(errors.New("Pre-deploy script requires \"Sync entire directory\" so the script is included in the synced files."), "field", "preDeployScriptPath"))
	}

	return nil
}

// resolveLifecycleEffectiveStringInternal computes the value a string field
// will take after an update: a nil update leaves the existing value, a
// non-nil update (including empty string, which clears) overrides it. Used
// to validate the post-update state of a sync record.
func resolveLifecycleEffectiveStringInternal(existing string, update *string) string {
	if update != nil {
		return strings.TrimSpace(*update)
	}
	return strings.TrimSpace(existing)
}

func currentStringInternal(sync *projectpkg.GitOpsSync, accessor func(*projectpkg.GitOpsSync) *string) string {
	if sync == nil {
		return ""
	}
	if p := accessor(sync); p != nil {
		return *p
	}
	return ""
}

func resolveLifecycleEffectiveTargetTypeInternal(current *projectpkg.GitOpsSync, update *string) string {
	if update != nil && strings.TrimSpace(*update) != "" {
		return strings.TrimSpace(*update)
	}
	if current != nil && strings.TrimSpace(current.TargetType) != "" {
		return strings.TrimSpace(current.TargetType)
	}
	return "project"
}

// resolveEffectiveSyncDirectoryInternal mirrors the string resolver but for
// the SyncDirectory bool: a nil update keeps the existing value, a non-nil
// update overrides it. On create (current=nil) and no update, defaults to
// false to match the model's create-time default.
func resolveEffectiveSyncDirectoryInternal(current *projectpkg.GitOpsSync, update *bool) bool {
	if update != nil {
		return *update
	}
	if current != nil {
		return current.SyncDirectory
	}
	return false
}

// applyLifecycleFieldsToSyncInternal copies lifecycle config from a Create
// request into a new GitOpsSync row before insert. Strings are trimmed;
// empty strings remain unset (nil pointer) except for fields that have a
// non-null default at the DB level.
func applyLifecycleFieldsToSyncInternal(sync *projectpkg.GitOpsSync, in lifecycleConfigInputInternal) {
	sync.PreDeployScriptPath = nullableTrimmedStringInternal(in.scriptPath)
	sync.PreDeployRunnerImage = nullableTrimmedStringInternal(in.runnerImage)
	sync.PreDeployEnv = nullableTrimmedStringInternal(in.env)
	sync.PreDeployExtraMounts = nullableTrimmedStringInternal(in.extraMounts)
	if in.timeoutSec != nil {
		sync.PreDeployTimeoutSec = *in.timeoutSec
	} else {
		sync.PreDeployTimeoutSec = projectpkg.DefaultTimeoutSec
	}
	if mode := normalizeLifecycleNetworkModeInternal(in.networkMode); mode != "" {
		sync.PreDeployNetworkMode = mode
	}
}

// addLifecycleUpdatesInternal appends lifecycle field updates to the GORM
// updates map. nil pointers leave the field unchanged; non-nil empty strings
// clear nullable fields to NULL and reset fixed-default fields to their
// default value.
func addLifecycleUpdatesInternal(updates map[string]any, in lifecycleConfigInputInternal) {
	if in.scriptPath != nil {
		updates["pre_deploy_script_path"] = nullableUpdateStringValueInternal(in.scriptPath)
	}
	if in.runnerImage != nil {
		updates["pre_deploy_runner_image"] = nullableUpdateStringValueInternal(in.runnerImage)
	}
	if in.env != nil {
		updates["pre_deploy_env"] = nullableUpdateStringValueInternal(in.env)
	}
	if in.extraMounts != nil {
		updates["pre_deploy_extra_mounts"] = nullableUpdateStringValueInternal(in.extraMounts)
	}
	if in.timeoutSec != nil {
		updates["pre_deploy_timeout_sec"] = *in.timeoutSec
	}
	if in.networkMode != nil {
		updates["pre_deploy_network_mode"] = normalizeLifecycleNetworkModeInternal(in.networkMode)
	}
}

// normalizeLifecycleNetworkModeInternal returns the canonical network mode
// string for a user-supplied value. Empty / whitespace / unset all map to
// "none" so the secure-by-default behaviour is restored when the user clears
// the field.
func normalizeLifecycleNetworkModeInternal(p *string) string {
	if p == nil {
		return "none"
	}
	trimmed := strings.TrimSpace(*p)
	if trimmed == "" {
		return "none"
	}
	return trimmed
}

func nullableTrimmedStringInternal(p *string) *string {
	if p == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*p)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func nullableUpdateStringValueInternal(p *string) any {
	if p == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*p)
	if trimmed == "" {
		return nil
	}
	return trimmed
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

func hasEstablishedProjectBindingInternal(sync *projectpkg.GitOpsSync) bool {
	return sync != nil && sync.ProjectID != nil && strings.TrimSpace(*sync.ProjectID) != ""
}

func NewGitOpsSyncService(db *database.DB, repoService *gitrepo.GitRepositoryService, projectService *projectpkg.ProjectService, swarmService *swarm.SwarmService, eventService *event.EventService, settingsService *settings.SettingsService) *GitOpsSyncService {
	return &GitOpsSyncService{
		db:              db,
		repoService:     repoService,
		projectService:  projectService,
		swarmService:    swarmService,
		eventService:    eventService,
		settingsService: settingsService,
		jobs:            entityjobs.New(entityjobs.GitOpsSyncJobPrefix, gitOpsSyncAdmissionScopeInternal),
	}
}

const gitOpsSyncAdmissionScopeInternal = "gitops-sync"

// SetScheduler injects the job scheduler and the app lifecycle context. It must be
// called during bootstrap (after the service graph is built) before any per-sync
// jobs are registered. The lifecycle context is used for background sync kicks so
// they outlive the request/bootstrap goroutine that triggered them.
func (s *GitOpsSyncService) SetScheduler(ctx context.Context, scheduler schedulertypes.DynamicScheduler, admissionGate *actors.Gate[actors.AdmissionKey]) error {
	return s.jobs.SetScheduler(ctx, scheduler, admissionGate)
}

// runScheduledSyncInternal is the body of a scheduled sync fire. It re-reads the
// sync each run so a row toggled to AutoSync=false self-cancels. If the row is gone
// (e.g. deleted out-of-band via raw SQL), the job unregisters itself instead of
// firing forever; transient load errors are skipped and retried next tick.
func (s *GitOpsSyncService) runScheduledSyncInternal(ctx context.Context, environmentID, syncID string) {
	syncRecord, err := s.getSyncRecordByIDInternal(ctx, environmentID, syncID)
	if err != nil {
		if errors.Is(err, common.ErrNotFound) {
			slog.InfoContext(ctx, "gitops auto-sync job unregistering; sync no longer exists", "syncId", syncID)
			s.unregisterSyncJobInternal(ctx, syncID)
			return
		}
		slog.DebugContext(ctx, "gitops auto-sync skipped; failed to load sync", "syncId", syncID, "error", err)
		return
	}
	if !syncRecord.AutoSync {
		return
	}
	if _, err := s.PerformSync(ctx, environmentID, syncID, common.SystemUser); err != nil {
		slog.ErrorContext(ctx, "gitops auto-sync run failed", "syncId", syncID, "error", err)
	}
}

// registerSyncJobInternal schedules a single sync on a fixed "@every Nm"
// interval. The run body re-reads the sync each fire, so a row deleted or
// toggled to AutoSync=false self-cancels cleanly, and delegates to PerformSync,
// which owns its own timeout and the per-sync in-flight guard.
func (s *GitOpsSyncService) registerSyncJobInternal(ctx context.Context, syncID, environmentID string, intervalMinutes int) {
	schedule := fmt.Sprintf("@every %dm", max(intervalMinutes, 1))
	s.jobs.Register(ctx, syncID,
		func(_ context.Context) string { return schedule },
		func(ctx context.Context) { s.runScheduledSyncInternal(ctx, environmentID, syncID) },
	)
}

func (s *GitOpsSyncService) unregisterSyncJobInternal(ctx context.Context, syncID string) {
	s.jobs.Unregister(ctx, syncID)
}

// kickSyncInternal runs a sync once in the background on the app lifecycle context.
// Used when auto-sync is freshly enabled or when a sync is overdue at startup, so
// the first run does not wait a full interval.
func (s *GitOpsSyncService) kickSyncInternal(ctx context.Context, syncID, environmentID string) {
	ctx = s.jobs.Context(ctx)
	go func() {
		if _, err := s.PerformSync(ctx, environmentID, syncID, common.SystemUser); err != nil {
			slog.ErrorContext(ctx, "gitops immediate sync kick failed", "syncId", syncID, "error", err)
		}
	}()
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
		if isGitOpsSyncOverdueInternal(&syncRecord) {
			s.kickSyncInternal(ctx, syncRecord.ID, syncRecord.EnvironmentID)
		}
	}
	slog.InfoContext(ctx, "Registered gitops auto-sync jobs on startup", "count", len(syncs))
}

func isGitOpsSyncOverdueInternal(sync *projectpkg.GitOpsSync) bool {
	if sync.LastSyncAt == nil {
		return true
	}
	interval := max(sync.SyncInterval, 1)
	return time.Now().After(sync.LastSyncAt.Add(time.Duration(interval) * time.Minute))
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

func (s *GitOpsSyncService) getEffectiveSyncLimits(ctx context.Context, sync *projectpkg.GitOpsSync) (int, int64, int64) {
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
	return s.settingsService != nil && s.settingsService.IsEnvOverrideActive(key)
}

func (s *GitOpsSyncService) GetSyncsPaginated(ctx context.Context, environmentID string, params pagination.QueryParams) ([]gitops.GitOpsSync, pagination.Response, gitops.SyncCounts, error) {
	var syncs []projectpkg.GitOpsSync
	q := s.db.WithContext(ctx).Model(&projectpkg.GitOpsSync{}).
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
		return nil, pagination.Response{}, gitops.SyncCounts{}, errors.WrapIf(err, "failed to get sync counts")
	}

	paginationResp, err := pagination.PaginateAndSortDB(params, q.Preload("Repository").Preload("Project"), &syncs)
	if err != nil {
		return nil, pagination.Response{}, gitops.SyncCounts{}, errors.WrapIf(err, "failed to paginate gitops syncs")
	}

	out, mapErr := mapper.MapSlice[projectpkg.GitOpsSync, gitops.GitOpsSync](syncs)
	if mapErr != nil {
		return nil, pagination.Response{}, gitops.SyncCounts{}, errors.WrapIf(mapErr, "failed to map syncs")
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

func (s *GitOpsSyncService) GetSyncByID(ctx context.Context, environmentID, id string) (*projectpkg.GitOpsSync, error) {
	syncRecord, err := s.getSyncByIDInternal(ctx, environmentID, id, true)
	if err != nil {
		if errors.Is(err, common.ErrNotFound) {
			slog.WarnContext(ctx, "GitOps sync not found", "syncID", id, "environmentID", environmentID)
			return nil, err
		}
		slog.ErrorContext(ctx, "Failed to get GitOps sync", "syncID", id, "environmentID", environmentID, "error", err)
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
		return nil, errors.WrapIf(err, "failed to get sync")
	}
	return &syncRecord, nil
}

func (s *GitOpsSyncService) getSyncRecordByIDInternal(ctx context.Context, environmentID, id string) (*projectpkg.GitOpsSync, error) {
	return s.getSyncByIDInternal(ctx, environmentID, id, false)
}

func (s *GitOpsSyncService) CreateSync(ctx context.Context, environmentID string, req gitops.CreateSyncRequest, actor common.User) (*projectpkg.GitOpsSync, error) {
	slog.InfoContext(ctx, "Creating GitOps sync", "environmentID", environmentID, "name", req.Name, "repositoryID", req.RepositoryID)

	// Validate repository exists
	repo, err := s.repoService.GetRepositoryByID(ctx, req.RepositoryID)
	if err != nil {
		slog.ErrorContext(ctx, "Repository not found for GitOps sync", "repositoryID", req.RepositoryID, "error", err)
		return nil, errors.WrapIf(err, "repository not found")
	}
	slog.InfoContext(ctx, "Found repository for GitOps sync", "repositoryID", req.RepositoryID, "repositoryName", repo.Name)

	// Store the project name - use sync name if project name not provided
	projectName := req.ProjectName
	if projectName == "" {
		projectName = req.Name
	}

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
		AutoSync:          false,
		SyncInterval:      60,
		SyncDirectory:     false, // Default to single-file sync
		MaxSyncFiles:      defaultMaxFiles,
		MaxSyncTotalSize:  defaultMaxTotalSize,
		MaxSyncBinarySize: defaultMaxBinarySize,
	}

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
	if err := validateSyncLimits(req.MaxSyncFiles, req.MaxSyncTotalSize, req.MaxSyncBinarySize); err != nil {
		return nil, err
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
	if err := s.validateLifecycleConfigInternal(ctx, nil, lifecycleCfg); err != nil {
		return nil, err
	}
	applyLifecycleFieldsToSyncInternal(&syncRecord, lifecycleCfg)

	// Select("*") forces explicit zero values (e.g. "0 = unlimited" sync limits and
	// unset pre-deploy fields) to persist instead of GORM substituting column defaults.
	if err := s.db.WithContext(ctx).Select("*").Omit("Environment", "Repository", "Project").Create(&syncRecord).Error; err != nil { //nolint:unqueryvet // intentional Select("*"); see comment above
		slog.ErrorContext(ctx, "Failed to create GitOps sync in database", "name", req.Name, "repositoryID", req.RepositoryID, "environmentID", environmentID, "error", err)
		return nil, errors.WrapIf(err, "failed to create sync")
	}
	slog.InfoContext(ctx, "GitOps sync created successfully", "syncID", syncRecord.ID, "name", syncRecord.Name)

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

	if _, err := s.PerformSync(ctx, syncRecord.EnvironmentID, syncRecord.ID, actor); err != nil {
		slog.ErrorContext(ctx, "Failed to perform initial sync after creation", "syncId", syncRecord.ID, "error", err)
		// Don't fail the entire creation - the sync config exists and can be retried
	}

	// Register the recurring job if auto-sync is on. The initial sync above already
	// ran once, so no extra kick is needed here.
	if syncRecord.AutoSync {
		s.registerSyncJobInternal(ctx, syncRecord.ID, syncRecord.EnvironmentID, syncRecord.SyncInterval)
	}

	return s.GetSyncByID(ctx, "", syncRecord.ID)
}

func (s *GitOpsSyncService) UpdateSync(ctx context.Context, environmentID, id string, req gitops.UpdateSyncRequest, actor common.User) (*projectpkg.GitOpsSync, error) {
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
		_, err := s.repoService.GetRepositoryByID(ctx, *req.RepositoryID)
		if err != nil {
			return nil, errors.WrapIf(err, "repository not found")
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
	if err := s.validateLifecycleConfigInternal(ctx, syncRecord, lifecycleCfg); err != nil {
		return nil, err
	}
	addLifecycleUpdatesInternal(updates, lifecycleCfg)

	if len(updates) > 0 {
		if err := s.db.WithContext(ctx).Model(syncRecord).Updates(updates).Error; err != nil {
			return nil, errors.WrapIf(err, "failed to update sync")
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
			s.kickSyncInternal(ctx, syncRecord.ID, syncRecord.EnvironmentID)
		}
	default:
		s.unregisterSyncJobInternal(ctx, syncRecord.ID)
	}

	return s.GetSyncByID(ctx, environmentID, id)
}

func (s *GitOpsSyncService) DeleteSync(ctx context.Context, environmentID, id string, actor common.User) error {
	// Stop the recurring job first, unconditionally. Even a sync whose row can no
	// longer be loaded (corrupt or environment-mismatched) must stop firing; any
	// in-flight run re-reads the row and self-cancels once it is gone.
	s.unregisterSyncJobInternal(ctx, id)

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
			return errors.WrapIf(err, "failed to clear gitops_managed_by")
		}

		// Delete by id only (no environment scoping). The handler already enforced the
		// delete permission; env scoping is precisely what made env-mismatched corrupt
		// rows undeletable. A zero-row delete is treated as success (idempotent).
		if err := tx.Where("id = ?", id).Delete(&projectpkg.GitOpsSync{}).Error; err != nil {
			return errors.WrapIf(err, "failed to delete sync")
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

func (s *GitOpsSyncService) PerformSync(ctx context.Context, environmentID, id string, actor common.User) (*gitops.SyncResult, error) {
	// Coalesce overlapping runs for the same sync (scheduled fire, startup/enable
	// kick, manual trigger, webhook) so they don't race the clone/redeploy.
	lease, admitted, err := s.jobs.TryAcquire(ctx, id)
	if err != nil {
		return nil, errors.WrapIf(err, "failed to admit GitOps sync")
	}
	if !admitted {
		slog.InfoContext(ctx, "GitOps sync already in progress; skipping", "syncId", id)
		return &gitops.SyncResult{Success: false, Message: "sync already in progress", SyncedAt: time.Now()}, nil
	}
	defer lease.Release()

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

	source, err := s.prepareSyncSource(syncCtx, syncRecord, result, actor)
	if source != nil && source.repoPath != "" {
		defer func() {
			if cleanupErr := s.repoService.Cleanup(source.repoPath); cleanupErr != nil {
				slog.WarnContext(syncCtx, "Failed to cleanup repository", "path", source.repoPath, "error", cleanupErr)
			}
		}()
	}
	if err != nil {
		return result, err
	}
	if source == nil {
		return result, errors.New("prepared sync source is missing")
	}

	if syncRecord.TargetType == "swarm_stack" {
		return s.performSwarmStackSyncInternal(syncCtx, syncRecord, id, actor, result, source)
	}

	if syncRecord.SyncDirectory {
		return s.performDirectorySync(syncCtx, syncRecord, id, actor, result, source)
	}

	return s.performSingleFileSyncInternal(syncCtx, syncRecord, id, actor, result, source)
}

// prepareSyncSource clones the source repository, validates that the configured
// compose file exists, and reads the compose/env inputs for the sync flow.
func (s *GitOpsSyncService) prepareSyncSource(ctx context.Context, sync *projectpkg.GitOpsSync, result *gitops.SyncResult, actor common.User) (*preparedSyncSource, error) {
	repository := sync.Repository
	if repository == nil {
		return nil, s.failSync(ctx, sync.ID, result, sync, actor, "Repository not found", "repository not found")
	}

	authConfig, err := s.repoService.GetAuthConfig(ctx, repository)
	if err != nil {
		return nil, s.failSync(ctx, sync.ID, result, sync, actor, "Failed to get authentication config", err.Error())
	}

	repoPath, err := s.repoService.Clone(ctx, repository.URL, sync.Branch, authConfig)
	if err != nil {
		return nil, s.failSync(ctx, sync.ID, result, sync, actor, "Failed to clone repository", err.Error())
	}

	commitHash, err := s.repoService.GetCurrentCommit(ctx, repoPath)
	if err != nil {
		slog.WarnContext(ctx, "Failed to get commit hash", "error", err)
		commitHash = ""
	}

	if !s.repoService.FileExists(ctx, repoPath, sync.ComposePath) {
		errMsg := "compose file not found: " + sync.ComposePath
		return &preparedSyncSource{repoPath: repoPath, commitHash: commitHash}, s.failSync(ctx, sync.ID, result, sync, actor, "Compose file not found at "+sync.ComposePath, errMsg)
	}

	composeContent, err := s.repoService.ReadFile(ctx, repoPath, sync.ComposePath)
	if err != nil {
		return &preparedSyncSource{repoPath: repoPath, commitHash: commitHash}, s.failSync(ctx, sync.ID, result, sync, actor, "Failed to read compose file", err.Error())
	}

	source := &preparedSyncSource{
		repoPath:       repoPath,
		commitHash:     commitHash,
		composeContent: composeContent,
	}

	envPath := filepath.Join(filepath.Dir(sync.ComposePath), ".env")
	if s.repoService.FileExists(ctx, repoPath, envPath) {
		content, err := s.repoService.ReadFile(ctx, repoPath, envPath)
		if err != nil {
			slog.WarnContext(ctx, "Failed to read .env file", "path", envPath, "error", err)
		} else {
			source.envContent = &content
		}
	}

	// Detect a Docker Compose override file (compose.override.yaml, etc.) sitting
	// beside the compose file in the repo, mirroring `docker compose` behavior.
	// Only auto-load an override when the compose file was referenced by a
	// standard filename; an explicit custom path is the `-f` case, where docker
	// compose does not auto-load overrides.
	if slices.Contains(projects.ComposeFileCandidates(), filepath.Base(sync.ComposePath)) {
		composeDir := filepath.Dir(sync.ComposePath)
		overrideName, overrideContent, overrideFound, overrideErr := projects.ResolveComposeOverride(
			func(name string) bool {
				return s.repoService.FileExists(ctx, repoPath, filepath.Join(composeDir, name))
			},
			func(name string) (string, error) {
				return s.repoService.ReadFile(ctx, repoPath, filepath.Join(composeDir, name))
			},
		)
		// Fail the sync on a read error instead of degrading: WriteComposeOverrideFile
		// with nil content removes the project's existing override, so a transient
		// read failure here would delete the override and redeploy without it.
		if overrideErr != nil {
			return source, s.failSync(ctx, sync.ID, result, sync, actor, "Failed to read compose override file", overrideErr.Error())
		}
		if overrideFound {
			source.overrideContent = &overrideContent
			source.overrideFileName = overrideName
		}
	}

	return source, nil
}

// performDirectorySync runs the directory-sync path and only triggers a
// redeploy when an already running project's synced contents changed.
func (s *GitOpsSyncService) performDirectorySync(ctx context.Context, sync *projectpkg.GitOpsSync, id string, actor common.User, result *gitops.SyncResult, source *preparedSyncSource) (*gitops.SyncResult, error) {
	slog.InfoContext(ctx, "Using directory sync mode", "syncId", id, "composePath", sync.ComposePath)

	syncFiles, err := s.walkAndParseSyncDirectory(ctx, sync, source.repoPath)
	if err != nil {
		return result, s.failSync(ctx, id, result, sync, actor, "Failed to walk directory", err.Error())
	}

	project, syncedFiles, _, contentsChanged, err := s.syncProjectDirectoryInternal(ctx, sync, syncFiles, actor)
	if err != nil {
		if errors.Is(err, common.ErrGitOpsSyncProjectBindingBroken) {
			errMsg := err.Error()
			result.Message = "GitOps project binding broken"
			result.Error = new(errMsg)
			return result, err
		}
		return result, s.failSync(ctx, id, result, sync, actor, "Failed to sync project directory", err.Error())
	}

	if contentsChanged {
		if redeployErr := s.redeployIfRunningAfterSync(ctx, sync, project, actor, "directory"); redeployErr != nil {
			if errors.Is(redeployErr, common.ErrRedeployAfterSyncFailed) {
				s.markSyncRedeployFailedInternal(ctx, sync, id, source.commitHash, syncedFiles, redeployErr, actor, result)
			}
			return result, redeployErr
		}
	}

	s.updateSyncStatusWithFiles(ctx, id, "success", "", source.commitHash, syncedFiles)
	result.Success = true
	result.Message = fmt.Sprintf("Successfully synced directory with %d files to project %s", len(syncedFiles), project.Name)
	s.logSyncSuccess(ctx, sync, project, actor)
	slog.InfoContext(ctx, "GitOps sync completed", "syncId", id, "project", project.Name)

	return result, nil
}

// singleFileSyncedFilesInternal builds the tracked-file list for a single-file
// sync: the compose file's base name plus the override file name when one was
// resolved. Shared by the compose-only and swarm-stack single-file paths so the
// two never disagree on what a single-file sync tracked.
func singleFileSyncedFilesInternal(sync *projectpkg.GitOpsSync, source *preparedSyncSource) []string {
	syncedFiles := []string{filepath.Base(sync.ComposePath)}
	if source.overrideFileName != "" {
		syncedFiles = append(syncedFiles, source.overrideFileName)
	}
	return syncedFiles
}

// performSingleFileSyncInternal preserves the legacy compose-only Git sync behavior.
func (s *GitOpsSyncService) performSingleFileSyncInternal(ctx context.Context, sync *projectpkg.GitOpsSync, id string, actor common.User, result *gitops.SyncResult, source *preparedSyncSource) (*gitops.SyncResult, error) {
	slog.InfoContext(ctx, "Using single file sync mode", "syncId", id, "composePath", sync.ComposePath)

	// Single-file sync copies only the one compose file (plus .env and a sibling
	// override). If the synced .env references additional files via COMPOSE_FILE
	// or COMPOSE_ENV_FILES, those files never reach the project, so direct the
	// user to directory sync instead of silently deploying an incomplete
	// selection.
	if composeFileEnvNeedsDirectorySyncInternal(sync, source) {
		return result, s.failSync(ctx, id, result, sync, actor,
			"COMPOSE_FILE or COMPOSE_ENV_FILES references additional files",
			"the synced .env references additional files via COMPOSE_FILE or COMPOSE_ENV_FILES; enable \"Sync entire directory\" for this sync")
	}

	syncedFiles := singleFileSyncedFilesInternal(sync, source)

	project, err := s.getOrCreateProjectInternal(ctx, sync, id, source.composeContent, source.envContent, source.overrideContent, source.overrideFileName, result, actor)
	if err != nil {
		if errors.Is(err, common.ErrRedeployAfterSyncFailed) {
			s.markSyncRedeployFailedInternal(ctx, sync, id, source.commitHash, syncedFiles, err, actor, result)
		}
		return result, err
	}

	s.updateSyncStatusWithFiles(ctx, id, "success", "", source.commitHash, syncedFiles)
	result.Success = true
	result.Message = fmt.Sprintf("Successfully synced compose file from %s to project %s", sync.ComposePath, project.Name)
	s.logSyncSuccess(ctx, sync, project, actor)
	slog.InfoContext(ctx, "GitOps sync completed", "syncId", id, "project", project.Name)

	return result, nil
}

// composeFileEnvNeedsDirectorySyncInternal reports whether the synced .env
// declares a COMPOSE_FILE or COMPOSE_ENV_FILES selection that single-file sync
// cannot satisfy — i.e. it references any file other than the ones single-file
// sync actually ships (the synced compose file, its sibling override, and .env
// itself), or any file in a subdirectory.
func composeFileEnvNeedsDirectorySyncInternal(sync *projectpkg.GitOpsSync, source *preparedSyncSource) bool {
	if source == nil || source.envContent == nil {
		return false
	}
	envMap, err := projects.ParseProjectEnvContent(*source.envContent, nil)
	if err != nil || len(envMap) == 0 {
		return false
	}

	allowedComposeFiles := map[string]struct{}{filepath.Base(sync.ComposePath): {}}
	if source.overrideFileName != "" {
		allowedComposeFiles[source.overrideFileName] = struct{}{}
	}
	if entriesNeedDirectorySyncInternal(projects.ComposeFileEntriesFromEnv(envMap), allowedComposeFiles) {
		return true
	}

	// Single-file sync writes the synced env content to .env, so a
	// self-reference is satisfiable; anything else never reaches the project.
	return entriesNeedDirectorySyncInternal(projects.ComposeEnvFileEntriesFromEnv(envMap), map[string]struct{}{projects.EffectiveEnvFileName: {}})
}

func entriesNeedDirectorySyncInternal(entries []string, allowed map[string]struct{}) bool {
	for _, entry := range entries {
		// Normalize so relative prefixes like "./compose.yaml" match allowed.
		cleaned := filepath.Clean(entry)
		if cleaned != filepath.Base(cleaned) {
			return true // nested path can't be single-file synced
		}
		if _, ok := allowed[cleaned]; !ok {
			return true
		}
	}
	return false
}

// buildSwarmStackDeployRequestInternal assembles the deploy request for a Git Sync that targets
// a Swarm stack. WithRegistryAuth is always set so the Git Sync path resolves stored registry
// credentials the same way a direct stack deploy does. Resolution happens per image at deploy
// time and yields nothing unless a configured container registry matches that image's host, so
// stacks built only from public images are unaffected.
func buildSwarmStackDeployRequestInternal(sync *projectpkg.GitOpsSync, source *preparedSyncSource, overrideContent string, envContent string, swarmFiles []swarmtypes.SyncFile) swarmtypes.StackDeployRequest {
	return swarmtypes.StackDeployRequest{
		Name:             sync.ProjectName,
		ComposeContent:   source.composeContent,
		OverrideContent:  overrideContent,
		EnvContent:       envContent,
		Files:            swarmFiles,
		Prune:            true,
		WithRegistryAuth: true,
		WorkingDir:       filepath.Dir(filepath.Join(source.repoPath, sync.ComposePath)),
	}
}

// performSwarmStackSyncInternal executes a single file sync targeted at a Swarm Stack
func (s *GitOpsSyncService) performSwarmStackSyncInternal(ctx context.Context, sync *projectpkg.GitOpsSync, id string, actor common.User, result *gitops.SyncResult, source *preparedSyncSource) (*gitops.SyncResult, error) {
	slog.InfoContext(ctx, "Deploying Swarm Stack from GitOps sync", "syncId", id, "stackName", sync.ProjectName)

	if s.swarmService == nil {
		return result, s.failSync(ctx, id, result, sync, actor, "Swarm service is unavailable", "swarm service is unavailable")
	}

	envContent := ""
	if source.envContent != nil {
		envContent = *source.envContent
	}

	overrideContent := ""
	if source.overrideContent != nil {
		overrideContent = *source.overrideContent
	}

	var syncFiles []projects.SyncFile
	if sync.SyncDirectory {
		files, err := s.walkAndParseSyncDirectory(ctx, sync, source.repoPath)
		if err != nil {
			return result, s.failSync(ctx, id, result, sync, actor, "Failed to walk directory", err.Error())
		}
		syncFiles = files
	}

	swarmFiles := make([]swarmtypes.SyncFile, len(syncFiles))
	syncedFiles := make([]string, 0, len(syncFiles))
	for i, f := range syncFiles {
		swarmFiles[i] = swarmtypes.SyncFile{
			RelativePath: f.RelativePath,
			Content:      f.Content,
		}
		syncedFiles = append(syncedFiles, f.RelativePath)
	}

	req := buildSwarmStackDeployRequestInternal(sync, source, overrideContent, envContent, swarmFiles)

	if _, err := s.swarmService.DeployStack(ctx, sync.EnvironmentID, req); err != nil {
		return result, s.failSync(ctx, id, result, sync, actor, "Failed to deploy swarm stack", err.Error())
	}

	if len(syncedFiles) == 0 {
		syncedFiles = singleFileSyncedFilesInternal(sync, source)
	}
	s.updateSyncStatusWithFiles(ctx, id, "success", "", source.commitHash, syncedFiles)
	result.Success = true
	result.Message = fmt.Sprintf("Successfully deployed swarm stack %s from %s", sync.ProjectName, sync.ComposePath)

	// Log event
	_, _ = s.eventService.CreateEvent(ctx, event.CreateEventRequest{
		Type:          event.EventTypeGitSyncRun,
		Severity:      event.EventSeveritySuccess,
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

// redeployIfRunningAfterSync redeploys a project when it is already running,
// or when the sync has RedeployAfterSync enabled (in which case a stopped
// project is redeployed too, mirroring Portainer's "Re-pull image" stack
// option). Returns a common.ErrRedeployAfterSyncFailed when the redeploy
// itself fails; callers surface that on the sync row's LastSyncError.
//
// When neither condition holds — project stopped and RedeployAfterSync off —
// RedeployProject is never called (Arcane does not start a stopped project on
// its behalf by default), which also means the image pull that RedeployProject
// performs as a side effect never happens. Left alone, that can leave a
// stopped container referencing an image tag that was since re-pointed or
// pruned upstream, with no local copy to fall back on at next start. If the
// sync opted into PullImageAfterSync, pull the image here instead so it stays
// available even while the project is stopped.
func (s *GitOpsSyncService) redeployIfRunningAfterSync(ctx context.Context, sync *projectpkg.GitOpsSync, project *projectpkg.Project, actor common.User, syncMode string) error {
	details, err := s.projectService.GetProjectDetails(ctx, project.ID, projecttypes.DetailsOptions{})
	running := err == nil && (details.Status == string(projectpkg.ProjectStatusRunning) || details.Status == string(projectpkg.ProjectStatusPartiallyRunning))

	if !running && !sync.RedeployAfterSync {
		return s.pullImageAfterSyncIfConfiguredInternal(ctx, sync, project, actor)
	}

	slog.InfoContext(ctx, "Redeploying project due to content change from Git sync", "syncMode", syncMode, "projectName", project.Name, "projectId", project.ID, "wasRunning", running)
	if err := s.projectService.RedeployProject(ctx, project.ID, actor, nil); err != nil {
		slog.ErrorContext(ctx, "Failed to redeploy project after Git sync", "syncMode", syncMode, "error", err, "projectId", project.ID)
		return common.Classify(common.ErrRedeployAfterSyncFailed, errors.WrapIf(err, "redeploy failed"))
	}
	return nil
}

// pullImageAfterSyncIfConfiguredInternal pulls each service's image for a
// project that Git sync changed but did not redeploy (because it wasn't
// running), when the sync has PullImageAfterSync enabled. Best-effort: a pull
// failure is logged but never fails the sync itself, mirroring how
// RedeployProject treats its own pre-deploy pull.
func (s *GitOpsSyncService) pullImageAfterSyncIfConfiguredInternal(ctx context.Context, sync *projectpkg.GitOpsSync, project *projectpkg.Project, actor common.User) error {
	if !sync.PullImageAfterSync {
		return nil
	}

	credentials, cerr := s.projectService.ResolveRegistryCredentials(ctx)
	if cerr != nil {
		slog.WarnContext(ctx, "failed to resolve registry credentials for post-sync pull", "error", cerr, "projectId", project.ID)
	}
	slog.InfoContext(ctx, "Pulling project images after Git sync (project not running)", "projectName", project.Name, "projectId", project.ID)
	if err := s.projectService.PullProjectImages(ctx, project.ID, io.Discard, actor, credentials); err != nil {
		slog.ErrorContext(ctx, "failed to pull project images after Git sync", "error", err, "projectId", project.ID)
		return common.Classify(common.ErrRedeployAfterSyncFailed, errors.WrapIf(err, "post-sync image pull failed"))
	}
	return nil
}

// logSyncSuccess records the Git sync completion event once the filesystem and
// sync-status updates have already succeeded.
func (s *GitOpsSyncService) logSyncSuccess(ctx context.Context, sync *projectpkg.GitOpsSync, project *projectpkg.Project, actor common.User) {
	_, _ = s.eventService.CreateEvent(ctx, event.CreateEventRequest{
		Type:          event.EventTypeGitSyncRun,
		Severity:      event.EventSeveritySuccess,
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

	if err := s.db.WithContext(ctx).Model(&projectpkg.GitOpsSync{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		slog.ErrorContext(ctx, "Failed to update sync status", "error", err, "syncId", id)
	}
}

func (s *GitOpsSyncService) GetSyncStatus(ctx context.Context, environmentID, id string) (*gitops.SyncStatus, error) {
	syncRecord, err := s.GetSyncByID(ctx, environmentID, id)
	if err != nil {
		return nil, err
	}

	status := &gitops.SyncStatus{
		ID:             syncRecord.ID,
		AutoSync:       syncRecord.AutoSync,
		LastSyncAt:     syncRecord.LastSyncAt,
		LastSyncStatus: syncRecord.LastSyncStatus,
		LastSyncError:  syncRecord.LastSyncError,
		LastSyncCommit: syncRecord.LastSyncCommit,
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
		return errors.WrapIf(err, "failed to resolve projects directory for gitops scratch cleanup")
	}

	entries, err := acfs.List(ctx, projectsDir, "/")
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return errors.WrapIff(err, "failed to list projects directory %s for gitops scratch cleanup", projectsDir)
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
		return errors.WrapIf(err, "failed to purge leaked git clone scratch directories")
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
		return errors.WrapIf(err, "failed to list orphaned gitops syncs")
	}
	if len(syncIDs) == 0 {
		return nil
	}

	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&projectpkg.Project{}).
			Where("gitops_managed_by IN ?", syncIDs).
			Update("gitops_managed_by", nil).Error; err != nil {
			return errors.WrapIf(err, "failed to clear orphaned gitops project references")
		}
		if err := tx.Where("id IN ?", syncIDs).Delete(&projectpkg.GitOpsSync{}).Error; err != nil {
			return errors.WrapIf(err, "failed to delete orphaned gitops syncs")
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
		return errors.WrapIf(err, "failed to list directory syncs for startup reconciliation")
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
	repoPath, err := s.repoService.Clone(browseCtx, repository.URL, syncRecord.Branch, authConfig)
	if err != nil {
		return nil, errors.WrapIf(err, "failed to clone repository")
	}
	defer func() {
		if cleanupErr := s.repoService.Cleanup(repoPath); cleanupErr != nil {
			slog.WarnContext(browseCtx, "Failed to cleanup repository", "path", repoPath, "error", cleanupErr)
		}
	}()

	// Browse the tree
	files, err := s.repoService.BrowseTree(browseCtx, repoPath, path)
	if err != nil {
		return nil, err
	}

	return &gitops.BrowseResponse{
		Path:  path,
		Files: files,
	}, nil
}

func (s *GitOpsSyncService) ImportSyncs(ctx context.Context, environmentID string, req []gitops.ImportGitOpsSyncRequest, actor common.User) (*gitops.ImportGitOpsSyncResponse, error) {
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

func (s *GitOpsSyncService) logSyncError(ctx context.Context, sync *projectpkg.GitOpsSync, actor common.User, errorMsg string) {
	_, _ = s.eventService.CreateEvent(ctx, event.CreateEventRequest{
		Type:          event.EventTypeGitSyncError,
		Severity:      event.EventSeverityError,
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

func (s *GitOpsSyncService) failSync(ctx context.Context, id string, result *gitops.SyncResult, sync *projectpkg.GitOpsSync, actor common.User, message, errMsg string) error {
	result.Message = message
	result.Error = new(errMsg)
	s.updateSyncStatus(ctx, id, "failed", errMsg, "")
	s.logSyncError(ctx, sync, actor, errMsg)
	return errors.Errorf("%s", errMsg)
}

// markSyncRedeployFailedInternal records a sync where the file sync wrote
// cleanly but the auto-redeploy that follows failed (typically a pre-deploy
// lifecycle hook returning non-zero). The synced-files list and commit hash
// are preserved on the row so operators can see what reached disk; the
// error message surfaces the redeploy failure on LastSyncError.
func (s *GitOpsSyncService) markSyncRedeployFailedInternal(ctx context.Context, sync *projectpkg.GitOpsSync, id, commitHash string, syncedFiles []string, redeployErr error, actor common.User, result *gitops.SyncResult) {
	errMsg := redeployErr.Error()
	result.Success = false
	result.Message = "Sync wrote files but redeploy failed"
	result.Error = new(errMsg)
	s.updateSyncStatusWithFiles(ctx, id, "failed", errMsg, commitHash, syncedFiles)
	s.logSyncError(ctx, sync, actor, errMsg)
}

func (s *GitOpsSyncService) disableAutoSyncForBrokenBindingInternal(ctx context.Context, sync *projectpkg.GitOpsSync) {
	if sync == nil || sync.ID == "" {
		return
	}
	if err := s.db.WithContext(ctx).
		Model(&projectpkg.GitOpsSync{}).
		Where("id = ?", sync.ID).
		Update("auto_sync", false).Error; err != nil {
		slog.ErrorContext(ctx, "Failed to disable GitOps auto-sync after broken project binding", "syncId", sync.ID, "error", err)
	}
	sync.AutoSync = false
	s.unregisterSyncJobInternal(ctx, sync.ID)
}

func (s *GitOpsSyncService) failSyncAndDisableAutoSyncInternal(ctx context.Context, id string, result *gitops.SyncResult, sync *projectpkg.GitOpsSync, actor common.User, message string, failure error) error {
	errMsg := failure.Error()
	_ = s.failSync(ctx, id, result, sync, actor, message, errMsg)
	s.disableAutoSyncForBrokenBindingInternal(ctx, sync)
	return failure
}

func (s *GitOpsSyncService) recordBrokenProjectBindingInternal(ctx context.Context, sync *projectpkg.GitOpsSync, actor common.User, err error) {
	if !errors.Is(err, common.ErrGitOpsSyncProjectBindingBroken) || sync == nil {
		return
	}
	errMsg := err.Error()
	s.updateSyncStatus(ctx, sync.ID, "failed", errMsg, "")
	s.logSyncError(ctx, sync, actor, errMsg)
	s.disableAutoSyncForBrokenBindingInternal(ctx, sync)
}

func (s *GitOpsSyncService) createProjectForSyncInternal(ctx context.Context, sync *projectpkg.GitOpsSync, id string, composeContent string, envContent *string, overrideContent *string, overrideFileName string, result *gitops.SyncResult, actor common.User) (*projectpkg.Project, error) {
	// Use the non-suffixing create: a GitOps sync must never mint a "-N" duplicate.
	// A name collision means a project directory already exists for this name, so the
	// binding is broken — fail loudly and disable auto-sync instead of duplicating.
	project, err := s.projectService.CreateProject(ctx, sync.ProjectName, composeContent, envContent, projecttypes.CreateProjectWorkspaceManifest{}, nil, nil, nil, actor, false)
	if err != nil {
		if errors.Is(err, projects.ErrProjectDirExists) {
			bindingErr := common.Classify(common.ErrGitOpsSyncProjectBindingBroken, errors.WrapIf(errors.Errorf("sync %s cannot create project %q: a directory with that name already exists; refusing to create a duplicate", sync.ID, projects.SanitizeProjectName(sync.ProjectName)), "GitOps sync project binding broken"))

			return nil, s.failSyncAndDisableAutoSyncInternal(ctx, id, result, sync, actor, "GitOps project binding broken", bindingErr)
		}
		return nil, s.failSync(ctx, id, result, sync, actor, "Failed to create project", err.Error())
	}

	// Update sync with project ID
	if err := s.db.WithContext(ctx).Model(&projectpkg.GitOpsSync{}).Where("id = ?", id).Updates(map[string]any{
		"project_id": project.ID,
	}).Error; err != nil {
		return nil, s.failSync(ctx, id, result, sync, actor, "Failed to update sync with project ID", err.Error())
	}

	// Mark project as GitOps-managed
	if err := s.db.WithContext(ctx).Model(&projectpkg.Project{}).Where("id = ?", project.ID).Update("gitops_managed_by", id).Error; err != nil {
		return nil, s.failSync(ctx, id, result, sync, actor, "Failed to mark project as GitOps-managed", err.Error())
	}

	if _, err := s.projectService.ApplyGitSyncProjectFiles(ctx, project.ID, composeContent, envContent, overrideContent, overrideFileName, actor); err != nil {
		return nil, s.failSync(ctx, id, result, sync, actor, "Failed to sync project env files", err.Error())
	}

	slog.InfoContext(ctx, "Created project for GitOps sync", "projectName", sync.ProjectName, "projectId", project.ID)

	// A freshly created project has no containers yet, so it's always
	// "stopped" from RedeployProject's point of view — mirror
	// updateProjectForSyncInternal's post-sync behavior here instead of
	// silently leaving the new project undeployed with no image pulled.
	if sync.RedeployAfterSync {
		slog.InfoContext(ctx, "Redeploying newly created project from Git sync", "projectName", project.Name, "projectId", project.ID)
		if err := s.projectService.RedeployProject(ctx, project.ID, actor, nil); err != nil {
			slog.ErrorContext(ctx, "Failed to redeploy newly created project after Git sync", "error", err, "projectId", project.ID)
			return nil, common.Classify(common.ErrRedeployAfterSyncFailed, errors.WrapIf(err, "redeploy failed"))
		}
	} else if err := s.pullImageAfterSyncIfConfiguredInternal(ctx, sync, project, actor); err != nil {
		return nil, err
	}

	return project, nil
}

func (s *GitOpsSyncService) getOrCreateProjectInternal(ctx context.Context, sync *projectpkg.GitOpsSync, id string, composeContent string, envContent *string, overrideContent *string, overrideFileName string, result *gitops.SyncResult, actor common.User) (*projectpkg.Project, error) {
	var project *projectpkg.Project

	if sync.ProjectID != nil && *sync.ProjectID != "" {
		var found bool
		var lookupErr error
		project, found, lookupErr = s.lookupProjectByIDInternal(ctx, *sync.ProjectID)
		if lookupErr != nil {
			return nil, s.failSync(ctx, id, result, sync, actor, "Failed to load existing project", lookupErr.Error())
		}
		if !found {
			err := common.Classify(common.ErrGitOpsSyncProjectBindingBroken, errors.WrapIf(errors.Errorf("sync %s references missing project %s", sync.ID, *sync.ProjectID), "GitOps sync project binding broken"))

			slog.WarnContext(ctx, "Existing project not found; GitOps project binding is broken", "projectId", *sync.ProjectID, "syncId", sync.ID)
			return nil, s.failSyncAndDisableAutoSyncInternal(ctx, id, result, sync, actor, "GitOps project binding broken", err)
		}
	}

	if project == nil {
		return s.createProjectForSyncInternal(ctx, sync, id, composeContent, envContent, overrideContent, overrideFileName, result, actor)
	}

	if err := s.updateProjectForSyncInternal(ctx, sync, id, project, composeContent, envContent, overrideContent, overrideFileName, result, actor); err != nil {
		return nil, err
	}
	return project, nil
}

func (s *GitOpsSyncService) updateProjectForSyncInternal(ctx context.Context, sync *projectpkg.GitOpsSync, id string, project *projectpkg.Project, composeContent string, envContent *string, overrideContent *string, overrideFileName string, result *gitops.SyncResult, actor common.User) error {
	// Get current content to see if it changed
	oldCompose, oldEnv, oldOverride, _ := s.projectService.GetProjectContent(ctx, project.ID)

	// Update existing project's compose and env files
	_, err := s.projectService.ApplyGitSyncProjectFiles(ctx, project.ID, composeContent, envContent, overrideContent, overrideFileName, actor)
	if err != nil {
		return s.failSync(ctx, id, result, sync, actor, "Failed to update project files", err.Error())
	}
	slog.InfoContext(ctx, "Updated project files", "projectName", project.Name, "projectId", project.ID)

	newCompose, newEnv, newOverride, _ := s.projectService.GetProjectContent(ctx, project.ID)
	contentChanged := oldCompose != newCompose || envContentChangedInternal(oldEnv, newEnv) || oldOverride != newOverride

	// If content changed and project is running, redeploy. A redeploy failure
	// is classified as common.ErrRedeployAfterSyncFailed so the parent flow
	// can reflect it on the sync row's LastSyncError.
	if contentChanged {
		details, err := s.projectService.GetProjectDetails(ctx, project.ID, projecttypes.DetailsOptions{})
		running := err == nil && (details.Status == string(projectpkg.ProjectStatusRunning) || details.Status == string(projectpkg.ProjectStatusPartiallyRunning))
		if running || sync.RedeployAfterSync {
			slog.InfoContext(ctx, "Redeploying project due to content change from Git sync", "projectName", project.Name, "projectId", project.ID, "wasRunning", running)
			if err := s.projectService.RedeployProject(ctx, project.ID, actor, nil); err != nil {
				slog.ErrorContext(ctx, "Failed to redeploy project after Git sync", "error", err, "projectId", project.ID)
				return common.Classify(common.ErrRedeployAfterSyncFailed, errors.WrapIf(err, "redeploy failed"))
			}
		} else {
			// Project isn't running and RedeployAfterSync is off, so
			// RedeployProject (and its pull side effect) never runs. Pull
			// explicitly if this sync opted into PullImageAfterSync, so a
			// stopped container isn't left referencing an image tag that's
			// since been re-pointed or pruned upstream. A pull failure is
			// returned rather than swallowed, so it surfaces on the sync
			// row's LastSyncError instead of the sync silently reporting
			// success with a stale image.
			if err := s.pullImageAfterSyncIfConfiguredInternal(ctx, sync, project, actor); err != nil {
				return err
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
func (s *GitOpsSyncService) walkAndParseSyncDirectory(ctx context.Context, sync *projectpkg.GitOpsSync, repoPath string) ([]projects.SyncFile, error) {
	slog.InfoContext(ctx, "Starting directory walk", "syncId", sync.ID, "composePath", sync.ComposePath)

	// Walk the directory to get all files
	maxFiles, maxTotalSize, maxBinarySize := s.getEffectiveSyncLimits(ctx, sync)

	walkResult, err := s.repoService.WalkDirectory(ctx, repoPath, sync.ComposePath, maxFiles, maxTotalSize, maxBinarySize)
	if err != nil {
		return nil, errors.WrapIf(err, "failed to walk directory")
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
			Executable:   f.Executable,
		}
		if f.RelativePath == composeFileName {
			composeFound = true
		}
	}

	if !composeFound {
		return nil, errors.Errorf("compose file %s not found in walked directory", composeFileName)
	}

	return syncFiles, nil
}

// syncProjectDirectoryInternal runs the new directory-sync path end to end:
// stage files, validate the staged tree, then create or update the project.
func (s *GitOpsSyncService) syncProjectDirectoryInternal(ctx context.Context, sync *projectpkg.GitOpsSync, syncFiles []projects.SyncFile, actor common.User) (*projectpkg.Project, []string, bool, bool, error) {
	stage, err := s.stageDirectorySyncInternal(ctx, sync, syncFiles)
	if err != nil {
		s.recordBrokenProjectBindingInternal(ctx, sync, actor, err)
		return nil, nil, false, false, err
	}
	defer func() {
		if stage != nil && stage.stagePath != "" {
			_ = acfs.RemoveAll(ctx, stage.projectsDir, stage.stageLogical)
		}
	}()

	if stage.project == nil {
		project, err := s.createDirectorySyncProjectInternal(ctx, sync, stage, actor)
		if err != nil {
			// A name-collision on create surfaces as a broken-binding error; disable
			// auto-sync so it cannot keep retrying (no-op for other error kinds).
			s.recordBrokenProjectBindingInternal(ctx, sync, actor, err)
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
func (s *GitOpsSyncService) stageDirectorySyncInternal(ctx context.Context, sync *projectpkg.GitOpsSync, syncFiles []projects.SyncFile) (*stagedDirectorySync, error) {
	projectsDir, err := s.projectService.GetProjectsDirectory(ctx)
	if err != nil {
		return nil, errors.WrapIf(err, "failed to get projects directory")
	}

	// The project-root env files are reserved for the three-file override
	// merge applied below by the Project domain rather than a raw
	// overwrite — a raw .env write would silently wipe edits made in Arcane
	// on every sync.
	filteredSyncFiles, gitEnvContent := partitionReservedRootEnvFilesInternal(ctx, syncFiles)

	stageLogical, err := acfs.MkdirTemp(ctx, projectsDir, "/", ".gitops-sync-stage-*")
	if err != nil {
		return nil, errors.WrapIf(err, "failed to create staging directory")
	}
	stagePath := filepath.Join(projectsDir, filepath.FromSlash(strings.TrimPrefix(stageLogical, "/")))

	project, err := s.getDirectorySyncProjectInternal(ctx, sync)
	if err != nil {
		_ = acfs.RemoveAll(ctx, projectsDir, stageLogical)
		return nil, err
	}

	var copySkipped []string
	if project != nil {
		// Tolerate files Arcane cannot read (e.g. foreign-owned files a container
		// wrote into the project directory through a relative bind mount). Skipping
		// them keeps an unrelated unreadable file from aborting the whole sync; the
		// skipped paths are preserved (not pruned) when the staged tree is mirrored
		// back over the live project during promotion.
		staged, stageErr := acfs.CopyDir(ctx, project.Path, stagePath, acfstypes.CopyOptions{TolerateUnreadable: true})
		copySkipped = staged.Skipped
		if err = stageErr; err != nil {
			_ = acfs.RemoveAll(ctx, projectsDir, stageLogical)
			return nil, errors.WrapIf(err, "failed to stage current project files")
		}
		if len(copySkipped) > 0 {
			slog.WarnContext(ctx, "skipped unreadable files while staging project sync; they will be left untouched on promotion", "projectPath", project.Path, "skipped", copySkipped)
		}
	} else if err := s.seedStageEnvFromCandidateDirInternal(ctx, sync, projectsDir, stagePath); err != nil {
		_ = acfs.RemoveAll(ctx, projectsDir, stageLogical)
		return nil, err
	}

	syncedFiles := make([]string, len(filteredSyncFiles))
	for i, file := range filteredSyncFiles {
		syncedFiles[i] = file.RelativePath
	}

	// Syncs created before this fix may still have .env recorded as a tracked
	// file. Drop reserved root env files here too, or CleanupRemovedFiles would
	// treat .env as removed-by-git and delete the live-copied stage .env before
	// ApplyGitSyncEnvToDirectory gets a chance to read it for the merge.
	oldSyncedFiles := filterReservedRootEnvFilesInternal(parseSyncedFiles(sync.SyncedFiles))
	if len(oldSyncedFiles) > 0 {
		if err := projects.CleanupRemovedFiles(ctx, projectsDir, stagePath, oldSyncedFiles, syncedFiles); err != nil {
			_ = acfs.RemoveAll(ctx, projectsDir, stageLogical)
			return nil, errors.WrapIf(err, "failed to clean removed synced files")
		}
	}

	composeFileName := filepath.Base(sync.ComposePath)
	if err := projects.RemoveStaleComposeFiles(ctx, stagePath, composeFileName, syncedFiles); err != nil {
		_ = acfs.RemoveAll(ctx, projectsDir, stageLogical)
		return nil, errors.WrapIf(err, "failed to remove stale compose files")
	}

	contentsChanged := true
	if project != nil {
		contentsChanged, err = projects.DirectorySyncContentsChanged(ctx, project.Path, filteredSyncFiles, oldSyncedFiles, composeFileName)
		if err != nil {
			_ = acfs.RemoveAll(ctx, projectsDir, stageLogical)
			return nil, errors.WrapIf(err, "failed to compare staged directory changes")
		}
	}

	// Write the repo files (excluding reserved root env files, handled below)
	// after cleanup so validation sees the final on-disk tree exactly as it
	// will exist in the managed project.
	if _, err := projects.WriteSyncedDirectory(ctx, projectsDir, stagePath, filteredSyncFiles); err != nil {
		_ = acfs.RemoveAll(ctx, projectsDir, stageLogical)
		return nil, errors.WrapIf(err, "failed to write staged sync files")
	}

	// Route the project-root .env through the same three-file override merge
	// single-file git sync uses: git is source-of-truth, edits made in Arcane
	// become an override that wins, and new git-introduced keys still flow in.
	preEnvContent, postEnvContent, err := s.projectService.ApplyGitSyncEnvToDirectory(ctx, stagePath, projectsDir, gitEnvContent)
	if err != nil {
		_ = acfs.RemoveAll(ctx, projectsDir, stageLogical)
		return nil, err
	}
	if project != nil && envContentChangedInternal(preEnvContent, postEnvContent) {
		contentsChanged = true
	}

	serviceCount, err := s.projectService.ValidateComposeDirectory(ctx, sync.ProjectName, stagePath, composeFileName)
	if err != nil {
		_ = acfs.RemoveAll(ctx, projectsDir, stageLogical)
		return nil, errors.WrapIf(err, "invalid compose file")
	}

	return &stagedDirectorySync{
		stagePath:       stagePath,
		stageLogical:    stageLogical,
		projectsDir:     projectsDir,
		composeFileName: composeFileName,
		project:         project,
		syncedFiles:     syncedFiles,
		serviceCount:    serviceCount,
		contentsChanged: contentsChanged,
		copySkipped:     copySkipped,
	}, nil
}

// isReservedRootEnvFileInternal reports whether relPath is one of the
// project-root env bookkeeping files the override-merge system owns (.env,
// .env.git, project.env, .env.global). These are excluded from the raw
// directory-sync write and routed through ProjectService.ApplyGitSyncEnvToDirectory
// instead. Exact-matching RelativePath intentionally leaves nested env files
// (e.g. svc/.env) in the raw write set — only the project root is
// merge-managed.
func isReservedRootEnvFileInternal(relPath string) bool {
	switch relPath {
	case projects.EffectiveEnvFileName, projects.GitSourceEnvFileName, projects.OverrideEnvFileName, projects.GlobalEnvFileName:
		return true
	default:
		return false
	}
}

// filterReservedRootEnvFilesInternal drops reserved root env file paths from a
// tracked synced-file list. Syncs created before the override-merge migration
// may still have .env recorded from an earlier sync; leaving it in would make
// CleanupRemovedFiles treat .env as removed-by-git and delete the live-copied
// stage .env before ApplyGitSyncEnvToDirectory can read it for the merge.
func filterReservedRootEnvFilesInternal(paths []string) []string {
	filtered := make([]string, 0, len(paths))
	for _, p := range paths {
		if !isReservedRootEnvFileInternal(p) {
			filtered = append(filtered, p)
		}
	}
	return filtered
}

// partitionReservedRootEnvFilesInternal splits syncFiles into the set safe to
// write directly (everything except the reserved root env files) and the
// git-sourced root .env content, if the repo has one. A committed
// .env.git/project.env/.env.global at the project root is dropped rather than
// raw-written: those are Arcane-owned bookkeeping files that must only ever be
// produced by the override merge, never by an untrusted git payload.
func partitionReservedRootEnvFilesInternal(ctx context.Context, syncFiles []projects.SyncFile) ([]projects.SyncFile, *string) {
	filtered := make([]projects.SyncFile, 0, len(syncFiles))
	var gitEnvContent *string
	for _, file := range syncFiles {
		if !isReservedRootEnvFileInternal(file.RelativePath) {
			filtered = append(filtered, file)
			continue
		}
		switch file.RelativePath {
		case projects.EffectiveEnvFileName:
			content := string(file.Content)
			gitEnvContent = &content
		case projects.GlobalEnvFileName:
			slog.WarnContext(ctx, "dropping .env.global from git payload; project-root env files are not raw-written from git and .env.global is not project-scoped",
				"path", file.RelativePath,
			)
		default:
			slog.WarnContext(ctx, "dropping reserved Arcane env file from git payload; will be re-derived from override merge",
				"path", file.RelativePath,
			)
		}
	}
	return filtered, gitEnvContent
}

// seedStageEnvFromCandidateDirInternal copies env files from a pre-existing project
// directory at the conventional path (projectsDir/<sanitized-sync-name>/) into the
// staging directory before initial-sync validation. This lets users pre-seed
// ${VAR} substitutions via a server-side .env when the compose file in git
// expects values that the git repo intentionally does not provide. Only env
// files are touched — other files would conflict with what WriteSyncedDirectory
// is about to lay down from git.
func (s *GitOpsSyncService) seedStageEnvFromCandidateDirInternal(ctx context.Context, sync *projectpkg.GitOpsSync, projectsDir, stagePath string) error {
	// The candidate may itself be a symlinked project directory, so it is probed
	// with os.Stat and then used as the confinement root for the reads below.
	candidatePath := filepath.Join(projectsDir, projects.SanitizeProjectName(sync.ProjectName))
	info, err := os.Stat(candidatePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return errors.WrapIff(err, "inspect pre-existing project directory %s", candidatePath)
	}
	if !info.IsDir() {
		return nil
	}

	// A permission error (e.g. a chmod 000 or foreign-owned file) is treated
	// like the file being absent: it's skipped rather than aborting the whole
	// staging attempt, since seeding is a best-effort convenience.
	readOptional := func(name string) (string, bool, error) {
		content, readErr := acfs.ReadFile(ctx, candidatePath, "/"+name)
		if readErr == nil {
			return string(content), true, nil
		}
		if errors.Is(readErr, fs.ErrNotExist) {
			return "", false, nil
		}
		if errors.Is(readErr, os.ErrPermission) {
			slog.WarnContext(ctx, "skipping permission-locked project env file while seeding GitOps stage", "path", filepath.Join(candidatePath, name))
			return "", false, nil
		}
		return "", false, errors.WrapIff(readErr, "read %s from %s", name, candidatePath)
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
		if err := projects.WriteProjectFile(ctx, projectsDir, stagePath, projects.GitSourceEnvFileName, gitSource); err != nil {
			return errors.WrapIf(err, "seed stage .env.git")
		}
	}
	if hasOverride {
		if err := projects.WriteProjectFile(ctx, projectsDir, stagePath, projects.OverrideEnvFileName, override); err != nil {
			return errors.WrapIf(err, "seed stage project.env")
		}
	}

	if hasEffective {
		if err := projects.WriteProjectFile(ctx, projectsDir, stagePath, ".env", effective); err != nil {
			return errors.WrapIf(err, "seed stage .env")
		}
	} else {
		// Only .env.git and/or project.env exist, so derive .env from them.
		merged, mergeErr := projects.BuildEffectiveEnvContent(gitSource, override)
		if mergeErr != nil {
			return errors.WrapIf(mergeErr, "build effective env from pre-existing project")
		}
		if err := projects.WriteProjectFile(ctx, projectsDir, stagePath, ".env", merged); err != nil {
			return errors.WrapIf(err, "seed stage .env")
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

func (s *GitOpsSyncService) lookupProjectByIDInternal(ctx context.Context, projectID string) (*projectpkg.Project, bool, error) {
	var project projectpkg.Project
	if err := s.db.WithContext(ctx).Where("id = ?", projectID).First(&project).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, false, nil
		}
		return nil, false, errors.WrapIff(err, "failed to get project %s", projectID)
	}

	return &project, true, nil
}

func (s *GitOpsSyncService) lookupProjectByPathInternal(ctx context.Context, projectPath string) (*projectpkg.Project, bool, error) {
	var project projectpkg.Project
	if err := s.db.WithContext(ctx).Where("path = ?", projectPath).First(&project).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, false, nil
		}
		return nil, false, errors.WrapIff(err, "failed to get project by path %s", projectPath)
	}

	return &project, true, nil
}

func (s *GitOpsSyncService) findRecoverableManagedProjectInternal(ctx context.Context, sync *projectpkg.GitOpsSync) (*projectpkg.Project, error) {
	var managedProjects []projectpkg.Project
	if err := s.db.WithContext(ctx).
		Where("gitops_managed_by = ?", sync.ID).
		Find(&managedProjects).Error; err != nil {
		return nil, errors.WrapIff(err, "failed to list GitOps-managed projects for sync %s", sync.ID)
	}

	matches := make([]projectpkg.Project, 0, len(managedProjects))
	for i := range managedProjects {
		project := managedProjects[i]
		if err := s.projectService.EnsureProjectPathUnderRoot(ctx, &project, true); err != nil {
			return nil, err
		}
		if _, err := s.projectService.ResolveProjectComposeFile(ctx, &project); err != nil {
			if errors.Is(err, common.ErrProjectComposeFileNotFound) {
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
		return nil, errors.Errorf("multiple GitOps-managed projects match sync %s; refusing automatic relink", sync.ID)
	}
}

func (s *GitOpsSyncService) findUniqueProjectDirectoryCandidateInternal(ctx context.Context, sync *projectpkg.GitOpsSync) (string, error) {
	projectsDir, err := s.projectService.GetProjectsDirectory(ctx)
	if err != nil {
		return "", err
	}

	entries, err := acfs.List(ctx, projectsDir, "/")
	if err != nil {
		return "", errors.WrapIff(err, "failed to list projects directory %s", projectsDir)
	}

	composeFileName := strings.TrimSpace(filepath.Base(sync.ComposePath))
	if composeFileName == "" || composeFileName == "." {
		return "", nil
	}

	prefix := projects.SanitizeProjectName(sync.ProjectName)
	matches := make([]string, 0, 1)
	for _, entry := range entries {
		// Adoption never follows symlinks, and acfs reports a symlink as a
		// non-directory, so a plain directory is the only candidate kind.
		if !entry.IsDirectory {
			continue
		}
		// Never adopt one of Arcane's own scratch dirs as a recovery candidate.
		if projects.IsInternalScratchDirName(entry.Name) {
			continue
		}
		if prefix != "" && entry.Name != prefix && !strings.HasPrefix(entry.Name, prefix+"-") {
			continue
		}

		candidatePath := filepath.Join(projectsDir, entry.Name)
		composePath := filepath.Join(candidatePath, composeFileName)
		if composeEntry, statErr := acfs.Stat(ctx, projectsDir, path.Join(entry.Path, composeFileName), true); statErr == nil {
			if !composeEntry.IsDirectory {
				matches = append(matches, candidatePath)
			}
		} else if !errors.Is(statErr, fs.ErrNotExist) {
			return "", errors.WrapIff(statErr, "failed to inspect recovery candidate %s", composePath)
		}
	}

	switch len(matches) {
	case 0:
		return "", nil
	case 1:
		return matches[0], nil
	default:
		return "", errors.Errorf("multiple candidate project directories match sync %s; refusing automatic relink", sync.ID)
	}
}

func (s *GitOpsSyncService) createRecoveredProjectFromDirectoryInternal(ctx context.Context, sync *projectpkg.GitOpsSync, projectPath string) (*projectpkg.Project, error) {
	project := &projectpkg.Project{
		Name:            sync.ProjectName,
		DirName:         new(filepath.Base(projectPath)),
		Path:            projectPath,
		Status:          projectpkg.ProjectStatusUnknown,
		StatusReason:    new("Project recovered from existing GitOps-managed directory"),
		ServiceCount:    0,
		RunningCount:    0,
		GitOpsManagedBy: &sync.ID,
	}

	if serviceCount, err := s.projectService.CountServicesFromCompose(ctx, *project); err == nil {
		project.ServiceCount = serviceCount
	} else {
		slog.WarnContext(ctx, "Failed to count services while recovering GitOps project", "syncId", sync.ID, "path", projectPath, "error", err)
	}

	if err := s.projectService.CreateGitOpsManagedProject(ctx, sync, project, common.User{}, false); err != nil {
		return nil, err
	}

	return project, nil
}

func (s *GitOpsSyncService) recoverProjectFromDirectoryCandidateInternal(ctx context.Context, sync *projectpkg.GitOpsSync) (*projectpkg.Project, error) {
	projectPath, err := s.findUniqueProjectDirectoryCandidateInternal(ctx, sync)
	if err != nil || projectPath == "" {
		return nil, err
	}

	project, found, err := s.lookupProjectByPathInternal(ctx, projectPath)
	if err != nil {
		return nil, err
	}
	if found {
		if err := s.projectService.EnsureProjectPathUnderRoot(ctx, project, true); err != nil {
			return nil, err
		}
		if err := s.projectService.EnsureGitOpsProjectLinked(ctx, sync, project); err != nil {
			return nil, err
		}
		return project, nil
	}

	return s.createRecoveredProjectFromDirectoryInternal(ctx, sync, projectPath)
}

// getDirectorySyncProjectInternal resolves the linked project for a sync when one
// exists, while tolerating deleted/stale project references.
func (s *GitOpsSyncService) getDirectorySyncProjectInternal(ctx context.Context, sync *projectpkg.GitOpsSync) (*projectpkg.Project, error) {
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
			if err := s.projectService.EnsureProjectPathUnderRoot(ctx, project, true); err != nil {
				return nil, err
			}
			if err := s.projectService.EnsureGitOpsProjectLinked(ctx, sync, project); err != nil {
				return nil, err
			}
			return project, nil
		}

		slog.WarnContext(ctx, "Existing project not found, attempting recovery", "projectId", *sync.ProjectID, "syncId", sync.ID)
	}

	project, err := s.findRecoverableManagedProjectInternal(ctx, sync)
	if err != nil {
		if establishedBinding {
			return nil, common.Classify(common.ErrGitOpsSyncProjectBindingBroken, errors.WrapIf(errors.WrapIff(err, "sync %s references missing project %s", sync.ID, *sync.ProjectID), "GitOps sync project binding broken"))
		}
		return nil, err
	}
	if project != nil {
		if err := s.projectService.EnsureGitOpsProjectLinked(ctx, sync, project); err != nil {
			return nil, err
		}
		return project, nil
	}

	project, err = s.recoverProjectFromDirectoryCandidateInternal(ctx, sync)
	if err != nil {
		if establishedBinding {
			return nil, common.Classify(common.ErrGitOpsSyncProjectBindingBroken, errors.WrapIf(errors.WrapIff(err, "sync %s references missing project %s", sync.ID, *sync.ProjectID), "GitOps sync project binding broken"))
		}
		return nil, err
	}
	if project != nil {
		return project, nil
	}

	if establishedBinding {
		return nil, common.Classify(common.ErrGitOpsSyncProjectBindingBroken, errors.WrapIf(errors.Errorf("sync %s references missing project %s: no unique recovery candidate was found", sync.ID, *sync.ProjectID), "GitOps sync project binding broken"))
	}

	return nil, nil
}

// createDirectorySyncProjectInternal promotes a validated staged tree into a new
// managed project directory and links it back to the Git sync record.
func (s *GitOpsSyncService) createDirectorySyncProjectInternal(ctx context.Context, sync *projectpkg.GitOpsSync, stage *stagedDirectorySync, actor common.User) (*projectpkg.Project, error) {
	projectsDir, err := s.projectService.GetProjectsDirectory(ctx)
	if err != nil {
		return nil, errors.WrapIf(err, "failed to get projects directory")
	}

	// Non-suffixing create: a GitOps sync must never mint a "-N" duplicate. Any
	// adoptable existing directory was already resolved by getDirectorySyncProjectInternal,
	// so a collision here means the name is taken by an unrelated/unrecoverable dir;
	// treat it as a broken binding rather than creating a duplicate.
	basePath := filepath.Join(projectsDir, projects.SanitizeProjectName(sync.ProjectName))
	projectPath, folderName, err := projects.CreateExactDir(ctx, projectsDir, basePath, sync.ProjectName, 0o755)
	if err != nil {
		if errors.Is(err, projects.ErrProjectDirExists) {
			return nil, common.Classify(common.ErrGitOpsSyncProjectBindingBroken, errors.WrapIf(errors.Errorf("sync %s cannot create project %q: a directory with that name already exists; refusing to create a duplicate", sync.ID, projects.SanitizeProjectName(sync.ProjectName)), "GitOps sync project binding broken"))
		}
		return nil, errors.WrapIf(err, "failed to create project directory")
	}

	projectLogical, err := acfs.LogicalPath(projectsDir, projectPath)
	if err != nil {
		return nil, errors.WrapIf(err, "failed to resolve created project directory")
	}
	if err := acfs.Remove(ctx, projectsDir, projectLogical); err != nil {
		return nil, errors.WrapIf(err, "failed to prepare project directory")
	}

	if err := acfs.Rename(ctx, projectsDir, stage.stageLogical, projectLogical); err != nil {
		return nil, errors.WrapIf(err, "failed to promote staged project directory")
	}
	stage.stagePath = ""

	project := &projectpkg.Project{
		Name:         sync.ProjectName,
		DirName:      new(folderName),
		Path:         projectPath,
		Status:       projectpkg.ProjectStatusStopped,
		ServiceCount: stage.serviceCount,
		RunningCount: 0,
	}

	if err := s.projectService.CreateGitOpsManagedProject(ctx, sync, project, actor); err != nil {
		_ = acfs.RemoveAll(ctx, projectsDir, projectLogical)
		return nil, err
	}

	return project, nil
}

// updateDirectorySyncProjectInternal mirrors a validated staged tree into the
// existing project path in place so running containers keep their bind-mount
// inodes; a temporary backup copy allows rollback if promotion fails.
func (s *GitOpsSyncService) updateDirectorySyncProjectInternal(ctx context.Context, sync *projectpkg.GitOpsSync, stage *stagedDirectorySync) (*projectpkg.Project, error) {
	project := stage.project
	projectPath := filepath.Clean(project.Path)
	backupPath := ""
	existed := true

	// The project directory is the confinement root for the mirror below and may
	// itself be a symlink, so it is probed and bootstrapped through os.
	if info, err := os.Stat(projectPath); err == nil {
		if !info.IsDir() {
			return nil, errors.Errorf("project path is not a directory: %s", projectPath)
		}
	} else if errors.Is(err, os.ErrNotExist) {
		existed = false
		if err := os.MkdirAll(projectPath, 0o755); err != nil {
			return nil, errors.WrapIf(err, "failed to recreate project directory")
		}
	} else {
		return nil, errors.WrapIf(err, "failed to inspect current project directory")
	}

	var backupSkipped []string
	if existed {
		projectsDir, err := s.projectService.GetProjectsDirectory(ctx)
		if err != nil {
			return nil, errors.WrapIf(err, "failed to get projects directory")
		}
		backupLogical, tempErr := acfs.MkdirTemp(ctx, projectsDir, "/", ".gitops-backup-*")
		if tempErr != nil {
			return nil, errors.WrapIf(tempErr, "failed to create backup directory")
		}
		backupPath = filepath.Join(projectsDir, filepath.FromSlash(strings.TrimPrefix(backupLogical, "/")))
		defer func() { _ = acfs.RemoveAll(ctx, projectsDir, backupLogical) }()
		// Tolerate unreadable files (e.g. foreign-owned bind-mount data) so an
		// unrelated file can't block the backup; the skipped paths are absent from
		// the backup and must be preserved, not pruned, when restoring.
		backedUp, backupErr := acfs.CopyDir(ctx, projectPath, backupPath, acfstypes.CopyOptions{TolerateUnreadable: true})
		backupSkipped = backedUp.Skipped
		if err = backupErr; err != nil {
			return nil, errors.WrapIf(err, "failed to back up current project directory")
		}
		if len(backupSkipped) > 0 {
			slog.WarnContext(ctx, "skipped unreadable files while backing up project for sync; they will be left untouched on rollback", "projectPath", projectPath, "skipped", backupSkipped)
		}
	}

	restore := func() {
		var restoreErr error
		if existed {
			restoreErr = acfs.MirrorDir(ctx, backupPath, projectPath, acfstypes.MirrorOptions{Preserve: backupSkipped})
		} else {
			restoreErr = acfs.RemoveAll(ctx, filepath.Dir(projectPath), "/"+filepath.Base(projectPath))
		}
		if restoreErr != nil {
			slog.ErrorContext(ctx, "Failed to restore project directory after sync promotion failure; directory may be in a mixed state", "projectPath", projectPath, "backupPath", backupPath, "error", restoreErr)
		}
	}

	// Preserve files skipped while staging: they remain in the live project but are
	// absent from the stage, so a plain mirror would prune them.
	if err := acfs.MirrorDir(ctx, stage.stagePath, projectPath, acfstypes.MirrorOptions{Preserve: stage.copySkipped}); err != nil {
		restore()
		return nil, errors.WrapIf(err, "failed to promote staged project directory")
	}

	if err := s.db.WithContext(ctx).Model(&projectpkg.Project{}).Where("id = ?", project.ID).Updates(map[string]any{
		"service_count":     stage.serviceCount,
		"gitops_managed_by": sync.ID,
		"updated_at":        time.Now(),
	}).Error; err != nil {
		restore()
		return nil, errors.WrapIf(err, "failed to update project metadata after directory sync")
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

	if err := s.db.WithContext(ctx).Model(&projectpkg.GitOpsSync{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		slog.ErrorContext(ctx, "Failed to update sync status with files", "error", err, "syncId", id)
	}
}
