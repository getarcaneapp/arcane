package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"

	"github.com/getarcaneapp/arcane/backend/v3/pkg/scheduler/runs"
	scheduleutil "github.com/getarcaneapp/arcane/backend/v3/pkg/scheduler/schedule"
)

var (
	cronScheduleParser     = scheduleutil.Parser()
	errJobSchedulerStopped = errors.New("job scheduler stopped")
)

type watcherRegistration struct {
	watcher types.BusWatcher
	done    chan struct{}
}
type jobScheduler struct {
	context     context.Context
	jobsByID    map[string]types.SchedulerJob
	watchers    map[string]types.BusWatcher
	allWatchers map[string]watcherRegistration
	supervisors map[string]*watcherSupervisor
	cancel      context.CancelFunc
	location    *time.Location
	coordinator *runs.Coordinator
	mu          sync.RWMutex
	stopping    bool
	started     bool
}

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
			supervisors: map[string]*watcherSupervisor{},
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
	supervisor := &watcherSupervisor{watcher: watcher, restart: make(chan struct{}, 1), health: types.SchedulerWorkerHealth{Status: "starting", UpdatedAt: time.Now()}}
	done := make(chan struct{})
	js.supervisors[watcher.Name()] = supervisor
	js.allWatchers[watcher.Name()] = watcherRegistration{watcher: watcher, done: done}
	if manual {
		js.watchers[watcher.Name()] = watcher
	}
	go func() {
		defer close(done)
		if err := supervisor.run(js.context); err != nil && js.context.Err() == nil {
			slog.ErrorContext(js.context, "Watcher stopped", "name", watcher.Name(), "error", err)
		}
	}()
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
	record, err := js.coordinator.ScheduleState(js.context, id)
	if err != nil {
		return types.JobRuntimeState{}, true
	}
	state := types.JobRuntimeState{Schedule: record.Schedule, Scheduled: !record.NextRun.IsZero()}
	if state.Scheduled {
		state.NextRun = new(record.NextRun)
	}
	return state, true
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
			if removeErr := js.coordinator.Remove(ctx, record.JobID); removeErr != nil {
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
	next := time.Time{}
	schedule := job.Schedule(ctx)
	should := true
	if conditional, ok := job.(types.ConditionalJob); ok {
		should = conditional.ShouldSchedule(ctx)
	}
	if should {
		parsed, err := cronScheduleParser.Parse(schedule)
		if err != nil {
			return err
		}
		next = parsed.Next(time.Now().In(js.location))
	}
	if !should {
		schedule = ""
	}
	return js.coordinator.Checkpoint(ctx, job.Name(), schedule, next)
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

func (js *jobScheduler) RescheduleJob(ctx context.Context, job types.SchedulerJob) error {
	return js.AddJob(ctx, job)
}

func (js *jobScheduler) RemoveJob(ctx context.Context, id string) {
	js.mu.Lock()
	delete(js.jobsByID, id)
	started := js.started
	js.mu.Unlock()
	if started {
		if err := js.coordinator.Remove(ctx, id); err != nil {
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
		select {
		case <-w.done:
		case <-ctx.Done():
			return errors.Join(err, ctx.Err())
		}
		if stop, ok := w.watcher.(types.StoppableBusWatcher); ok {
			err = errors.Join(err, stop.Stop(ctx))
		}
	}
	return err
}
