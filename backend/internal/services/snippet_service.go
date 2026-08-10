package services

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"emperror.dev/errors"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/mapper"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
	snippettypes "github.com/getarcaneapp/arcane/types/v2/snippet"
)

// Snippet run limits. Mirrors the lifecycle hook's output cap; see
// lifecycleMaxOutputBytes for why 16 KiB and why no re-slicing.
const (
	snippetDefaultTimeoutSec = 60
	snippetMaxTimeoutSec     = 300
	snippetMaxOutputBytes    = 16 * 1024
	// snippetRunHistoryLimit is a fixed cap, not a setting — see
	// pruneSnippetRunsInternal.
	snippetRunHistoryLimit = 50
)

// SnippetService runs user-authored commands with typed parameters, manually
// or on a cron schedule, either on the host shell (HostShellService.RunScript)
// or inside a target container (ContainerService.RunScript).
type SnippetService struct {
	db               *database.DB
	eventService     *EventService
	hostShellService *HostShellService
	containerService *ContainerService

	// scheduler and lifecycleCtx are injected post-construction via
	// SetScheduler, for the same reason as GitOpsSyncService's identical
	// fields: pkg/scheduler imports this package, so the scheduler can't be
	// a wire input here, and it's built after the service graph.
	scheduler    schedulertypes.DynamicScheduler
	lifecycleCtx context.Context
}

func NewSnippetService(db *database.DB, eventService *EventService, hostShellService *HostShellService, containerService *ContainerService) *SnippetService {
	return &SnippetService{
		db:               db,
		eventService:     eventService,
		hostShellService: hostShellService,
		containerService: containerService,
	}
}

const snippetJobPrefix = "snippet:"

func snippetJobNameInternal(snippetID string) string { return snippetJobPrefix + snippetID }

// SetScheduler injects the job scheduler and the app lifecycle context. Must
// be called during bootstrap, after the service graph is built, before any
// snippet schedule is registered.
func (s *SnippetService) SetScheduler(ctx context.Context, scheduler schedulertypes.DynamicScheduler) { //nolint:contextcheck // scheduled runs must capture the app lifecycle context, not request contexts
	if ctx == nil {
		ctx = context.Background()
	}
	s.lifecycleCtx = ctx
	s.scheduler = scheduler
}

func (s *SnippetService) schedulerCtxInternal(ctx context.Context) context.Context {
	if s.lifecycleCtx != nil {
		return s.lifecycleCtx
	}
	if ctx != nil {
		return context.WithoutCancel(ctx)
	}
	return context.Background()
}

// buildSnippetJobInternal returns the dynamic job for a single snippet's
// schedule. Unlike GitOps auto-sync (a fixed "@every Nm" interval), the
// schedule is the raw 6-field cron expression the user supplied; the
// scheduler's own cron parser validates it, and a parse error is surfaced to
// the caller by registerJobInternal instead of only being logged.
func (s *SnippetService) buildSnippetJobInternal(snippetID, environmentID, schedule string) *schedulertypes.GenericJob {
	return &schedulertypes.GenericJob{
		JobName: snippetJobNameInternal(snippetID),
		ScheduleFn: func(_ context.Context) string {
			return schedule
		},
		RunFn: func(ctx context.Context) {
			s.runScheduledInternal(ctx, environmentID, snippetID)
		},
	}
}

// runScheduledInternal is the body of a scheduled snippet fire. It re-reads
// the snippet each run so a row toggled to ScheduleEnabled=false or deleted
// out-of-band self-cancels instead of firing forever.
func (s *SnippetService) runScheduledInternal(ctx context.Context, environmentID, snippetID string) {
	snippet, err := s.getSnippetRecordInternal(ctx, environmentID, snippetID)
	if err != nil {
		if errors.Is(err, common.ErrNotFound) {
			slog.InfoContext(ctx, "snippet schedule unregistering; snippet no longer exists", "snippetId", snippetID)
			s.unregisterJobInternal(ctx, snippetID)
			return
		}
		slog.DebugContext(ctx, "scheduled snippet run skipped; failed to load snippet", "snippetId", snippetID, "error", err)
		return
	}
	if !snippet.ScheduleEnabled {
		return
	}
	if _, err := s.RunSnippet(ctx, environmentID, snippetID, snippet.ScheduleParameters, "schedule", systemUser); err != nil {
		slog.ErrorContext(ctx, "scheduled snippet run failed", "snippetId", snippetID, "error", err)
	}
}

// registerJobInternal (re-)registers the recurring job for a snippet. Unlike
// GitOps auto-sync jobs, a bad cron expression is a caller mistake made at
// write time, so the scheduler's parse error is returned rather than only
// logged — CreateSnippet/UpdateSnippet turn it into a 400.
func (s *SnippetService) registerJobInternal(ctx context.Context, snippetID, environmentID, schedule string) error {
	if s.scheduler == nil || strings.TrimSpace(schedule) == "" {
		return nil
	}
	job := s.buildSnippetJobInternal(snippetID, environmentID, schedule)
	if err := s.scheduler.AddJob(s.schedulerCtxInternal(ctx), job); err != nil {
		return common.Classify(common.ErrValidation, errors.WrapIf(err, "invalid schedule"))
	}
	return nil
}

func (s *SnippetService) unregisterJobInternal(ctx context.Context, snippetID string) {
	if s.scheduler == nil {
		return
	}
	s.scheduler.RemoveJob(s.schedulerCtxInternal(ctx), snippetJobNameInternal(snippetID))
}

// RegisterScheduledSnippetsOnStartup registers a dynamic job for every
// schedule-enabled snippet. Unlike GitOps auto-sync, there is deliberately no
// overdue kick: a missed snippet run should not fire just because the
// process restarted.
func (s *SnippetService) RegisterScheduledSnippetsOnStartup(ctx context.Context) {
	if s.scheduler == nil {
		return
	}
	var snippets []models.Snippet
	if err := s.db.WithContext(ctx).
		Where("schedule_enabled = ? AND schedule IS NOT NULL AND schedule != '' AND environment_id IN (SELECT id FROM environments)", true).
		Find(&snippets).Error; err != nil {
		slog.ErrorContext(ctx, "failed to load scheduled snippets on startup", "error", err)
		return
	}
	for i := range snippets {
		snippet := snippets[i]
		schedule := ""
		if snippet.Schedule != nil {
			schedule = *snippet.Schedule
		}
		if err := s.registerJobInternal(ctx, snippet.ID, snippet.EnvironmentID, schedule); err != nil {
			slog.ErrorContext(ctx, "failed to register snippet schedule on startup", "snippetId", snippet.ID, "error", err)
		}
	}
	slog.InfoContext(ctx, "registered scheduled snippet jobs on startup", "count", len(snippets))
}

func (s *SnippetService) GetSnippetsPaginated(ctx context.Context, environmentID string, params pagination.QueryParams) ([]snippettypes.Snippet, pagination.Response, error) {
	var snippets []models.Snippet
	q := s.db.WithContext(ctx).Model(&models.Snippet{}).Where("environment_id = ?", environmentID)

	if term := strings.TrimSpace(params.Search); term != "" {
		searchPattern := "%" + term + "%"
		q = q.Where("name LIKE ? OR description LIKE ?", searchPattern, searchPattern)
	}

	paginationResp, err := pagination.PaginateAndSortDB(params, q, &snippets)
	if err != nil {
		return nil, pagination.Response{}, errors.WrapIf(err, "failed to paginate snippets")
	}

	out, mapErr := mapper.MapSlice[models.Snippet, snippettypes.Snippet](snippets)
	if mapErr != nil {
		return nil, pagination.Response{}, errors.WrapIf(mapErr, "failed to map snippets")
	}
	return out, paginationResp, nil
}

func (s *SnippetService) GetSnippetByID(ctx context.Context, environmentID, id string) (*models.Snippet, error) {
	return s.getSnippetRecordInternal(ctx, environmentID, id)
}

func (s *SnippetService) getSnippetRecordInternal(ctx context.Context, environmentID, id string) (*models.Snippet, error) {
	var snippet models.Snippet
	q := s.db.WithContext(ctx).Where("id = ?", id)
	if environmentID != "" {
		q = q.Where("environment_id = ?", environmentID)
	}
	if err := q.First(&snippet).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.Classify(common.ErrNotFound, errors.New("snippet not found"))
		}
		return nil, errors.WrapIf(err, "failed to get snippet")
	}
	return &snippet, nil
}

func (s *SnippetService) checkNameConflictInternal(ctx context.Context, environmentID, name, excludeID string) error {
	q := s.db.WithContext(ctx).Model(&models.Snippet{}).
		Where("environment_id = ? AND name = ?", environmentID, name)
	if excludeID != "" {
		q = q.Where("id <> ?", excludeID)
	}
	var count int64
	if err := q.Count(&count).Error; err != nil {
		return errors.WrapIf(err, "failed to check for conflicting snippet name")
	}
	if count > 0 {
		return common.Classify(common.ErrConflict, errors.Errorf("a snippet named %q already exists in this environment", name))
	}
	return nil
}

// resolveSnippetTargetInternal validates the requested target, defaulting to
// "host" when omitted/blank.
func resolveSnippetTargetInternal(requested *string) (string, error) {
	target := snippettypes.TargetHost
	if requested != nil && strings.TrimSpace(*requested) != "" {
		target = strings.TrimSpace(*requested)
	}
	if target != snippettypes.TargetHost && target != snippettypes.TargetContainer {
		return "", common.Classify(common.ErrValidation, errors.Errorf("target must be %q or %q", snippettypes.TargetHost, snippettypes.TargetContainer))
	}
	return target, nil
}

func resolveSnippetTimeoutSecInternal(requested *int) int {
	if requested == nil || *requested <= 0 {
		return snippetDefaultTimeoutSec
	}
	if *requested > snippetMaxTimeoutSec {
		return snippetMaxTimeoutSec
	}
	return *requested
}

func (s *SnippetService) CreateSnippet(ctx context.Context, environmentID string, req snippettypes.CreateSnippetRequest, actor models.User) (*models.Snippet, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, common.Classify(common.ErrValidation, errors.New("name is required"))
	}
	if strings.TrimSpace(req.Script) == "" {
		return nil, common.Classify(common.ErrValidation, errors.New("script is required"))
	}
	target, err := resolveSnippetTargetInternal(req.Target)
	if err != nil {
		return nil, err
	}
	var containerID *string
	if target == snippettypes.TargetContainer {
		trimmed := strings.TrimSpace(stringOrEmptyInternal(req.ContainerID))
		if trimmed == "" {
			return nil, common.Classify(common.ErrValidation, errors.New("containerId is required when target is \"container\""))
		}
		containerID = &trimmed
	}
	if err := validateSnippetParameterDefsInternal(req.Parameters); err != nil {
		return nil, err
	}
	if err := s.checkNameConflictInternal(ctx, environmentID, name, ""); err != nil {
		return nil, err
	}
	if req.ScheduleParameters != nil {
		if _, err := resolveSnippetParamsInternal(req.Parameters, req.ScheduleParameters); err != nil {
			return nil, errors.WrapIf(err, "schedule parameters")
		}
	}

	snippet := models.Snippet{
		EnvironmentID:      environmentID,
		Name:               name,
		Description:        nullableTrimmedStringInternal(req.Description),
		Script:             req.Script,
		Target:             target,
		ContainerID:        containerID,
		Parameters:         normalizeSnippetParametersInternal(req.Parameters),
		WorkingDir:         nullableTrimmedStringInternal(req.WorkingDir),
		TimeoutSec:         resolveSnippetTimeoutSecInternal(req.TimeoutSec),
		Schedule:           nullableTrimmedStringInternal(req.Schedule),
		ScheduleParameters: normalizeSnippetScheduleParametersInternal(req.ScheduleParameters),
		CreatedByUserID:    nullableTrimmedStringInternal(&actor.ID),
	}
	if req.ScheduleEnabled != nil {
		snippet.ScheduleEnabled = *req.ScheduleEnabled
	}

	if err := s.db.WithContext(ctx).Select("*").Create(&snippet).Error; err != nil { //nolint:unqueryvet // intentional Select("*"); see GitOpsSyncService.CreateSync
		return nil, errors.WrapIf(err, "failed to create snippet")
	}

	if snippet.ScheduleEnabled {
		if err := s.registerJobInternal(ctx, snippet.ID, environmentID, stringOrEmptyInternal(snippet.Schedule)); err != nil {
			// The row is already created; surface the schedule error but leave
			// the snippet in place with its schedule inactive rather than
			// rolling back a snippet the user can otherwise use immediately.
			_ = s.db.WithContext(ctx).Model(&snippet).Update("schedule_enabled", false).Error
			return nil, err
		}
	}

	_, _ = s.eventService.CreateEvent(ctx, CreateEventRequest{
		Type:          models.EventTypeSnippetCreate,
		Severity:      models.EventSeveritySuccess,
		Title:         "Snippet created: " + snippet.Name,
		Description:   "Snippet '" + snippet.Name + "' has been created",
		ResourceType:  new("snippet"),
		ResourceID:    new(snippet.ID),
		ResourceName:  new(snippet.Name),
		UserID:        new(actor.ID),
		Username:      new(actor.Username),
		EnvironmentID: new(environmentID),
	})

	return s.getSnippetRecordInternal(ctx, environmentID, snippet.ID)
}

func (s *SnippetService) UpdateSnippet(ctx context.Context, environmentID, id string, req snippettypes.UpdateSnippetRequest, actor models.User) (*models.Snippet, error) {
	snippet, err := s.getSnippetRecordInternal(ctx, environmentID, id)
	if err != nil {
		return nil, err
	}

	finalParameters := snippet.Parameters
	if req.Parameters != nil {
		if err := validateSnippetParameterDefsInternal(req.Parameters); err != nil {
			return nil, err
		}
		finalParameters = req.Parameters
	}

	finalScheduleParameters := snippet.ScheduleParameters
	if req.ScheduleParameters != nil {
		finalScheduleParameters = req.ScheduleParameters
	}
	if finalScheduleParameters != nil {
		if _, err := resolveSnippetParamsInternal(finalParameters, finalScheduleParameters); err != nil {
			return nil, errors.WrapIf(err, "schedule parameters")
		}
	}

	newScheduleEnabled := snippet.ScheduleEnabled
	if req.ScheduleEnabled != nil {
		newScheduleEnabled = *req.ScheduleEnabled
	}
	newSchedule := stringOrEmptyInternal(snippet.Schedule)
	if req.Schedule != nil {
		newSchedule = strings.TrimSpace(*req.Schedule)
	}

	updates := make(map[string]any)
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return nil, common.Classify(common.ErrValidation, errors.New("name is required"))
		}
		if name != snippet.Name {
			if err := s.checkNameConflictInternal(ctx, environmentID, name, id); err != nil {
				return nil, err
			}
		}
		updates["name"] = name
	}
	if req.Description != nil {
		updates["description"] = nullableUpdateStringValueInternal(req.Description)
	}
	if req.Script != nil {
		if strings.TrimSpace(*req.Script) == "" {
			return nil, common.Classify(common.ErrValidation, errors.New("script is required"))
		}
		updates["script"] = *req.Script
	}

	finalTarget := snippet.Target
	if finalTarget == "" {
		finalTarget = snippettypes.TargetHost
	}
	if req.Target != nil {
		resolved, err := resolveSnippetTargetInternal(req.Target)
		if err != nil {
			return nil, err
		}
		finalTarget = resolved
		updates["target"] = resolved
	}
	finalContainerID := stringOrEmptyInternal(snippet.ContainerID)
	if req.ContainerID != nil {
		finalContainerID = strings.TrimSpace(*req.ContainerID)
		updates["container_id"] = nullableUpdateStringValueInternal(req.ContainerID)
	}
	if finalTarget == snippettypes.TargetContainer && finalContainerID == "" {
		return nil, common.Classify(common.ErrValidation, errors.New("containerId is required when target is \"container\""))
	}
	if finalTarget == snippettypes.TargetHost && finalContainerID != "" {
		finalContainerID = ""
		updates["container_id"] = nil
	}

	if req.Parameters != nil {
		encoded, err := jsonColumnValueInternal(normalizeSnippetParametersInternal(req.Parameters))
		if err != nil {
			return nil, errors.WrapIf(err, "failed to encode parameters")
		}
		updates["parameters"] = encoded
	}
	if req.WorkingDir != nil {
		updates["working_dir"] = nullableUpdateStringValueInternal(req.WorkingDir)
	}
	if req.TimeoutSec != nil {
		updates["timeout_sec"] = resolveSnippetTimeoutSecInternal(req.TimeoutSec)
	}
	if req.Schedule != nil {
		updates["schedule"] = nullableUpdateStringValueInternal(req.Schedule)
	}
	if req.ScheduleEnabled != nil {
		updates["schedule_enabled"] = *req.ScheduleEnabled
	}
	if req.ScheduleParameters != nil {
		encoded, err := jsonColumnValueInternal(normalizeSnippetScheduleParametersInternal(req.ScheduleParameters))
		if err != nil {
			return nil, errors.WrapIf(err, "failed to encode schedule parameters")
		}
		updates["schedule_parameters"] = encoded
	}

	if newScheduleEnabled {
		if err := s.registerJobInternal(ctx, id, environmentID, newSchedule); err != nil {
			return nil, err
		}
	}

	if len(updates) > 0 {
		if err := s.db.WithContext(ctx).Model(snippet).Updates(updates).Error; err != nil {
			return nil, errors.WrapIf(err, "failed to update snippet")
		}

		_, _ = s.eventService.CreateEvent(ctx, CreateEventRequest{
			Type:          models.EventTypeSnippetUpdate,
			Severity:      models.EventSeveritySuccess,
			Title:         "Snippet updated: " + snippet.Name,
			Description:   "Snippet '" + snippet.Name + "' has been updated",
			ResourceType:  new("snippet"),
			ResourceID:    new(snippet.ID),
			ResourceName:  new(snippet.Name),
			UserID:        new(actor.ID),
			Username:      new(actor.Username),
			EnvironmentID: new(environmentID),
		})
	}

	if !newScheduleEnabled {
		s.unregisterJobInternal(ctx, id)
	}

	return s.getSnippetRecordInternal(ctx, environmentID, id)
}

func (s *SnippetService) DeleteSnippet(ctx context.Context, environmentID, id string, actor models.User) error {
	// Stop the recurring job first, unconditionally, mirroring
	// GitOpsSyncService.DeleteSync: even a row that fails to load below must
	// stop firing.
	s.unregisterJobInternal(ctx, id)

	snippet, loadErr := s.getSnippetRecordInternal(ctx, environmentID, id)

	if err := s.db.WithContext(ctx).Where("id = ?", id).Delete(&models.Snippet{}).Error; err != nil {
		if loadErr == nil && snippet.ScheduleEnabled {
			_ = s.registerJobInternal(ctx, snippet.ID, snippet.EnvironmentID, stringOrEmptyInternal(snippet.Schedule))
		}
		return errors.WrapIf(err, "failed to delete snippet")
	}

	if loadErr != nil {
		slog.WarnContext(ctx, "deleted snippet whose record could not be loaded", "snippetId", id, "loadError", loadErr)
		return nil
	}

	_, _ = s.eventService.CreateEvent(ctx, CreateEventRequest{
		Type:          models.EventTypeSnippetDelete,
		Severity:      models.EventSeverityWarning,
		Title:         "Snippet deleted: " + snippet.Name,
		Description:   "Snippet '" + snippet.Name + "' has been deleted",
		ResourceType:  new("snippet"),
		ResourceID:    new(snippet.ID),
		ResourceName:  new(snippet.Name),
		UserID:        new(actor.ID),
		Username:      new(actor.Username),
		EnvironmentID: new(environmentID),
	})

	return nil
}

// resolveSnippetRunnerInternal picks the RunScript implementation for
// snippet.Target and validates that target is actually runnable right now:
// the host shell must be enabled, or the target container must exist and be
// running.
func (s *SnippetService) resolveSnippetRunnerInternal(ctx context.Context, snippet *models.Snippet) (func(context.Context, ScriptRequest) (ScriptResult, error), error) {
	if snippet.Target == snippettypes.TargetContainer {
		containerID := stringOrEmptyInternal(snippet.ContainerID)
		if containerID == "" {
			return nil, common.Classify(common.ErrValidation, errors.New("snippet has no target container configured"))
		}
		info, err := s.containerService.GetContainerByID(ctx, containerID)
		if err != nil {
			return nil, common.Classify(common.ErrNotFound, errors.WrapIf(err, "target container not found"))
		}
		if !info.State.Running {
			return nil, common.Classify(common.ErrUnavailable, errors.New("target container is not running"))
		}
		return func(ctx context.Context, req ScriptRequest) (ScriptResult, error) {
			return s.containerService.RunScript(ctx, containerID, req)
		}, nil
	}

	if !s.hostShellService.Enabled(ctx) {
		return nil, common.Classify(common.ErrUnavailable, errors.New("host shell is disabled; enable it in this environment's security settings before running snippets"))
	}
	return s.hostShellService.RunScript, nil
}

// RunSnippet executes a snippet once, synchronously, either from a manual
// API call (triggerSource "manual") or a scheduled fire (triggerSource
// "schedule"). Guards run in this order: target unavailable (503/404),
// resolve parameters (400), then the run itself — a request that fails
// either guard never reaches RunScript and never creates a run row, matching
// the "every invocation that reaches the exec step" rule.
func (s *SnippetService) RunSnippet(ctx context.Context, environmentID, id string, parameters map[string]string, triggerSource string, actor models.User) (*models.SnippetRun, error) {
	snippet, err := s.getSnippetRecordInternal(ctx, environmentID, id)
	if err != nil {
		return nil, err
	}

	runScript, err := s.resolveSnippetRunnerInternal(ctx, snippet)
	if err != nil {
		return nil, err
	}

	resolved, err := resolveSnippetParamsInternal(snippet.Parameters, parameters)
	if err != nil {
		return nil, err
	}

	timeoutSec := snippet.TimeoutSec
	if timeoutSec <= 0 {
		timeoutSec = snippetDefaultTimeoutSec
	}

	startedAt := time.Now()
	result, runErr := runScript(ctx, ScriptRequest{
		Script:         snippet.Script,
		Env:            resolved,
		WorkingDir:     stringOrEmptyInternal(snippet.WorkingDir),
		Timeout:        time.Duration(timeoutSec) * time.Second,
		MaxOutputBytes: snippetMaxOutputBytes,
	})
	durationMs := time.Since(startedAt).Milliseconds()

	status, exitCode, runErrMsg := snippetRunOutcomeInternal(result, runErr)

	run := &models.SnippetRun{
		SnippetID:     snippet.ID,
		EnvironmentID: snippet.EnvironmentID,
		TriggerSource: triggerSource,
		Status:        status,
		ExitCode:      exitCode,
		Parameters:    resolved,
		Output:        nullableTrimmedStringInternal(&result.Output),
		Error:         runErrMsg,
		StartedAt:     startedAt,
		DurationMs:    durationMs,
	}
	if actor.ID != "" {
		run.StartedByUserID = new(actor.ID)
		run.StartedByUsername = new(actor.Username)
	}

	persistCtx := context.WithoutCancel(ctx)
	if err := s.persistRunInternal(persistCtx, run); err != nil {
		slog.ErrorContext(ctx, "failed to persist snippet run", "snippetId", snippet.ID, "error", err)
	}

	s.emitRunEventInternal(persistCtx, snippet, run, actor)

	return run, nil
}

// snippetRunOutcomeInternal maps a HostShellService.RunScript result to the
// run row's status/exitCode/error fields. runErr is non-nil only for
// infrastructure failures or a timeout — a non-zero script exit is reported
// through result.ExitCode with a nil runErr.
func snippetRunOutcomeInternal(result ScriptResult, runErr error) (status string, exitCode *int64, errMsg *string) {
	if runErr != nil {
		if errors.Is(runErr, context.DeadlineExceeded) {
			return "timeout", nil, new(runErr.Error())
		}
		return "failed", nil, new(runErr.Error())
	}
	code := result.ExitCode
	if code != 0 {
		return "failed", &code, nil
	}
	return "success", &code, nil
}

// persistRunInternal writes the run row and denormalizes last-run state onto
// the parent snippet in one transaction, then prunes older runs beyond
// snippetRunHistoryLimit. ctx should already be context.WithoutCancel so a
// client disconnect cannot lose a completed run.
func (s *SnippetService) persistRunInternal(ctx context.Context, run *models.SnippetRun) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(run).Error; err != nil {
			return errors.WrapIf(err, "failed to create snippet run")
		}

		if err := tx.Model(&models.Snippet{}).Where("id = ?", run.SnippetID).Updates(map[string]any{
			"last_run_at":     run.StartedAt,
			"last_run_status": run.Status,
		}).Error; err != nil {
			return errors.WrapIf(err, "failed to update snippet last-run state")
		}

		return pruneSnippetRunsInternal(tx, run.SnippetID)
	})
}

// pruneSnippetRunsInternal keeps only the newest snippetRunHistoryLimit runs
// for a snippet. Fixed cap, no setting, no cron job — see the package doc.
func pruneSnippetRunsInternal(tx *gorm.DB, snippetID string) error {
	return tx.Where("snippet_id = ? AND id NOT IN (?)", snippetID,
		tx.Model(&models.SnippetRun{}).
			Select("id").
			Where("snippet_id = ?", snippetID).
			Order("started_at DESC").
			Limit(snippetRunHistoryLimit),
	).Delete(&models.SnippetRun{}).Error
}

func (s *SnippetService) emitRunEventInternal(ctx context.Context, snippet *models.Snippet, run *models.SnippetRun, actor models.User) {
	severity := models.EventSeveritySuccess
	if run.Status != "success" {
		severity = models.EventSeverityWarning
	}

	metadata := models.JSON{
		"snippetId":     snippet.ID,
		"runId":         run.ID,
		"triggerSource": run.TriggerSource,
		"status":        run.Status,
		"durationMs":    run.DurationMs,
	}
	if run.ExitCode != nil {
		metadata["exitCode"] = *run.ExitCode
	}

	_, err := s.eventService.CreateEvent(ctx, CreateEventRequest{
		Type:          models.EventTypeSnippetExecute,
		Severity:      severity,
		Title:         "Snippet run: " + snippet.Name,
		Description:   "Snippet '" + snippet.Name + "' finished with status " + run.Status,
		ResourceType:  new("snippet"),
		ResourceID:    new(snippet.ID),
		ResourceName:  new(snippet.Name),
		UserID:        nullableTrimmedStringInternal(&actor.ID),
		Username:      nullableTrimmedStringInternal(&actor.Username),
		EnvironmentID: new(snippet.EnvironmentID),
		Metadata:      metadata,
	})
	if err != nil {
		slog.WarnContext(ctx, "failed to emit snippet.execute event", "snippetId", snippet.ID, "runId", run.ID, "error", err)
	}
}

func (s *SnippetService) GetSnippetRunsPaginated(ctx context.Context, environmentID, snippetID string, params pagination.QueryParams) ([]snippettypes.SnippetRun, pagination.Response, error) {
	if _, err := s.getSnippetRecordInternal(ctx, environmentID, snippetID); err != nil {
		return nil, pagination.Response{}, err
	}

	var runs []models.SnippetRun
	q := s.db.WithContext(ctx).Model(&models.SnippetRun{}).Where("snippet_id = ?", snippetID)

	paginationResp, err := pagination.PaginateAndSortDB(params, q, &runs)
	if err != nil {
		return nil, pagination.Response{}, errors.WrapIf(err, "failed to paginate snippet runs")
	}

	out, mapErr := mapper.MapSlice[models.SnippetRun, snippettypes.SnippetRun](runs)
	if mapErr != nil {
		return nil, pagination.Response{}, errors.WrapIf(mapErr, "failed to map snippet runs")
	}
	return out, paginationResp, nil
}

func stringOrEmptyInternal(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// normalizeSnippetParametersInternal and normalizeSnippetScheduleParametersInternal
// turn a nil slice/map into an empty, non-nil one before it reaches either
// GORM's struct-based serializer path (Create) or jsonColumnValueInternal
// (Update's map path). A nil value marshals to the JSON literal "null",
// which the "parameters"/"schedule_parameters" columns (TEXT NOT NULL
// DEFAULT '[]'/'{}') must never receive.
func normalizeSnippetParametersInternal(defs []snippettypes.ParameterDef) []snippettypes.ParameterDef {
	if defs == nil {
		return []snippettypes.ParameterDef{}
	}
	return defs
}

func normalizeSnippetScheduleParametersInternal(params map[string]string) map[string]string {
	if params == nil {
		return map[string]string{}
	}
	return params
}

// jsonColumnValueInternal encodes a value the same way GORM's built-in
// "json" serializer would (encoding/json.Marshal, stored as a plain string),
// for use with the map[string]any path of .Updates() — which, unlike the
// struct-based Create path, does NOT run values through the schema's
// serializer. A raw Go slice/map handed to Updates() would otherwise be
// expanded by GORM as a SQL "(?,?,...)" value list instead of a single JSON
// column value.
func jsonColumnValueInternal(v any) (string, error) {
	encoded, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}
