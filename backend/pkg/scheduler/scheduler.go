package scheduler

import (
	"context"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/actors"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/robfig/cron/v3"
)

// cronScheduleParser is the shared parser for all cron settings: six fields
// with seconds, plus @-descriptors. The image update watcher parses its poll
// schedule with the same spec so Jobs-UI cron values behave identically.
var cronScheduleParser = cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

var errJobSchedulerStoppedInternal = errors.Sentinel("job scheduler stopped")

type schedulerStateInternal struct {
	jobs        []schedulertypes.Job
	jobsByID    map[string]schedulertypes.Job
	watchers    map[string]schedulertypes.BusWatcher
	allWatchers map[string]schedulertypes.BusWatcher
	runners     map[string]*actors.Runner
	entryIDs    map[string]cron.EntryID
	schedules   map[string]string
	runLocks    map[string]*sync.Mutex
	stopping    bool
}

type watcherRegistrationInternal struct {
	watcher schedulertypes.BusWatcher
	runner  *actors.Runner
}

type schedulerShutdownInternal struct {
	cronDone <-chan struct{}
	watchers []watcherRegistrationInternal
}

// jobSchedulerInternal serializes its control plane through one shared actor executor.
// robfig/cron remains the timing engine and invokes job bodies outside the actor.
type jobSchedulerInternal struct {
	cron     *cron.Cron
	parser   cron.Parser
	context  context.Context
	location *time.Location
	runtime  *actors.Runtime
	state    *actors.State[schedulerStateInternal]
}

// NewJobScheduler creates an actor-owned scheduler control plane.
func NewJobScheduler(ctx context.Context, runtime *actors.Runtime, location *time.Location) (schedulertypes.JobScheduler, error) {
	if ctx == nil {
		return nil, errors.New("job scheduler context unavailable")
	}
	if runtime == nil {
		return nil, errors.New("actor runtime unavailable")
	}
	if location == nil {
		location = time.UTC
	}
	initial := schedulerStateInternal{
		jobs:        []schedulertypes.Job{},
		jobsByID:    make(map[string]schedulertypes.Job),
		watchers:    make(map[string]schedulertypes.BusWatcher),
		allWatchers: make(map[string]schedulertypes.BusWatcher),
		runners:     make(map[string]*actors.Runner),
		entryIDs:    make(map[string]cron.EntryID),
		schedules:   make(map[string]string),
		runLocks:    make(map[string]*sync.Mutex),
	}
	state, err := actors.NewState(ctx, runtime, "scheduler", "control-plane", 3, initial, func(value schedulerStateInternal) schedulerStateInternal {
		value.jobs = slices.Clone(value.jobs)
		value.jobsByID = maps.Clone(value.jobsByID)
		value.watchers = maps.Clone(value.watchers)
		value.allWatchers = maps.Clone(value.allWatchers)
		value.runners = maps.Clone(value.runners)
		value.entryIDs = maps.Clone(value.entryIDs)
		value.schedules = maps.Clone(value.schedules)
		value.runLocks = maps.Clone(value.runLocks)
		return value
	})
	if err != nil {
		return nil, err
	}
	parser := cronScheduleParser
	scheduler := &jobSchedulerInternal{
		cron:     cron.New(cron.WithParser(parser), cron.WithLocation(location)),
		parser:   parser,
		context:  ctx,
		location: location,
		runtime:  runtime,
		state:    state,
	}
	slog.InfoContext(ctx, "Initializing job scheduler", "timezone", location.String())
	return scheduler, nil
}

// RegisterJob records a static job to be scheduled when StartScheduler runs.
func (js *jobSchedulerInternal) RegisterJob(job schedulertypes.Job) error {
	if job == nil {
		return errors.New("scheduler job unavailable")
	}
	return js.state.Apply(js.context, "register scheduler job", func(_ context.Context, state *schedulerStateInternal) error {
		if state.stopping {
			return errJobSchedulerStoppedInternal
		}
		state.jobs = append(state.jobs, job)
		state.jobsByID[job.Name()] = job
		return nil
	})
}

// RegisterBusWatcher starts a continuous watcher through a shared actor runner.
func (js *jobSchedulerInternal) RegisterBusWatcher(watcher schedulertypes.BusWatcher, canRunManually bool) error {
	if watcher == nil {
		return errors.New("scheduler bus watcher unavailable")
	}
	return js.state.Apply(js.context, "register scheduler bus watcher", func(_ context.Context, state *schedulerStateInternal) error {
		if state.stopping {
			return errJobSchedulerStoppedInternal
		}
		runner, err := actors.NewRunner(js.context, js.runtime, "scheduler-watchers", watcher.Name(), "bus watcher "+watcher.Name(), 3, watcher.Start) //nolint:contextcheck // watcher lifetime belongs to the scheduler, not one state-actor receiver incarnation.
		if err != nil {
			return err
		}
		state.runners[watcher.Name()] = runner
		state.allWatchers[watcher.Name()] = watcher
		if canRunManually {
			state.watchers[watcher.Name()] = watcher
		}
		return nil
	})
}

// RunBusWatcherNow runs a watcher through its serialized manual path.
func (js *jobSchedulerInternal) RunBusWatcherNow(ctx context.Context, watcherID string) error {
	snapshot, ok := js.state.Load()
	if !ok {
		return errors.New("job scheduler state unavailable")
	}
	watcher, ok := snapshot.watchers[watcherID]
	if !ok {
		return errors.Errorf("bus watcher %s is not manually runnable", watcherID)
	}
	return watcher.RunNow(ctx)
}

func (js *jobSchedulerInternal) GetJob(jobID string) (schedulertypes.Job, bool) {
	snapshot, ok := js.state.Load()
	if !ok {
		return nil, false
	}
	job, ok := snapshot.jobsByID[jobID]
	return job, ok
}

// GetJobRuntimeState returns the schedule currently installed for a registered job.
func (js *jobSchedulerInternal) GetJobRuntimeState(jobID string) (schedulertypes.JobRuntimeState, bool) {
	snapshot, ok := js.state.Load()
	if !ok {
		return schedulertypes.JobRuntimeState{}, false
	}
	if _, ok := snapshot.jobsByID[jobID]; !ok {
		return schedulertypes.JobRuntimeState{}, false
	}

	state := schedulertypes.JobRuntimeState{Schedule: snapshot.schedules[jobID]}
	entryID, ok := snapshot.entryIDs[jobID]
	if !ok {
		return state, true
	}
	entry := js.cron.Entry(entryID)
	if entry.ID == 0 {
		return state, true
	}
	state.Scheduled = true
	nextRun := entry.Next
	if nextRun.IsZero() && entry.Schedule != nil {
		nextRun = entry.Schedule.Next(time.Now().In(js.location))
	}
	if !nextRun.IsZero() {
		state.NextRun = new(nextRun)
	}
	return state, true
}

// HasJob reports whether a job with the given name is registered.
func (js *jobSchedulerInternal) HasJob(jobID string) bool {
	snapshot, ok := js.state.Load()
	if !ok {
		return false
	}
	_, ok = snapshot.jobsByID[jobID]
	return ok
}

// StartScheduler installs static schedules and starts cron.
func (js *jobSchedulerInternal) StartScheduler() error {
	return js.state.Apply(js.context, "start job scheduler", func(actorCtx context.Context, state *schedulerStateInternal) error {
		if state.stopping {
			return errJobSchedulerStoppedInternal
		}
		for _, job := range state.jobs {
			if err := js.upsertJobInternal(actorCtx, state, job); err != nil {
				slog.ErrorContext(actorCtx, "Failed to schedule job; continuing scheduler startup", "name", job.Name(), "error", err)
			}
		}
		js.cron.Start()
		return nil
	})
}

// AddJob registers and schedules a job at runtime.
func (js *jobSchedulerInternal) AddJob(ctx context.Context, job schedulertypes.Job) error {
	return js.state.Apply(ctx, "add scheduler job", func(_ context.Context, state *schedulerStateInternal) error {
		if state.stopping {
			return errJobSchedulerStoppedInternal
		}
		return js.upsertJobInternal(ctx, state, job)
	})
}

// RemoveJob unschedules and forgets a job. Actor failures are logged because
// DynamicScheduler preserves the historical no-result removal contract.
func (js *jobSchedulerInternal) RemoveJob(ctx context.Context, jobName string) {
	err := js.state.Apply(ctx, "remove scheduler job", func(_ context.Context, state *schedulerStateInternal) error {
		if state.stopping {
			return errJobSchedulerStoppedInternal
		}
		if entryID, ok := state.entryIDs[jobName]; ok {
			js.cron.Remove(entryID)
			delete(state.entryIDs, jobName)
		}
		delete(state.jobsByID, jobName)
		delete(state.schedules, jobName)
		for index, job := range state.jobs {
			if job.Name() == jobName {
				state.jobs = append(state.jobs[:index], state.jobs[index+1:]...)
				break
			}
		}
		slog.DebugContext(ctx, "Job removed", "name", jobName)
		return nil
	})
	if err != nil && !errors.Is(err, errJobSchedulerStoppedInternal) {
		slog.ErrorContext(ctx, "Failed to remove scheduler job", "name", jobName, "error", err)
	}
}

func (js *jobSchedulerInternal) RescheduleJob(ctx context.Context, job schedulertypes.Job) error {
	return js.state.Apply(ctx, "reschedule job", func(_ context.Context, state *schedulerStateInternal) error {
		if state.stopping {
			return errJobSchedulerStoppedInternal
		}
		return js.upsertJobInternal(ctx, state, job)
	})
}

func (js *jobSchedulerInternal) GetLocation() *time.Location {
	return js.location
}

// Stop fences the control plane, stops cron, and joins every actor-owned watcher.
func (js *jobSchedulerInternal) Stop(ctx context.Context) error {
	var shutdown schedulerShutdownInternal
	err := js.state.Apply(ctx, "stop job scheduler", func(_ context.Context, state *schedulerStateInternal) error {
		if state.stopping {
			return errJobSchedulerStoppedInternal
		}
		state.stopping = true
		registrations := make([]watcherRegistrationInternal, 0, len(state.runners))
		for name, runner := range state.runners {
			registrations = append(registrations, watcherRegistrationInternal{watcher: state.allWatchers[name], runner: runner})
		}
		shutdown = schedulerShutdownInternal{cronDone: js.cron.Stop().Done(), watchers: registrations}
		return nil
	})
	if errors.Is(err, errJobSchedulerStoppedInternal) {
		return js.state.Stop(ctx)
	}
	var stopErr error
	if err != nil {
		if snapshot, ok := js.state.Load(); ok && snapshot.stopping {
			return nil
		}
		stopErr = err
		shutdown.cronDone = js.cron.Stop().Done()
		if snapshot, ok := js.state.Load(); ok {
			shutdown.watchers = make([]watcherRegistrationInternal, 0, len(snapshot.runners))
			for name, runner := range snapshot.runners {
				shutdown.watchers = append(shutdown.watchers, watcherRegistrationInternal{watcher: snapshot.allWatchers[name], runner: runner})
			}
		}
	}

	for _, registration := range shutdown.watchers {
		stopErr = errors.Combine(stopErr, registration.runner.Stop(ctx))
		if stopper, ok := registration.watcher.(schedulertypes.StoppableBusWatcher); ok {
			stopErr = errors.Combine(stopErr, stopper.Stop(ctx))
		}
	}
	select {
	case <-shutdown.cronDone:
	case <-ctx.Done():
		stopErr = errors.Combine(stopErr, ctx.Err())
	}
	return errors.Combine(stopErr, js.state.Stop(ctx))
}

func (js *jobSchedulerInternal) upsertJobInternal(ctx context.Context, state *schedulerStateInternal, job schedulertypes.Job) error {
	if job == nil {
		return errors.New("scheduler job unavailable")
	}
	jobName := job.Name()
	previousSchedule := state.schedules[jobName]
	previousEntryID, hadPreviousEntry := state.entryIDs[jobName]
	schedule := job.Schedule(ctx)

	shouldSchedule := true
	if conditionalJob, ok := job.(schedulertypes.ConditionalJob); ok {
		shouldSchedule = conditionalJob.ShouldSchedule(ctx)
	}

	var (
		parsedSchedule cron.Schedule
		entryID        cron.EntryID
		nextRun        *time.Time
	)
	if shouldSchedule {
		var err error
		parsedSchedule, err = js.parser.Parse(schedule)
		if err != nil {
			return err
		}
	} else {
		slog.DebugContext(ctx, "Job disabled; not scheduling", "name", jobName)
	}

	if hadPreviousEntry {
		js.cron.Remove(previousEntryID)
		delete(state.entryIDs, jobName)
	}
	if shouldSchedule {
		runLock, ok := state.runLocks[jobName]
		if !ok {
			runLock = &sync.Mutex{}
			state.runLocks[jobName] = runLock
		}
		entryID, nextRun = js.addCronEntryInternal(job, schedule, parsedSchedule, runLock)
		state.entryIDs[jobName] = entryID
	}
	state.jobsByID[jobName] = job
	state.schedules[jobName] = schedule

	if previousSchedule == "" && shouldSchedule {
		slog.InfoContext(ctx, "Starting Job", "name", jobName, "schedule", schedule)
	} else if previousSchedule != schedule || hadPreviousEntry != shouldSchedule {
		slog.InfoContext(ctx, "Job rescheduled", "name", jobName, "previousSchedule", previousSchedule, "newSchedule", schedule, "nextRun", nextRun)
	}
	slog.DebugContext(ctx, "Job scheduled", "name", jobName, "scheduled", shouldSchedule, "contextCanceled", ctx.Err() != nil)
	return nil
}

func (js *jobSchedulerInternal) addCronEntryInternal(job schedulertypes.Job, schedule string, parsedSchedule cron.Schedule, runLock *sync.Mutex) (cron.EntryID, *time.Time) {
	entryID := js.cron.Schedule(parsedSchedule, cron.FuncJob(func() {
		if !runLock.TryLock() {
			slog.WarnContext(js.context, "Job skipped; previous run still in progress", "name", job.Name(), "schedule", schedule)
			return
		}
		defer runLock.Unlock()
		defer utils.RecoverToError(nil, "scheduled job", "name", job.Name(), "schedule", schedule)
		slog.InfoContext(js.context, "Job starting", "name", job.Name(), "schedule", schedule)
		job.Run(js.context)
		slog.InfoContext(js.context, "Job finished", "name", job.Name())
	}))

	entry := js.cron.Entry(entryID)
	nextRun := entry.Next
	if nextRun.IsZero() && entry.Schedule != nil {
		nextRun = entry.Schedule.Next(time.Now().In(js.location))
	}
	if nextRun.IsZero() {
		return entryID, nil
	}
	return entryID, new(nextRun)
}
