package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"kit.getarcane.app"
	"kit.getarcane.app/concurrency"

	"getarcane.dev/arcane/v3/internal/scheduler/runs"
	"getarcane.dev/types/v3"
)

type (
	watcherRegistration struct {
		watcher    types.BusWatcher
		supervisor *concurrency.Supervisor
	}

	jobScheduler struct {
		context     context.Context
		jobsByID    map[string]types.SchedulerJob
		watchers    map[string]types.BusWatcher
		allWatchers map[string]watcherRegistration
		cancel      context.CancelFunc
		location    *time.Location
		coordinator *runs.Coordinator
		mu          sync.RWMutex
		stopping    bool
		started     bool
	}
)

var errJobSchedulerStopped = errors.New("job scheduler stopped")

func NewJobScheduler(ctx context.Context, coordinator *runs.Coordinator, location *time.Location) (types.JobScheduler, error) {
	if ctx == nil || coordinator == nil {
		return nil, errors.New("scheduler dependencies unavailable")
	}
	if location == nil {
		location = time.UTC
	}
	lifetime, cancel := context.WithCancel(ctx)
	return &jobScheduler{
			context:     lifetime,
			cancel:      cancel,
			location:    location,
			coordinator: coordinator,
			jobsByID:    map[string]types.SchedulerJob{},
			watchers:    map[string]types.BusWatcher{},
			allWatchers: map[string]watcherRegistration{},
		},
		nil
}

func (js *jobScheduler) RegisterJob(job types.SchedulerJob) error {
	if job == nil {
		return errors.New("scheduler job unavailable")
	}
	js.mu.Lock()
	defer js.mu.Unlock()
	if js.stopping {
		return errJobSchedulerStopped
	}
	js.jobsByID[job.Name()] = job
	return nil
}

func (js *jobScheduler) RegisterBusWatcher(watcher types.BusWatcher, manual bool) error {
	js.mu.Lock()
	defer js.mu.Unlock()
	if js.stopping {
		return errJobSchedulerStopped
	}
	if _, found := js.allWatchers[watcher.Name()]; found {
		return errors.New("watcher already registered")
	}
	js.allWatchers[watcher.Name()] = watcherRegistration{
		watcher: watcher,
		supervisor: concurrency.Supervise(js.context, concurrency.SuperviseConfig{
			Run:  watcher.Start,
			Name: watcher.Name(),
		}),
	}
	if manual {
		js.watchers[watcher.Name()] = watcher
	}
	return nil
}

func (js *jobScheduler) RunBusWatcherNow(ctx context.Context, id string) error {
	js.mu.RLock()
	watcher, found := js.watchers[id]
	js.mu.RUnlock()
	if !found {
		return errors.New("watcher is not manually runnable")
	}
	return watcher.RunNow(ctx)
}

func (js *jobScheduler) GetJob(id string) (types.SchedulerJob, bool) {
	js.mu.RLock()
	defer js.mu.RUnlock()
	job, ok := js.jobsByID[id]
	return job, ok
}

func (js *jobScheduler) HasJob(id string) bool { _, ok := js.GetJob(id); return ok }

func (js *jobScheduler) GetJobRuntimeState(id string) (types.JobRuntimeState, bool) {
	if !js.HasJob(id) {
		return types.JobRuntimeState{}, false
	}
	record, nextRun, err := js.coordinator.ScheduleState(js.context, id)
	if err != nil {
		return types.JobRuntimeState{}, true
	}
	return types.JobRuntimeState{
		NextRun:   nextRun,
		Schedule:  record.Schedule,
		Scheduled: nextRun != nil,
	}, true
}

func (js *jobScheduler) StartScheduler(ctx context.Context) error {
	js.mu.Lock()
	if js.stopping {
		js.mu.Unlock()
		return errJobSchedulerStopped
	}
	js.started = true
	js.mu.Unlock()
	records, err := js.coordinator.Records(ctx)
	if err != nil {
		return err
	}
	for _, record := range records {
		js.mu.RLock()
		_, watching := js.watchers[record.JobID]
		js.mu.RUnlock()
		if record.EnvironmentID == "0" && record.Schedule != "" && !js.HasJob(record.JobID) && !watching {
			if removeErr := js.coordinator.Install(ctx, record.JobID, ""); removeErr != nil {
				return removeErr
			}
		}
	}
	var schedulingErr error
	for _, job := range js.ListRegisteredJobs() {
		if installErr := js.install(ctx, job); installErr != nil {
			schedulingErr = errors.Join(schedulingErr, fmt.Errorf("%s: %w", "schedule "+job.Name(), installErr))
		}
	}
	return schedulingErr
}

func (js *jobScheduler) install(ctx context.Context, job types.SchedulerJob) error {
	if conditional, ok := job.(types.ConditionalJob); ok && !conditional.ShouldSchedule(ctx) {
		return js.coordinator.Install(ctx, job.Name(), "")
	}
	schedule := job.Schedule(ctx)
	if _, err := cron.ParseStandard(schedule); err != nil {
		return err
	}
	return js.coordinator.Install(ctx, job.Name(), schedule)
}

func (js *jobScheduler) AddJob(ctx context.Context, job types.SchedulerJob) error {
	if err := js.RegisterJob(job); err != nil {
		return err
	}
	js.mu.RLock()
	started := js.started
	js.mu.RUnlock()
	if !started {
		return nil
	}
	return js.install(ctx, job)
}

func (js *jobScheduler) RemoveJob(ctx context.Context, id string) {
	js.mu.Lock()
	delete(js.jobsByID, id)
	started := js.started
	js.mu.Unlock()
	if started {
		if err := js.coordinator.Install(ctx, id, ""); err != nil {
			slog.ErrorContext(ctx, "Unschedule failed", "job", id, "error", err)
		}
	}
}

func (js *jobScheduler) GetLocation() *time.Location { return js.location }

func (js *jobScheduler) Stop(ctx context.Context) error {
	js.mu.Lock()
	js.stopping = true
	js.cancel()
	watchers := make([]watcherRegistration, 0, len(js.allWatchers))
	for _, w := range js.allWatchers {
		watchers = append(watchers, w)
	}
	js.mu.Unlock()
	var err error
	for _, w := range watchers {
		if stopErr := w.supervisor.Stop(ctx); stopErr != nil {
			return errors.Join(err, stopErr)
		}
		if stop, ok := w.watcher.(types.StoppableBusWatcher); ok {
			err = errors.Join(err, stop.Stop(ctx))
		}
	}
	return err
}

func (js *jobScheduler) Submit(ctx context.Context, request types.SchedulerRequest) (types.SchedulerRun, error) {
	if js.context.Err() != nil {
		return types.SchedulerRun{}, errJobSchedulerStopped
	}
	return js.coordinator.Submit(ctx, request)
}

func (js *jobScheduler) ListRegisteredJobs() []types.SchedulerJob {
	js.mu.RLock()
	defer js.mu.RUnlock()
	jobs := make([]types.SchedulerJob, 0, len(js.jobsByID))
	for _, job := range js.jobsByID {
		jobs = append(jobs, job)
	}
	return jobs
}

func (js *jobScheduler) RestartWatcher(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	js.mu.RLock()
	registration, ok := js.allWatchers[id]
	stopping := js.stopping
	js.mu.RUnlock()
	if stopping {
		return errJobSchedulerStopped
	}
	if !ok {
		return errors.New("watcher not found")
	}
	registration.supervisor.Restart()
	return nil
}

func (js *jobScheduler) WatcherHealth(id string) (types.SchedulerWorkerHealth, bool) {
	js.mu.RLock()
	registration, ok := js.allWatchers[id]
	js.mu.RUnlock()
	if !ok {
		return types.SchedulerWorkerHealth{}, false
	}
	state := registration.supervisor.State()
	watcher, local := registration.watcher.(*ImageUpdateWatcher)
	degraded := local && watcher.eventDegraded.Load() && state.Status == concurrency.SupervisorRunning
	return types.SchedulerWorkerHealth{
		UpdatedAt: state.UpdatedAt,
		NextRetry: state.NextRetry,
		Status:    kit.Ternary(degraded, "degraded", string(state.Status)),
		LastError: kit.Ternary(degraded, "Docker image event subscription unavailable; polling remains active", state.LastError),
	}, true
}
