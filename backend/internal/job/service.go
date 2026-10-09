package job

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sort"
	"strings"
	"sync"
	"time"
	"uuid"

	"github.com/danielgtaylor/huma/v2"
	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	"github.com/getarcaneapp/arcane/types/v2/jobschedule"
	"github.com/getarcaneapp/arcane/types/v2/meta"
	"github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/robfig/cron/v3"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/job/children/remote"
	"github.com/getarcaneapp/arcane/backend/v2/internal/job/children/runtime"
	"github.com/getarcaneapp/arcane/backend/v2/internal/kv"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/role"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/jobcontext"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/runs"
	scheduleutil "github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/schedule"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
)

// JobService manages configuration for background job schedules.
//
// Intervals are persisted in the existing settings table as individual keys.
// After updates, the settings.SettingsService cache is reloaded and active jobs are
// rescheduled through the configured scheduler.
//
// NOTE: This is intentionally separate from settings.SettingsService to keep the API
// surface job-focused and to centralize schedule validation/rescheduling.
type JobService struct {
	activity     *activity.ActivityService
	activityMu   sync.Mutex
	runs         *runs.Coordinator
	remote       *remote.Service
	environment  *environment.EnvironmentService
	roles        *role.RoleService
	db           *database.DB
	settings     *settings.SettingsService
	cfg          *config.Config
	scheduler    scheduler.JobController
	lifecycleCtx context.Context
	location     *time.Location // Timezone for cron schedule calculations

	// environment-health is no longer a single scheduler job — it fans out to one
	// dynamic job per environment owned by environment.EnvironmentService. These bridge the Jobs
	// UI (which addresses jobs by ID) back to that service. Set during bootstrap on
	// the manager only.
	OnEnvironmentHealthReschedule func(ctx context.Context)
	RunEnvironmentHealthNow       func(ctx context.Context) error
}

func NewJobService(
	db *database.DB,
	localSettings *settings.SettingsService,
	cfg *config.Config,
	coordinator *runs.Coordinator,
	roles *role.RoleService,
	localEnvironment *environment.EnvironmentService,
	localActivity *activity.ActivityService,
) *JobService {
	service := &JobService{
		roles:       roles,
		environment: localEnvironment,
		activity:    localActivity,
		remote:      remote.New(coordinator, kv.NewKVService(db), localEnvironment),
		runs:        coordinator,
		db:          db,
		settings:    localSettings,
		cfg:         cfg,
		location:    cfg.GetLocation(),
	}

	if coordinator != nil {
		coordinator.SetExecutor(service.executeRunInternal, service.reconcileRunInternal)
		if localActivity != nil {
			coordinator.SetObserver(service)
		}
	}
	return service
}

func (s *JobService) SetScheduler(ctx context.Context, controller scheduler.JobController) { //nolint:contextcheck // scheduler jobs must capture the app lifecycle context, not request contexts
	if ctx == nil {
		ctx = context.Background() //nolint:forbidigo // A nil scheduler context uses an independent lifecycle root.
	}
	s.lifecycleCtx = ctx
	s.scheduler = controller
}

func (s *JobService) GetJobSchedules(ctx context.Context) jobschedule.Config {
	defaults := settings.DefaultSettingsConfig()

	// Use settings.SettingsService cache for fast reads.
	return jobschedule.Config{
		EnvironmentHealthInterval:      s.settings.GetStringSetting(ctx, "environmentHealthInterval", defaults.EnvironmentHealthInterval.Value),
		EventCleanupInterval:           s.settings.GetStringSetting(ctx, "eventCleanupInterval", defaults.EventCleanupInterval.Value),
		ExpiredSessionsCleanupInterval: s.settings.GetStringSetting(ctx, "expiredSessionsCleanupInterval", defaults.ExpiredSessionsCleanupInterval.Value),
		AutoUpdateInterval:             s.settings.GetStringSetting(ctx, "autoUpdateInterval", defaults.AutoUpdateInterval.Value),
		DockerClientRefreshInterval:    s.settings.GetStringSetting(ctx, "dockerClientRefreshInterval", defaults.DockerClientRefreshInterval.Value),
		PollingInterval:                s.settings.GetStringSetting(ctx, "pollingInterval", defaults.PollingInterval.Value),
		ScheduledPruneInterval:         s.settings.GetStringSetting(ctx, "scheduledPruneInterval", defaults.ScheduledPruneInterval.Value),
		VulnerabilityScanInterval:      s.settings.GetStringSetting(ctx, "vulnerabilityScanInterval", defaults.VulnerabilityScanInterval.Value),
		ImageAutoPatchInterval:         s.settings.GetStringSetting(ctx, "imageAutoPatchInterval", defaults.ImageAutoPatchInterval.Value),
		AutoHealInterval:               s.settings.GetStringSetting(ctx, "autoHealInterval", defaults.AutoHealInterval.Value),
	}
}

func (s *JobService) UpdateJobSchedules(ctx context.Context, updates jobschedule.Update) (jobschedule.Config, error) {
	if s == nil || s.db == nil || s.settings == nil {
		return jobschedule.Config{}, errors.New("job service not initialized")
	}
	if s.cfg != nil && s.cfg.UIConfigurationDisabled {
		return jobschedule.Config{}, errors.New("job schedule updates are disabled")
	}

	current := s.GetJobSchedules(ctx)

	fields := []struct {
		key     string
		current string
		update  *string
	}{
		{key: "environmentHealthInterval", current: current.EnvironmentHealthInterval, update: updates.EnvironmentHealthInterval},
		{key: "eventCleanupInterval", current: current.EventCleanupInterval, update: updates.EventCleanupInterval},
		{key: "expiredSessionsCleanupInterval", current: current.ExpiredSessionsCleanupInterval, update: updates.ExpiredSessionsCleanupInterval},
		{key: "autoUpdateInterval", current: current.AutoUpdateInterval, update: updates.AutoUpdateInterval},
		{key: "dockerClientRefreshInterval", current: current.DockerClientRefreshInterval, update: updates.DockerClientRefreshInterval},
		{key: "pollingInterval", current: current.PollingInterval, update: updates.PollingInterval},
		{key: "scheduledPruneInterval", current: current.ScheduledPruneInterval, update: updates.ScheduledPruneInterval},
		{key: "vulnerabilityScanInterval", current: current.VulnerabilityScanInterval, update: updates.VulnerabilityScanInterval},
		{key: "imageAutoPatchInterval", current: current.ImageAutoPatchInterval, update: updates.ImageAutoPatchInterval},
		{key: "autoHealInterval", current: current.AutoHealInterval, update: updates.AutoHealInterval},
	}

	// Validate inputs (cron expressions)
	parser := scheduleutil.Parser()
	for _, field := range fields {
		if field.update == nil || *field.update == "" {
			continue
		}
		if _, err := parser.Parse(*field.update); err != nil {
			return jobschedule.Config{}, fmt.Errorf("invalid cron expression for %s: %w", field.key, err)
		}
	}

	changedKeys := make([]string, 0, len(fields))
	previousValues := make(map[string]string, len(fields))
	valuesToUpdate := make([]libarcane.SettingUpdate, 0, len(fields))
	for _, field := range fields {
		key := field.key
		value := field.update
		currentValue := field.current
		if value == nil || *value == currentValue {
			continue
		}
		changedKeys = append(changedKeys, key)
		previousValues[key] = currentValue
		valuesToUpdate = append(valuesToUpdate, libarcane.SettingUpdate{Key: key, Value: *value})
	}

	if len(valuesToUpdate) == 0 {
		return s.GetJobSchedules(ctx), nil
	}
	if err := s.settings.UpdateSettingValues(ctx, valuesToUpdate); err != nil {
		return jobschedule.Config{}, fmt.Errorf("failed to update job schedules: %w", err)
	}

	if err := s.RescheduleJobsForSettingKeys(ctx, changedKeys); err != nil {
		restoreErr := s.restoreJobSchedulesInternal(ctx, previousValues, changedKeys)
		return jobschedule.Config{}, errors.Join(fmt.Errorf("failed to apply job schedule update: %w", err), restoreErr)
	}

	return s.GetJobSchedules(ctx), nil
}

func (s *JobService) RescheduleJobsForSettingKeys(ctx context.Context, changedKeys []string) error {
	if len(changedKeys) == 0 {
		return nil
	}
	if s == nil || s.scheduler == nil {
		return errors.New("job scheduler not initialized")
	}

	changed := make(map[string]struct{}, len(changedKeys))
	for _, key := range changedKeys {
		changed[key] = struct{}{}
	}

	var rescheduleErrors []error
	for jobID, jobMeta := range meta.GetAllJobMetadata() {
		if !jobMetadataAffectedBySettingInternal(jobMeta, changed) {
			continue
		}
		if s.cfg != nil && s.cfg.AgentMode && jobMeta.ManagerOnly {
			slog.DebugContext(ctx, "Skipping manager-only job reschedule in agent mode", "job", jobID)
			continue
		}
		if err := s.rescheduleAffectedJobInternal(ctx, jobID, jobMeta); err != nil {
			rescheduleErrors = append(rescheduleErrors, err)
		}
	}

	return errors.Join(rescheduleErrors...)
}

// jobRescheduleContextInternal prefers the app lifecycle context so cron jobs
// and watchers outlive the HTTP request that triggered the reschedule.
func (s *JobService) jobRescheduleContextInternal(ctx context.Context) context.Context {
	if s.lifecycleCtx != nil {
		return s.lifecycleCtx
	}
	return ctx
}

// rescheduleAffectedJobInternal applies a settings change to one job:
// delegated fan-out jobs and continuous bus watchers are notified, cron jobs
// are rescheduled and their runtime schedule verified.
func (s *JobService) rescheduleAffectedJobInternal(ctx context.Context, jobID string, jobMeta meta.JobMetadata) error {
	// environment-health fans out to per-environment dynamic jobs; delegate the
	// reschedule to environment.EnvironmentService instead of looking up a single job.
	if jobID == "environment-health" {
		if s.OnEnvironmentHealthReschedule == nil {
			return errors.New("environment-health rescheduler not initialized")
		}
		s.OnEnvironmentHealthReschedule(s.jobRescheduleContextInternal(ctx))
		return nil
	}

	// Continuous bus watchers have no cron entry to reschedule; notify the
	// watcher so it re-reads its poll schedule instead.
	if jobMeta.IsContinuous {
		if jobID == "image-polling" && s.settings != nil {
			return s.settings.NotifySettingsChanges(s.jobRescheduleContextInternal(ctx), "pollingInterval")
		}
		return nil
	}

	job, ok := s.scheduler.GetJob(jobID)
	if !ok {
		return fmt.Errorf("job %s not found in scheduler", jobID)
	}

	slog.DebugContext(ctx, "Processing job setting change", "job", jobID, "settingsKey", jobMeta.SettingsKey, "enabledKey", jobMeta.EnabledKey)
	rescheduleCtx := s.jobRescheduleContextInternal(ctx)
	if err := s.scheduler.RescheduleJob(rescheduleCtx, job); err != nil {
		return fmt.Errorf("reschedule job %s: %w", jobID, err)
	}

	runtimeState, ok := s.scheduler.GetJobRuntimeState(jobID)
	if !ok {
		return fmt.Errorf("job %s has no runtime scheduler state", jobID)
	}

	expectedSchedule := job.Schedule(rescheduleCtx)
	if conditional, isConditional := job.(scheduler.ConditionalJob); isConditional && !conditional.ShouldSchedule(rescheduleCtx) {
		expectedSchedule = ""
	}
	if runtimeState.Schedule != expectedSchedule {
		return fmt.Errorf("job %s runtime schedule %q does not match requested schedule %q", jobID, runtimeState.Schedule, expectedSchedule)
	}
	return nil
}

func (s *JobService) restoreJobSchedulesInternal(ctx context.Context, previousValues map[string]string, changedKeys []string) error {
	if len(previousValues) == 0 {
		return nil
	}

	updates := make([]libarcane.SettingUpdate, 0, len(previousValues))
	for key, value := range previousValues {
		updates = append(updates, libarcane.SettingUpdate{Key: key, Value: value})
	}
	if err := s.settings.UpdateSettingValues(ctx, updates); err != nil {
		return fmt.Errorf("failed to restore previous job schedules: %w", err)
	}

	if err := s.RescheduleJobsForSettingKeys(ctx, changedKeys); err != nil {
		return fmt.Errorf("failed to restore runtime job schedules: %w", err)
	}

	return nil
}

func jobMetadataAffectedBySettingInternal(jobMeta meta.JobMetadata, changed map[string]struct{}) bool {
	if jobMeta.SettingsKey != "" {
		if _, ok := changed[jobMeta.SettingsKey]; ok {
			return true
		}
	}
	if jobMeta.EnabledKey != "" {
		if _, ok := changed[jobMeta.EnabledKey]; ok {
			return true
		}
	}
	return false
}

func (s *JobService) ListJobs(ctx context.Context) (*jobschedule.JobListResponse, error) {
	if s == nil || s.settings == nil {
		return nil, errors.New("job service not initialized")
	}

	allMetadata := meta.GetAllJobMetadata()
	jobs := make([]jobschedule.JobStatus, 0, len(allMetadata))

	for _, jobMeta := range allMetadata {
		schedule := s.getJobScheduleInternal(ctx, jobMeta)
		nextRun := s.calculateNextRunInternal(schedule)
		enabled := s.isJobEnabledInternal(ctx, jobMeta)
		prerequisites := s.evaluatePrerequisitesInternal(ctx, jobMeta)

		if s.scheduler != nil && (jobMeta.SettingsKey != "" || jobMeta.ID == "upgrade-log-cleanup") && jobMeta.ID != "environment-health" {
			if runtimeState, ok := s.scheduler.GetJobRuntimeState(jobMeta.ID); ok {
				schedule = cmp.Or(runtimeState.Schedule, schedule)
				nextRun = runtimeState.NextRun
			}
		}

		jobStatus := jobMeta.ToJobStatus(schedule, nextRun, enabled, prerequisites)
		jobs = append(jobs, jobStatus)
	}

	if err := runtime.ApplyStatuses(ctx, s.runs, s.scheduler, "0", &jobs); err != nil {
		return nil, err
	}

	// Sort jobs by ID to ensure stable UI order
	sort.Slice(jobs, func(i, j int) bool {
		return jobs[i].ID < jobs[j].ID
	})

	isAgent := s.cfg != nil && s.cfg.AgentMode

	return &jobschedule.JobListResponse{
		Jobs:        jobs,
		IsAgent:     isAgent,
		DurableRuns: true, ObservedAt: time.Now().UTC(),
	}, nil
}

func (s *JobService) getJobScheduleInternal(ctx context.Context, localMeta meta.JobMetadata) string {
	// Continuous jobs with a settings key (image-polling) are event-driven but
	// also poll on that cron schedule, so surface the real expression.
	if localMeta.IsContinuous && localMeta.SettingsKey == "" {
		return "continuous"
	}

	if localMeta.ID == "analytics-heartbeat" {
		return "automatic (checked hourly; sent once per 24h)"
	}

	if localMeta.SettingsKey == "" {
		return ""
	}

	defaultSchedule, _, _, err := settings.DefaultSettingsConfig().FieldByKey(localMeta.SettingsKey)
	if err != nil || defaultSchedule == "" {
		defaultSchedule = "0 0 0 * * *"
	}

	return s.settings.GetStringSetting(ctx, localMeta.SettingsKey, defaultSchedule)
}

func (s *JobService) isJobEnabledInternal(ctx context.Context, localMeta meta.JobMetadata) bool {
	if localMeta.EnabledKey != "" {
		return s.settings.GetBoolSetting(ctx, localMeta.EnabledKey, false)
	}
	if localMeta.IsContinuous {
		return true
	}

	return true
}

func (s *JobService) evaluatePrerequisitesInternal(ctx context.Context, localMeta meta.JobMetadata) []jobschedule.JobPrerequisite {
	prerequisites := make([]jobschedule.JobPrerequisite, 0, len(localMeta.Prerequisites))

	for _, prereq := range localMeta.Prerequisites {
		isMet := s.settings.GetBoolSetting(ctx, prereq.SettingKey, false)

		prerequisites = append(prerequisites, jobschedule.JobPrerequisite{
			SettingKey:  prereq.SettingKey,
			Label:       prereq.Label,
			IsMet:       isMet,
			SettingsURL: prereq.SettingsURL,
		})
	}

	return prerequisites
}

func (s *JobService) calculateNextRunInternal(schedule string) *time.Time {
	if schedule == "" || schedule == "continuous" {
		return nil
	}

	// Parse schedule and force it to use the same timezone as the scheduler.
	parser := scheduleutil.Parser()
	sched, err := parser.Parse(schedule)
	if err != nil {
		return nil
	}

	location := time.UTC
	if s != nil && s.location != nil {
		location = s.location
	}

	if specSchedule, ok := sched.(*cron.SpecSchedule); ok {
		specSchedule.Location = location
	}

	// Calculate next run using the configured timezone.
	now := time.Now().In(location)
	return new(sched.Next(now))
}

func (s *JobService) Coordinator() *runs.Coordinator { return s.runs }

// ActivityID assigns a summary identity before a visible run is persisted.
func (s *JobService) ActivityID(run scheduler.Run) string {
	// A remotely delivered run already has a summary on its accepting manager.
	if run.Trigger == "remote" {
		return ""
	}
	visible := run.Trigger == "manual"
	switch run.JobID {
	case "auto-update", "auto-patch", "auto-heal", "scheduled-prune", "vulnerability-scan":
		visible = true
	default:
		visible = visible || strings.HasPrefix(run.JobID, "gitops-sync:") || strings.HasPrefix(run.JobID, "volume-backup:") || strings.HasPrefix(run.JobID, "system-backup:")
	}
	if !visible {
		return ""
	}
	return uuid.New().String()
}

// SyncRunActivity projects the latest committed run without changing execution state.
func (s *JobService) SyncRunActivity(ctx context.Context, run scheduler.Run) error {
	if s.activity == nil {
		return nil
	}
	s.activityMu.Lock()
	defer s.activityMu.Unlock()
	current, err := s.runs.Get(ctx, run.EnvironmentID, run.JobID, run.ID)
	if errors.Is(err, runs.ErrRunNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if current.ActivityID == "" {
		return nil
	}
	name := current.JobID
	if metadata, found := meta.GetJobMetadata(current.JobID); found {
		name = metadata.Name
	}
	return s.activity.SyncJobRun(ctx, runtime.ProjectRunOutcome(current), name)
}

func (s *JobService) ReconcileStartupActivities(ctx context.Context, extraProtectedIDs ...string) error {
	if s.activity == nil {
		return nil
	}
	records, err := s.runs.Records(ctx)
	if err != nil {
		return err
	}
	protected := append([]string(nil), extraProtectedIDs...)
	for _, record := range records {
		for _, run := range record.Runs {
			if run.Status.Terminal() {
				continue
			}
			if run.ActivityID != "" {
				protected = append(protected, run.ActivityID)
			}
			if run.Outcome.ActivityID != "" {
				protected = append(protected, run.Outcome.ActivityID)
			}
			for _, target := range run.Outcome.Targets {
				if target.ActivityID != "" {
					protected = append(protected, target.ActivityID)
				}
			}
		}
	}
	if failInterruptedBackupsErr := s.activity.FailInterruptedBackups(ctx, protected...); failInterruptedBackupsErr != nil {
		return failInterruptedBackupsErr
	}
	if _, failStaleImageUpdateChecksErr := s.activity.FailStaleImageUpdateChecks(ctx); failStaleImageUpdateChecksErr != nil {
		return failStaleImageUpdateChecksErr
	}
	if _, resolveStaleAutoUpdateActivitiesErr := s.activity.ResolveStaleAutoUpdateActivities(ctx, protected...); resolveStaleAutoUpdateActivitiesErr != nil {
		return resolveStaleAutoUpdateActivitiesErr
	}
	_, err = s.activity.ResolveOrphanedQueuedActivities(ctx, protected...)
	return err
}

// Submit validates job eligibility and persists acceptance before execution.
// Managers may accept requests for offline environments. Reusing a run ID
// deduplicates admission within the same job and environment.
func (s *JobService) Submit(ctx context.Context, request scheduler.Request) (scheduler.Run, error) {
	request.EnvironmentID = cmp.Or(request.EnvironmentID, "0")
	if request.EnvironmentID != "0" {
		if s.cfg.AgentMode {
			return scheduler.Run{}, errors.New("agents cannot queue work for another environment")
		}
		env, err := s.environment.GetEnvironmentByID(ctx, request.EnvironmentID)
		if err != nil {
			return scheduler.Run{}, err
		}
		if !env.Enabled {
			return scheduler.Run{}, errors.New("environment is disabled")
		}
		metadata, ok := meta.GetJobMetadata(request.JobID)
		dynamicRemote := strings.HasPrefix(request.JobID, "gitops-sync:") || strings.HasPrefix(request.JobID, "volume-backup:")
		if !dynamicRemote && (!ok || metadata.ManagerOnly || !metadata.CanRunManually) {
			return scheduler.Run{}, errors.New("job is not remotely runnable")
		}
	} else if err := s.validateLocalJobInternal(ctx, request.JobID); err != nil {
		return scheduler.Run{}, err
	}
	if err := s.authorizeRunInternal(
		ctx,
		scheduler.Run{
			Trigger:          request.Trigger,
			RequestedBy:      request.RequestedBy,
			RequestedWithKey: request.RequestedWithKey,
			EnvironmentID:    request.EnvironmentID,
		},
	); err != nil {
		return scheduler.Run{}, err
	}
	return s.runs.Submit(ctx, request)
}

func (s *JobService) validateLocalJobInternal(ctx context.Context, jobID string) error {
	if metadata, ok := meta.GetJobMetadata(jobID); ok {
		if !metadata.CanRunManually {
			return errors.New("job cannot be run manually")
		}
		if s.cfg.AgentMode && metadata.ManagerOnly {
			return errors.New("job is manager-only")
		}
		if !s.isJobEnabledInternal(ctx, metadata) {
			return errors.New("job is disabled")
		}
		for _, prereq := range s.evaluatePrerequisitesInternal(ctx, metadata) {
			if !prereq.IsMet {
				return errors.New("job prerequisites are not met")
			}
		}
		if metadata.IsContinuous || jobID == "environment-health" {
			return nil
		}
	}
	if s.scheduler == nil {
		return errors.New("scheduler unavailable")
	}
	job, ok := s.scheduler.GetJob(jobID)
	if !ok {
		return errors.New("job is not registered")
	}
	if conditional, localOk := job.(scheduler.ConditionalJob); localOk && !conditional.ShouldSchedule(ctx) {
		return errors.New("job is disabled")
	}
	return nil
}

func (s *JobService) authorizeRunInternal(ctx context.Context, run scheduler.Run) error {
	if run.RequestedBy == "" {
		if run.Trigger == "manual" && !s.cfg.AgentMode {
			return errors.New("requesting user unavailable")
		}
		return nil
	}
	if s.roles == nil {
		return errors.New("permission resolver unavailable")
	}
	permissions, err := s.roles.ResolveExecutionPermissions(ctx, run.RequestedBy, run.RequestedWithKey)
	if err != nil {
		return err
	}
	if !permissions.Allows(authz.PermJobsManage, run.EnvironmentID) {
		return errors.New("requesting user no longer has permission to manage jobs")
	}
	return nil
}

func (s *JobService) executeRunInternal(ctx context.Context, run scheduler.Run) (scheduler.Outcome, error) {
	if err := s.authorizeRunInternal(ctx, run); err != nil {
		return scheduler.Outcome{Status: scheduler.Failed}, err
	}
	if run.EnvironmentID != "0" {
		return s.remote.Deliver(ctx, run)
	}
	if requiresDockerInternal(run.JobID) && s.environment != nil {
		status, err := s.environment.TestConnection(ctx, "0", nil)
		if err != nil || status != "online" {
			return scheduler.Outcome{Status: scheduler.Waiting, Message: "Waiting for Docker"}, err
		}
	}
	ctx = s.runContextInternal(ctx, run)
	if run.JobID == "environment-health" && s.RunEnvironmentHealthNow != nil {
		err := s.RunEnvironmentHealthNow(ctx)
		return classifyOutcomeInternal(run.JobID, scheduler.Outcome{}, err)
	}
	if metadata, ok := meta.GetJobMetadata(run.JobID); ok && metadata.IsContinuous {
		if err := s.validateLocalJobInternal(ctx, run.JobID); err != nil {
			return scheduler.Outcome{Status: scheduler.Canceled}, err
		}
		err := s.scheduler.RunBusWatcherNow(ctx, run.JobID)
		return classifyOutcomeInternal(run.JobID, scheduler.Outcome{}, err)
	}
	unavailableStatus := kit.Ternary(run.JobID == "auto-update" && run.AttemptCount > 1, scheduler.Failed, scheduler.Canceled)
	job, ok := s.scheduler.GetJob(run.JobID)
	if !ok {
		return scheduler.Outcome{Status: unavailableStatus, Message: "Job or target no longer exists"}, nil
	}
	if conditional, localOk := job.(scheduler.ConditionalJob); localOk && !conditional.ShouldSchedule(ctx) {
		return scheduler.Outcome{Status: unavailableStatus, Message: "Job is disabled"}, nil
	}
	outcome, err := job.Run(ctx)
	return classifyOutcomeInternal(run.JobID, outcome, err)
}

func classifyOutcomeInternal(jobID string, outcome scheduler.Outcome, err error) (scheduler.Outcome, error) {
	if resultErr, ok := errors.AsType[*scheduler.OutcomeError](err); ok {
		outcome = resultErr.Outcome
	}
	switch outcome.Status {
	case scheduler.NeedsAttention:
		outcome.Status = scheduler.Failed
		return outcome, err
	case scheduler.Waiting, scheduler.Retrying, scheduler.Failed, scheduler.Partial, scheduler.Canceled, scheduler.Skipped:
		return outcome, err
	case scheduler.Queued, scheduler.Running, scheduler.Succeeded:
		// Classify these and unspecified outcomes below.
	}

	var networkErr net.Error
	switch {
	case err == nil:
		outcome.Status = cmp.Or(outcome.Status, scheduler.Succeeded)
	case safeJobInternal(jobID) && (errors.As(err, &networkErr) || errors.Is(err, context.DeadlineExceeded)):
		outcome.Status = scheduler.Retrying
	case outcome.Status == "" || outcome.Status == scheduler.Succeeded:
		outcome.Status = scheduler.Failed
	}

	return outcome, err
}

func safeJobInternal(jobID string) bool {
	switch jobID {
	case "image-polling", "environment-health", "docker-client-refresh", "event-cleanup", "expired-sessions-cleanup", "activity-sweep", "upload-sessions-cleanup",
		"git-clone-scratch-cleanup", "analytics-heartbeat", "apns-outbox", "vulnerability-scan":
		return true
	}
	return strings.HasPrefix(jobID, "environment-health:")
}

func (s *JobService) reconcileRunInternal(ctx context.Context, run scheduler.Run) (scheduler.Outcome, error) {
	if err := s.authorizeRunInternal(ctx, run); err != nil {
		return scheduler.Outcome{Status: scheduler.Failed}, err
	}
	if run.EnvironmentID != "0" {
		return s.executeRunInternal(ctx, run)
	}
	if safeJobInternal(run.JobID) {
		return s.executeRunInternal(ctx, run)
	}
	if s.scheduler != nil {
		if job, ok := s.scheduler.GetJob(run.JobID); ok {
			if reconciler, localOk := job.(scheduler.Reconciler); localOk {
				return reconciler.Reconcile(s.runContextInternal(ctx, run), run)
			}
		}
	}
	// A successful activity proves only that target, never the entire batch.
	for index, target := range run.Outcome.Targets {
		if run.JobID == "auto-update" {
			break
		}
		if target.Status == scheduler.Succeeded || target.Status == scheduler.Skipped {
			continue
		}
		if target.ActivityID == "" {
			return scheduler.Outcome{Status: scheduler.Failed, Message: "Interrupted operation has no confirmed outcome", Targets: run.Outcome.Targets}, nil
		}
		var record activity.Activity
		if err := s.db.WithContext(ctx).First(&record, "id = ?", target.ActivityID).Error; err != nil {
			return scheduler.Outcome{Status: scheduler.Failed}, err
		}
		if record.Status != activitytypes.StatusSuccess {
			return scheduler.Outcome{Status: scheduler.Failed, Message: "Interrupted operation has no confirmed completion", Targets: run.Outcome.Targets}, nil
		}
		run.Outcome.Targets[index].Status = scheduler.Succeeded
	}
	if job, ok := s.scheduler.GetJob(run.JobID); ok {
		if reconciler, localOk2 := job.(scheduler.Reconciler); localOk2 {
			outcome, err := reconciler.Reconcile(s.runContextInternal(ctx, run), run)
			if err != nil {
				slog.WarnContext(ctx, "Job reconciliation requires attention", "runId", run.ID, "error", err)
			}
			return outcome, err
		}
	}
	return scheduler.Outcome{Status: scheduler.Failed, Message: "Interrupted operation has no confirmed completion", Targets: run.Outcome.Targets}, nil
}

func (s *JobService) runContextInternal(ctx context.Context, run scheduler.Run) context.Context {
	ctx = utils.WithActivityBatchID(ctx, run.ID)
	ctx = jobcontext.WithExecution(ctx, run, func(target scheduler.TargetOutcome) error {
		return s.runs.UpdateRun(ctx, run, func(current *scheduler.Run) error {
			if current.Status != scheduler.Running || current.Owner != run.Owner {
				return runs.ErrRunConflict
			}
			for index := range current.Outcome.Targets {
				if current.Outcome.Targets[index].ID == target.ID {
					if len(target.RecoveryData) == 0 {
						target.RecoveryData = current.Outcome.Targets[index].RecoveryData
					}
					current.Outcome.Targets[index] = target
					return nil
				}
			}
			current.Outcome.Targets = append(current.Outcome.Targets, target)
			return nil
		})
	})
	return ctx
}

func requiresDockerInternal(jobID string) bool {
	switch jobID {
	case "auto-update", "auto-patch", "auto-heal", "scheduled-prune", "vulnerability-scan", "image-polling":
		return true
	}
	return strings.HasPrefix(jobID, "gitops-sync:") || strings.HasPrefix(jobID, "volume-backup:") || strings.HasPrefix(jobID, "system-backup:")
}

// ResolveRun authorizes the current operator independently of the original requester.
// TODO(v3): remove this deprecated mixed-version compatibility contract.
func (s *JobService) ResolveRun(ctx context.Context, environmentID, jobID, runID, actor string) (scheduler.Run, error) {
	permissions, _ := middleware.PermissionsFromContext(ctx)
	if !permissions.Allows(authz.PermJobsManage, environmentID) {
		return scheduler.Run{}, huma.Error403Forbidden("permission denied: " + authz.PermJobsManage)
	}
	if actor == "" {
		return scheduler.Run{}, errors.New("resolving operator identity is required")
	}
	if environmentID != "0" {
		return s.remote.ResolveRun(ctx, environmentID, jobID, runID, actor)
	}
	return s.runs.Resolve(ctx, environmentID, jobID, runID, actor)
}

// RetryRun revalidates local job eligibility before requeuing the existing run.
// Remote retries follow the delivery protocol and retain confirmed target progress.
func (s *JobService) RetryRun(ctx context.Context, environmentID, jobID, runID string) (scheduler.Run, error) {
	if environmentID != "0" {
		return s.retryRemoteRunInternal(ctx, environmentID, jobID, runID)
	}
	if environmentID == "0" {
		if err := s.validateLocalJobInternal(ctx, jobID); err != nil {
			return scheduler.Run{}, err
		}
	}
	run, err := s.runs.Get(ctx, environmentID, jobID, runID)
	if err != nil {
		return scheduler.Run{}, err
	}
	if s.scheduler != nil {
		if job, ok := s.scheduler.GetJob(jobID); ok {
			if validator, localOk := job.(scheduler.RetryValidator); localOk {
				if validateRetryErr := validator.ValidateRetry(ctx, run); validateRetryErr != nil {
					return run, validateRetryErr
				}
			}
		}
	}
	return s.runs.Retry(ctx, environmentID, jobID, runID)
}

// ListRemoteJobs returns an agent's catalog with manager-side run status.
func (s *JobService) ListRemoteJobs(ctx context.Context, environmentID string) (*jobschedule.JobListResponse, error) {
	catalog, err := s.remote.Catalog(ctx, environmentID)
	if err != nil {
		return nil, err
	}
	if statusErr := runtime.ApplyStatuses(ctx, s.runs, s.scheduler, environmentID, &catalog.Jobs); statusErr != nil {
		return nil, statusErr
	}
	sort.Slice(catalog.Jobs, func(i, j int) bool { return catalog.Jobs[i].ID < catalog.Jobs[j].ID })
	return catalog, nil
}

// retryRemoteRunInternal queues one explicit retry while retaining the agent's target progress.
func (s *JobService) retryRemoteRunInternal(ctx context.Context, environmentID, jobID, runID string) (scheduler.Run, error) {
	run, err := s.runs.Get(ctx, environmentID, jobID, runID)
	if errors.Is(err, runs.ErrRunNotFound) {
		return s.remote.MutateAgentRun(ctx, environmentID, jobID, runID, "retry")
	}
	if err != nil {
		return run, err
	}
	if authorizeRunErr := s.authorizeRunInternal(ctx, run); authorizeRunErr != nil {
		return run, authorizeRunErr
	}
	return s.remote.RetryRun(ctx, run)
}

// ListRuns merges agent history with requests accepted by this manager.
func (s *JobService) ListRuns(ctx context.Context, environmentID, jobID string, page, limit int) (scheduler.RunList, error) {
	if environmentID == "0" {
		return s.runs.List(ctx, environmentID, jobID, page, limit)
	}
	merged, err := s.remote.Runs(ctx, environmentID, jobID)
	if err != nil {
		return scheduler.RunList{}, err
	}
	localRuns := make([]scheduler.Run, 0, len(merged))
	for _, run := range merged {
		localRuns = append(localRuns, runtime.ProjectRunOutcome(run))
	}
	sort.Slice(localRuns, func(i, j int) bool {
		if localRuns[i].CreatedAt.Equal(localRuns[j].CreatedAt) {
			return localRuns[i].ID < localRuns[j].ID
		}
		return localRuns[i].CreatedAt.After(localRuns[j].CreatedAt)
	})
	page = max(page, 1)
	limit = min(max(limit, 1), 100)
	start := len(localRuns)
	if page-1 <= len(localRuns)/limit {
		start = min((page-1)*limit, len(localRuns))
	}
	return scheduler.RunList{Runs: localRuns[start:min(start+limit, len(localRuns))], Total: len(localRuns), Page: page, Limit: limit}, nil
}

// GetRun prefers the manager's durable delivery record, then queries agent-owned history.
func (s *JobService) GetRun(ctx context.Context, environmentID, jobID, runID string) (scheduler.Run, error) {
	run, err := s.runs.Get(ctx, environmentID, jobID, runID)
	if environmentID == "0" || !errors.Is(err, runs.ErrRunNotFound) {
		return runtime.ProjectRunOutcome(run), err
	}
	run, err = s.remote.AgentRun(ctx, environmentID, jobID, runID)
	if err != nil {
		return scheduler.Run{}, err
	}
	return runtime.ProjectRunOutcome(run), nil
}

// CancelRun cancels manager admission or explicitly forwards an agent-owned cancellation.
func (s *JobService) CancelRun(ctx context.Context, environmentID, jobID, runID string) (scheduler.Run, error) {
	_, err := s.runs.Get(ctx, environmentID, jobID, runID)
	if environmentID != "0" && errors.Is(err, runs.ErrRunNotFound) {
		return s.remote.MutateAgentRun(ctx, environmentID, jobID, runID, "cancel")
	}
	if err != nil {
		return scheduler.Run{}, err
	}
	return s.runs.Cancel(ctx, environmentID, jobID, runID)
}
