package job

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"slices"
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
	"github.com/samber/mo"
	"go.getarcane.app/kit/pkg"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"

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
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/runs"
	scheduleutil "github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/schedule"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/tracing"
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
	runCount     metric.Int64Counter
	runDuration  metric.Float64Histogram
	activeRuns   metric.Int64UpDownCounter

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
	meter := otel.Meter(tracing.InstrumentationName)
	runCount, runCountErr := meter.Int64Counter("arcane.job.runs",
		metric.WithDescription("Executed job runs by outcome"), metric.WithUnit("{run}"))
	runDuration, runDurationErr := meter.Float64Histogram("arcane.job.duration",
		metric.WithDescription("Job run duration by outcome"), metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.01, 0.05, 0.1, 0.5, 1, 5, 10, 30, 60, 120, 300, 600, 1800, 3600))
	activeRuns, activeRunsErr := meter.Int64UpDownCounter("arcane.job.active",
		metric.WithDescription("Job runs currently executing"), metric.WithUnit("{run}"))
	if err := errors.Join(runCountErr, runDurationErr, activeRunsErr); err != nil {
		otel.Handle(err)
	}

	service := &JobService{
		activity:    localActivity,
		runs:        coordinator,
		remote:      remote.New(coordinator, kv.NewKVService(db), localEnvironment),
		environment: localEnvironment,
		roles:       roles,
		db:          db,
		settings:    localSettings,
		cfg:         cfg,
		location:    cfg.GetLocation(),
		runCount:    runCount,
		runDuration: runDuration,
		activeRuns:  activeRuns,
	}
	coordinator.SetExecutor(service.executeRun, service.reconcileRun)
	coordinator.SetObserver(service)
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
	if s.cfg.UIConfigurationDisabled {
		return jobschedule.Config{}, errors.New("job schedule updates are disabled")
	}

	current := s.GetJobSchedules(ctx)

	fields := []struct {
		key     string
		current string
		update  *string
	}{
		{
			key:     "environmentHealthInterval",
			current: current.EnvironmentHealthInterval,
			update:  updates.EnvironmentHealthInterval,
		},
		{
			key:     "eventCleanupInterval",
			current: current.EventCleanupInterval,
			update:  updates.EventCleanupInterval,
		},
		{
			key:     "expiredSessionsCleanupInterval",
			current: current.ExpiredSessionsCleanupInterval,
			update:  updates.ExpiredSessionsCleanupInterval,
		},
		{
			key:     "autoUpdateInterval",
			current: current.AutoUpdateInterval,
			update:  updates.AutoUpdateInterval,
		},
		{
			key:     "dockerClientRefreshInterval",
			current: current.DockerClientRefreshInterval,
			update:  updates.DockerClientRefreshInterval,
		},
		{
			key:     "pollingInterval",
			current: current.PollingInterval,
			update:  updates.PollingInterval,
		},
		{
			key:     "scheduledPruneInterval",
			current: current.ScheduledPruneInterval,
			update:  updates.ScheduledPruneInterval,
		},
		{
			key:     "vulnerabilityScanInterval",
			current: current.VulnerabilityScanInterval,
			update:  updates.VulnerabilityScanInterval,
		},
		{
			key:     "imageAutoPatchInterval",
			current: current.ImageAutoPatchInterval,
			update:  updates.ImageAutoPatchInterval,
		},
		{
			key:     "autoHealInterval",
			current: current.AutoHealInterval,
			update:  updates.AutoHealInterval,
		},
	}

	// Validate inputs (cron expressions)
	parser := scheduleutil.Parser()
	for _, field := range fields {
		if kit.FromPtr(field.update) == "" {
			continue
		}
		if _, err := parser.Parse(*field.update); err != nil {
			return jobschedule.Config{}, fmt.Errorf("invalid cron expression for %s: %w", field.key, err)
		}
	}

	changedKeys := make([]string, 0, len(fields))
	previousValues := make([]libarcane.SettingUpdate, 0, len(fields))
	valuesToUpdate := make([]libarcane.SettingUpdate, 0, len(fields))
	for _, field := range fields {
		if mo.PointerToOption(field.update).OrElse(field.current) == field.current {
			continue
		}
		changedKeys = append(changedKeys, field.key)
		previousValues = append(previousValues, libarcane.SettingUpdate{Key: field.key, Value: field.current})
		valuesToUpdate = append(valuesToUpdate, libarcane.SettingUpdate{Key: field.key, Value: *field.update})
	}

	if len(valuesToUpdate) == 0 {
		return s.GetJobSchedules(ctx), nil
	}
	if err := s.settings.UpdateSettingValues(ctx, valuesToUpdate); err != nil {
		return jobschedule.Config{}, fmt.Errorf("failed to update job schedules: %w", err)
	}

	if err := s.RescheduleJobsForSettingKeys(ctx, changedKeys); err != nil {
		return jobschedule.Config{}, errors.Join(fmt.Errorf("failed to apply job schedule update: %w", err), func() error {
			if restoreErr := s.settings.UpdateSettingValues(ctx, previousValues); restoreErr != nil {
				return fmt.Errorf("failed to restore previous job schedules: %w", restoreErr)
			}
			if rescheduleErr := s.RescheduleJobsForSettingKeys(ctx, changedKeys); rescheduleErr != nil {
				return fmt.Errorf("failed to restore runtime job schedules: %w", rescheduleErr)
			}
			return nil
		}())
	}

	return s.GetJobSchedules(ctx), nil
}

func (s *JobService) RescheduleJobsForSettingKeys(ctx context.Context, changedKeys []string) error {
	var rescheduleErrors []error
	for jobID, jobMeta := range meta.GetAllJobMetadata() {
		affected := slices.ContainsFunc([]string{jobMeta.SettingsKey, jobMeta.EnabledKey}, func(key string) bool {
			return key != "" && slices.Contains(changedKeys, key)
		})
		if !affected {
			continue
		}
		if s.cfg.AgentMode && jobMeta.ManagerOnly {
			slog.DebugContext(ctx, "Skipping manager-only job reschedule in agent mode", "job", jobID)
			continue
		}

		// environment-health fans out to per-environment dynamic jobs; delegate the
		// reschedule to environment.EnvironmentService instead of looking up a single job.
		if jobID == "environment-health" {
			if s.OnEnvironmentHealthReschedule == nil {
				rescheduleErrors = append(rescheduleErrors, errors.New("environment-health rescheduler not initialized"))
				continue
			}
			s.OnEnvironmentHealthReschedule(s.lifecycleCtx)
			continue
		}

		// Continuous bus watchers have no cron entry to reschedule; notify the
		// watcher so it re-reads its poll schedule instead.
		if jobMeta.IsContinuous {
			if jobID == "image-polling" {
				if err := s.settings.NotifySettingsChanges(s.lifecycleCtx, "pollingInterval"); err != nil {
					rescheduleErrors = append(rescheduleErrors, err)
				}
			}
			continue
		}

		job, ok := s.scheduler.GetJob(jobID)
		if !ok {
			rescheduleErrors = append(rescheduleErrors, fmt.Errorf("job %s not found in scheduler", jobID))
			continue
		}

		slog.DebugContext(ctx, "Processing job setting change", "job", jobID, "settingsKey", jobMeta.SettingsKey, "enabledKey", jobMeta.EnabledKey)
		if err := s.scheduler.RescheduleJob(s.lifecycleCtx, job); err != nil {
			rescheduleErrors = append(rescheduleErrors, fmt.Errorf("reschedule job %s: %w", jobID, err))
			continue
		}

		runtimeState, ok := s.scheduler.GetJobRuntimeState(jobID)
		if !ok {
			rescheduleErrors = append(rescheduleErrors, fmt.Errorf("job %s has no runtime scheduler state", jobID))
			continue
		}

		conditional, isConditional := job.(scheduler.ConditionalJob)
		expectedSchedule := kit.Ternary(isConditional && !conditional.ShouldSchedule(s.lifecycleCtx), "", job.Schedule(s.lifecycleCtx))
		if runtimeState.Schedule != expectedSchedule {
			rescheduleErrors = append(rescheduleErrors, fmt.Errorf("job %s runtime schedule %q does not match requested schedule %q", jobID, runtimeState.Schedule, expectedSchedule))
		}
	}

	return errors.Join(rescheduleErrors...)
}

func (s *JobService) ListJobs(ctx context.Context) (*jobschedule.JobListResponse, error) {
	allMetadata := meta.GetAllJobMetadata()
	jobs := make([]jobschedule.JobStatus, 0, len(allMetadata))

	for _, jobMeta := range allMetadata {
		schedule := func() string {
			switch {
			// Continuous jobs with a settings key (image-polling) are event-driven but
			// also poll on that cron schedule, so surface the real expression.
			case jobMeta.IsContinuous && jobMeta.SettingsKey == "":
				return "continuous"
			case jobMeta.ID == "analytics-heartbeat":
				return "automatic (checked hourly; sent once per 24h)"
			case jobMeta.SettingsKey == "":
				return ""
			}
			defaultSchedule, _, _, err := settings.DefaultSettingsConfig().FieldByKey(jobMeta.SettingsKey)
			return s.settings.GetStringSetting(ctx, jobMeta.SettingsKey, kit.Ternary(err != nil || defaultSchedule == "", "0 0 0 * * *", defaultSchedule))
		}()
		runtimeState, hasRuntimeState := s.scheduler.GetJobRuntimeState(jobMeta.ID)
		useRuntimeState := hasRuntimeState && (jobMeta.SettingsKey != "" || jobMeta.ID == "upgrade-log-cleanup") && jobMeta.ID != "environment-health"
		jobs = append(jobs, jobMeta.ToJobStatus(
			cmp.Or(kit.Ternary(useRuntimeState, runtimeState.Schedule, ""), schedule),
			kit.Ternary(useRuntimeState, runtimeState.NextRun, func() *time.Time {
				if schedule == "" || schedule == "continuous" {
					return nil
				}
				// Parse schedule and force it to use the same timezone as the scheduler.
				sched, err := scheduleutil.Parser().Parse(schedule)
				if err != nil {
					return nil
				}
				if specSchedule, ok := sched.(*cron.SpecSchedule); ok {
					specSchedule.Location = s.location // keep: cron's parser returns the schedule without a location option
				}
				// Calculate next run using the configured timezone.
				return new(sched.Next(time.Now().In(s.location)))
			}()),
			s.isJobEnabled(ctx, jobMeta),
			s.evaluatePrerequisites(ctx, jobMeta),
		))
	}

	statuses, err := runtime.ApplyStatuses(ctx, s.runs, s.scheduler, "0", jobs)
	if err != nil {
		return nil, err
	}

	// Sort jobs by ID to ensure stable UI order
	slices.SortFunc(statuses, func(left, right jobschedule.JobStatus) int {
		return strings.Compare(left.ID, right.ID)
	})

	return &jobschedule.JobListResponse{
		Jobs:        statuses,
		IsAgent:     s.cfg.AgentMode,
		DurableRuns: true,
		ObservedAt:  time.Now().UTC(),
	}, nil
}

func (s *JobService) isJobEnabled(ctx context.Context, localMeta meta.JobMetadata) bool {
	return localMeta.EnabledKey == "" || s.settings.GetBoolSetting(ctx, localMeta.EnabledKey, false)
}

func (s *JobService) evaluatePrerequisites(ctx context.Context, localMeta meta.JobMetadata) []jobschedule.JobPrerequisite {
	prerequisites := make([]jobschedule.JobPrerequisite, 0, len(localMeta.Prerequisites))

	for _, prereq := range localMeta.Prerequisites {
		prerequisites = append(prerequisites, jobschedule.JobPrerequisite{
			SettingKey:  prereq.SettingKey,
			Label:       prereq.Label,
			IsMet:       s.settings.GetBoolSetting(ctx, prereq.SettingKey, false),
			SettingsURL: prereq.SettingsURL,
		})
	}

	return prerequisites
}

func (s *JobService) Coordinator() *runs.Coordinator { return s.runs }

// ActivityID assigns a summary identity before a visible run is persisted.
func (s *JobService) ActivityID(run scheduler.Run) string {
	// A remotely delivered run already has a summary on its accepting manager.
	if run.Trigger == "remote" {
		return ""
	}
	if run.Trigger != "manual" &&
		!slices.Contains([]string{"auto-update", "auto-patch", "auto-heal", "scheduled-prune", "vulnerability-scan"}, run.JobID) &&
		!strings.HasPrefix(run.JobID, "gitops-sync:") && !strings.HasPrefix(run.JobID, "volume-backup:") && !strings.HasPrefix(run.JobID, "system-backup:") {
		return ""
	}
	return uuid.New().String()
}

// SyncRunActivity projects the latest committed run without changing execution state.
func (s *JobService) SyncRunActivity(ctx context.Context, run scheduler.Run) error {
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
	metadata, found := meta.GetJobMetadata(current.JobID)
	return s.activity.SyncJobRun(ctx, runtime.ProjectRunOutcome(current), kit.Ternary(found, metadata.Name, current.JobID))
}

func (s *JobService) ReconcileStartupActivities(ctx context.Context, extraProtectedIDs ...string) error {
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
	request = scheduler.Request{ //exhaustruct:enforce
		ObservedAgentRun: request.ObservedAgentRun,
		RunID:            request.RunID,
		JobID:            request.JobID,
		EnvironmentID:    cmp.Or(request.EnvironmentID, "0"),
		Trigger:          request.Trigger,
		RequestedWithKey: request.RequestedWithKey,
		RequestedBy:      request.RequestedBy,
	}
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
		if !strings.HasPrefix(request.JobID, "gitops-sync:") && !strings.HasPrefix(request.JobID, "volume-backup:") && (!ok || metadata.ManagerOnly || !metadata.CanRunManually) {
			return scheduler.Run{}, errors.New("job is not remotely runnable")
		}
	} else if err := s.validateLocalJob(ctx, request.JobID); err != nil {
		return scheduler.Run{}, err
	}
	if err := s.authorizeRun(
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

func (s *JobService) validateLocalJob(ctx context.Context, jobID string) error {
	if metadata, ok := meta.GetJobMetadata(jobID); ok {
		if !metadata.CanRunManually {
			return errors.New("job cannot be run manually")
		}
		if s.cfg.AgentMode && metadata.ManagerOnly {
			return errors.New("job is manager-only")
		}
		if !s.isJobEnabled(ctx, metadata) {
			return errors.New("job is disabled")
		}
		if slices.ContainsFunc(s.evaluatePrerequisites(ctx, metadata), func(prereq jobschedule.JobPrerequisite) bool { return !prereq.IsMet }) {
			return errors.New("job prerequisites are not met")
		}
		if metadata.IsContinuous || jobID == "environment-health" {
			return nil
		}
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

func (s *JobService) authorizeRun(ctx context.Context, run scheduler.Run) error {
	if run.RequestedBy == "" {
		if run.Trigger == "manual" && !s.cfg.AgentMode {
			return errors.New("requesting user unavailable")
		}
		return nil
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

func (s *JobService) executeRun(ctx context.Context, run scheduler.Run) (result scheduler.Outcome, runErr error) {
	// Entity-scoped jobs ("gitops-sync:<id>") share one span name and metric series per job type.
	jobType, _, _ := strings.Cut(run.JobID, ":")
	ctx, span := otel.Tracer(tracing.InstrumentationName).Start(ctx, "job "+jobType, trace.WithAttributes(
		attribute.String("arcane.job.id", run.JobID),
		attribute.String("arcane.job.run_id", run.ID),
		attribute.String("arcane.environment.id", run.EnvironmentID),
	))
	jobAttr := attribute.String("arcane.job.id", jobType)
	started := time.Now()
	s.activeRuns.Add(ctx, 1, metric.WithAttributes(jobAttr))
	defer func() {
		outcomeAttr := attribute.String("arcane.job.outcome", string(result.Status))
		span.SetAttributes(outcomeAttr)
		tracing.End(span, runErr)
		// Record against a non-canceled context so shutdown-interrupted runs still count.
		metricCtx := context.WithoutCancel(ctx)
		attrs := metric.WithAttributes(jobAttr, outcomeAttr)
		s.activeRuns.Add(metricCtx, -1, metric.WithAttributes(jobAttr))
		s.runCount.Add(metricCtx, 1, attrs)
		s.runDuration.Record(metricCtx, time.Since(started).Seconds(), attrs)
	}()

	if err := s.authorizeRun(ctx, run); err != nil {
		return scheduler.Outcome{Status: scheduler.Failed}, err
	}
	if run.EnvironmentID != "0" {
		return s.remote.Deliver(ctx, run)
	}
	if slices.Contains([]string{"auto-update", "auto-patch", "auto-heal", "scheduled-prune", "vulnerability-scan", "image-polling"}, run.JobID) ||
		strings.HasPrefix(run.JobID, "gitops-sync:") || strings.HasPrefix(run.JobID, "volume-backup:") || strings.HasPrefix(run.JobID, "system-backup:") {
		status, err := s.environment.TestConnection(ctx, "0", nil)
		if err != nil || status != "online" {
			return scheduler.Outcome{Status: scheduler.Waiting, Message: "Waiting for Docker"}, err
		}
	}
	ctx = s.runs.ExecutionContext(ctx, run, "")
	if run.JobID == "environment-health" && s.RunEnvironmentHealthNow != nil {
		return classifyOutcome(run.JobID, scheduler.Outcome{}, s.RunEnvironmentHealthNow(ctx))
	}
	if metadata, ok := meta.GetJobMetadata(run.JobID); ok && metadata.IsContinuous {
		if err := s.validateLocalJob(ctx, run.JobID); err != nil {
			return scheduler.Outcome{Status: scheduler.Canceled}, err
		}
		return classifyOutcome(run.JobID, scheduler.Outcome{}, s.scheduler.RunBusWatcherNow(ctx, run.JobID))
	}
	job, ok := s.scheduler.GetJob(run.JobID)
	if !ok {
		return scheduler.Outcome{
			Status:  kit.Ternary(run.JobID == "auto-update" && run.AttemptCount > 1, scheduler.Failed, scheduler.Canceled),
			Message: "Job or target no longer exists",
		}, nil
	}
	if conditional, localOk := job.(scheduler.ConditionalJob); localOk && !conditional.ShouldSchedule(ctx) {
		return scheduler.Outcome{
			Status:  kit.Ternary(run.JobID == "auto-update" && run.AttemptCount > 1, scheduler.Failed, scheduler.Canceled),
			Message: "Job is disabled",
		}, nil
	}
	outcome, err := job.Run(ctx)
	return classifyOutcome(run.JobID, outcome, err)
}

func classifyOutcome(jobID string, outcome scheduler.Outcome, err error) (scheduler.Outcome, error) {
	if resultErr, ok := errors.AsType[*scheduler.OutcomeError](err); ok {
		outcome = resultErr.Outcome
	}
	return scheduler.Outcome{ //exhaustruct:enforce
		Status: func() scheduler.RunStatus {
			switch outcome.Status {
			case scheduler.NeedsAttention:
				return scheduler.Failed
			case scheduler.Waiting, scheduler.Retrying, scheduler.Failed, scheduler.Partial, scheduler.Canceled, scheduler.Skipped:
				return outcome.Status
			case scheduler.Queued, scheduler.Running, scheduler.Succeeded:
				// Classify these and unspecified outcomes below.
			}
			var networkErr net.Error
			switch {
			case err == nil:
				return cmp.Or(outcome.Status, scheduler.Succeeded)
			case safeJob(jobID) && (errors.As(err, &networkErr) || errors.Is(err, context.DeadlineExceeded)):
				return scheduler.Retrying
			case outcome.Status == "" || outcome.Status == scheduler.Succeeded:
				return scheduler.Failed
			}
			return outcome.Status
		}(),
		Message:    outcome.Message,
		ActivityID: outcome.ActivityID,
		Targets:    outcome.Targets,
		Steps:      outcome.Steps,
	}, err
}

func safeJob(jobID string) bool {
	switch jobID {
	case "image-polling", "environment-health", "docker-client-refresh", "event-cleanup", "expired-sessions-cleanup", "activity-sweep", "upload-sessions-cleanup",
		"git-clone-scratch-cleanup", "analytics-heartbeat", "apns-outbox", "vulnerability-scan":
		return true
	}
	return strings.HasPrefix(jobID, "environment-health:")
}

func (s *JobService) reconcileRun(ctx context.Context, run scheduler.Run) (scheduler.Outcome, error) {
	if err := s.authorizeRun(ctx, run); err != nil {
		return scheduler.Outcome{Status: scheduler.Failed}, err
	}
	if run.EnvironmentID != "0" || safeJob(run.JobID) {
		return s.executeRun(ctx, run)
	}
	if job, ok := s.scheduler.GetJob(run.JobID); ok {
		if reconciler, localOk := job.(scheduler.Reconciler); localOk {
			return reconciler.Reconcile(s.runs.ExecutionContext(ctx, run, ""), run)
		}
	}
	var activityIDs []string
	for _, target := range run.Outcome.Targets {
		if run.JobID != "auto-update" && target.Status != scheduler.Succeeded && target.Status != scheduler.Skipped && target.ActivityID != "" {
			activityIDs = append(activityIDs, target.ActivityID)
		}
	}
	var records []activity.Activity
	if len(activityIDs) > 0 {
		if err := s.db.WithContext(ctx).Select("id", "status").Where("id IN ?", activityIDs).Find(&records).Error; err != nil {
			return scheduler.Outcome{Status: scheduler.Failed}, err
		}
	}
	statusByID := make(map[string]activitytypes.Status, len(records))
	for _, record := range records {
		statusByID[record.ID] = record.Status
	}
	// A successful activity proves only that target, never the entire batch.
	targets := make([]scheduler.TargetOutcome, 0, len(run.Outcome.Targets))
	for index, target := range run.Outcome.Targets {
		if run.JobID == "auto-update" || target.Status == scheduler.Succeeded || target.Status == scheduler.Skipped {
			targets = append(targets, target)
			continue
		}
		if target.ActivityID == "" {
			return scheduler.Outcome{
				Status:  scheduler.Failed,
				Message: "Interrupted operation has no confirmed outcome",
				Targets: slices.Concat(targets, run.Outcome.Targets[index:]),
			}, nil
		}
		status, found := statusByID[target.ActivityID]
		if !found {
			return scheduler.Outcome{Status: scheduler.Failed}, gorm.ErrRecordNotFound
		}
		if status != activitytypes.StatusSuccess {
			return scheduler.Outcome{
				Status:  scheduler.Failed,
				Message: "Interrupted operation has no confirmed completion",
				Targets: slices.Concat(targets, run.Outcome.Targets[index:]),
			}, nil
		}
		targets = append(targets, scheduler.TargetOutcome{ //exhaustruct:enforce
			RecoveryData: target.RecoveryData,
			ResourceType: target.ResourceType,
			ID:           target.ID,
			Status:       scheduler.Succeeded,
			Message:      target.Message,
			ActivityID:   target.ActivityID,
		})
	}
	return scheduler.Outcome{
		Status:  scheduler.Failed,
		Message: "Interrupted operation has no confirmed completion",
		Targets: targets,
	}, nil
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
		// Queue one explicit retry while retaining the agent's target progress.
		run, err := s.runs.Get(ctx, environmentID, jobID, runID)
		if errors.Is(err, runs.ErrRunNotFound) {
			return s.remote.MutateAgentRun(ctx, environmentID, jobID, runID, "retry")
		}
		if err != nil {
			return run, err
		}
		if authorizeRunErr := s.authorizeRun(ctx, run); authorizeRunErr != nil {
			return run, authorizeRunErr
		}
		return s.remote.RetryRun(ctx, run)
	}
	if err := s.validateLocalJob(ctx, jobID); err != nil {
		return scheduler.Run{}, err
	}
	run, err := s.runs.Get(ctx, environmentID, jobID, runID)
	if err != nil {
		return scheduler.Run{}, err
	}
	if job, ok := s.scheduler.GetJob(jobID); ok {
		if validator, localOk := job.(scheduler.RetryValidator); localOk {
			if validateRetryErr := validator.ValidateRetry(ctx, run); validateRetryErr != nil {
				return run, validateRetryErr
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
	jobs, err := runtime.ApplyStatuses(ctx, s.runs, s.scheduler, environmentID, catalog.Jobs)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(jobs, func(left, right jobschedule.JobStatus) int { return strings.Compare(left.ID, right.ID) })
	return &jobschedule.JobListResponse{ //exhaustruct:enforce
		DurableRuns: catalog.DurableRuns,
		ObservedAt:  catalog.ObservedAt,
		Offline:     catalog.Offline,
		Jobs:        jobs,
		IsAgent:     catalog.IsAgent,
	}, nil
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
	slices.SortFunc(localRuns, func(left, right scheduler.Run) int {
		if left.CreatedAt.Equal(right.CreatedAt) {
			return strings.Compare(left.ID, right.ID)
		}
		return right.CreatedAt.Compare(left.CreatedAt)
	})
	page = max(page, 1)
	limit = min(max(limit, 1), 100)
	start := kit.Ternary(page-1 <= len(localRuns)/limit, min((page-1)*limit, len(localRuns)), len(localRuns))
	return scheduler.RunList{
		Runs:  localRuns[start:min(start+limit, len(localRuns))],
		Total: len(localRuns),
		Page:  page,
		Limit: limit,
	}, nil
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
