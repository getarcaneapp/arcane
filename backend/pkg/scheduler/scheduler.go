package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/robfig/cron/v3"
)

// cronScheduleParser is the shared parser for all cron settings: six fields
// with seconds, plus @-descriptors. The image update watcher parses its poll
// schedule with the same spec so Jobs-UI cron values behave identically.
var cronScheduleParser = cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

type JobScheduler struct {
	// mu guards jobs, jobsByID, entryIDs and schedules. It is held across the cron
	// add/remove calls (which are themselves quick and never block on job
	// execution) but never across job.Run — Run executes on cron's own
	// goroutine, so a running job must not call back into a locking method here.
	mu        sync.Mutex
	cron      *cron.Cron
	jobs      []schedulertypes.Job
	jobsByID  map[string]schedulertypes.Job
	watchers  map[string]schedulertypes.BusWatcher
	entryIDs  map[string]cron.EntryID
	schedules map[string]string
	parser    cron.Parser
	context   context.Context
	location  *time.Location
	watcherWG sync.WaitGroup
}

// NewJobScheduler creates a new job scheduler with the specified timezone location.
// The location is used for interpreting cron expressions.
// If location is nil, UTC is used.
func NewJobScheduler(ctx context.Context, location *time.Location) *JobScheduler {
	if location == nil {
		location = time.UTC
	}
	parser := cronScheduleParser
	slog.InfoContext(ctx, "Initializing job scheduler", "timezone", location.String())
	return &JobScheduler{
		cron:      cron.New(cron.WithParser(parser), cron.WithLocation(location)),
		jobs:      []schedulertypes.Job{},
		jobsByID:  make(map[string]schedulertypes.Job),
		watchers:  make(map[string]schedulertypes.BusWatcher),
		entryIDs:  make(map[string]cron.EntryID),
		schedules: make(map[string]string),
		parser:    parser,
		context:   ctx,
		location:  location,
	}
}

// RegisterJob records a static job to be scheduled when StartScheduler runs. Use
// AddJob for jobs added dynamically at runtime.
func (js *JobScheduler) RegisterJob(job schedulertypes.Job) {
	js.mu.Lock()
	defer js.mu.Unlock()
	js.jobs = append(js.jobs, job)
	js.jobsByID[job.Name()] = job
}

// RegisterBusWatcher starts a continuous event watcher on the scheduler lifecycle.
func (js *JobScheduler) RegisterBusWatcher(watcher schedulertypes.BusWatcher, canRunManually bool) {
	if watcher == nil {
		return
	}
	if canRunManually {
		js.mu.Lock()
		js.watchers[watcher.Name()] = watcher
		js.mu.Unlock()
	}

	js.watcherWG.Go(func() {
		if err := watcher.Start(js.context); err != nil {
			slog.ErrorContext(js.context, "Bus watcher failed", "name", watcher.Name(), "error", err)
		}
	})
}

// RunBusWatcherNow runs a watcher through its serialized manual path.
func (js *JobScheduler) RunBusWatcherNow(ctx context.Context, watcherID string) error {
	js.mu.Lock()
	watcher, ok := js.watchers[watcherID]
	js.mu.Unlock()
	if !ok {
		return fmt.Errorf("bus watcher %s is not manually runnable", watcherID)
	}

	return watcher.RunNow(ctx)
}

func (js *JobScheduler) GetJob(jobID string) (schedulertypes.Job, bool) {
	js.mu.Lock()
	defer js.mu.Unlock()
	job, ok := js.jobsByID[jobID]
	return job, ok
}

// GetJobRuntimeState returns the schedule currently installed for a registered job.
func (js *JobScheduler) GetJobRuntimeState(jobID string) (schedulertypes.JobRuntimeState, bool) {
	js.mu.Lock()
	defer js.mu.Unlock()

	if _, ok := js.jobsByID[jobID]; !ok {
		return schedulertypes.JobRuntimeState{}, false
	}

	state := schedulertypes.JobRuntimeState{Schedule: js.schedules[jobID]}
	entryID, ok := js.entryIDs[jobID]
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

// HasJob reports whether a job with the given name is currently registered.
func (js *JobScheduler) HasJob(jobID string) bool {
	js.mu.Lock()
	defer js.mu.Unlock()
	_, ok := js.jobsByID[jobID]
	return ok
}

func (js *JobScheduler) StartScheduler() {
	js.mu.Lock()
	for _, job := range js.jobs {
		if err := js.upsertJobInternal(js.context, job); err != nil {
			slog.ErrorContext(js.context, "Failed to schedule job", "name", job.Name(), "error", err)
		}
	}
	js.mu.Unlock()
	js.cron.Start()
}

// AddJob registers and schedules a job at runtime. It is an idempotent upsert: a
// replacement expression is validated before the existing entry is removed, then
// the replacement is installed without leaking a second live entry.
// Safe to call before or after StartScheduler.
func (js *JobScheduler) AddJob(ctx context.Context, job schedulertypes.Job) error {
	js.mu.Lock()
	defer js.mu.Unlock()
	return js.upsertJobInternal(ctx, job)
}

// RemoveJob unschedules and forgets a job by name. It is a no-op (not an error)
// when no job with that name is registered.
func (js *JobScheduler) RemoveJob(ctx context.Context, jobName string) {
	js.mu.Lock()
	defer js.mu.Unlock()

	if entryID, ok := js.entryIDs[jobName]; ok {
		js.cron.Remove(entryID)
		delete(js.entryIDs, jobName)
	}
	delete(js.jobsByID, jobName)
	delete(js.schedules, jobName)
	for i, j := range js.jobs {
		if j.Name() == jobName {
			js.jobs = append(js.jobs[:i], js.jobs[i+1:]...)
			break
		}
	}
	slog.DebugContext(ctx, "Job removed", "name", jobName)
}

func (js *JobScheduler) RescheduleJob(ctx context.Context, job schedulertypes.Job) error {
	js.mu.Lock()
	defer js.mu.Unlock()
	return js.upsertJobInternal(ctx, job)
}

// GetLocation returns the timezone location used by the scheduler for cron expressions.
func (js *JobScheduler) GetLocation() *time.Location {
	return js.location
}

func (js *JobScheduler) Run(ctx context.Context) error {
	js.StartScheduler()
	<-ctx.Done()
	// Running jobs may still own Docker or database resources. Wait for them here
	// so Bootstrap cannot close shared services underneath them; the process-level
	// signal handler owns the hard shutdown deadline for non-cooperative jobs.
	<-js.cron.Stop().Done()
	js.watcherWG.Wait()
	return nil
}

// upsertJobInternal records the job and (re)schedules it. A replacement expression
// is parsed before the previous entry is removed so invalid input leaves the last
// valid entry untouched. Callers must hold js.mu.
func (js *JobScheduler) upsertJobInternal(ctx context.Context, job schedulertypes.Job) error {
	jobName := job.Name()
	previousSchedule := js.schedules[jobName]
	previousEntryID, hadPreviousEntry := js.entryIDs[jobName]
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
		delete(js.entryIDs, jobName)
	}
	if shouldSchedule {
		entryID, nextRun = js.addCronEntryInternal(job, schedule, parsedSchedule)
		js.entryIDs[jobName] = entryID
	}

	js.jobsByID[jobName] = job
	js.schedules[jobName] = schedule

	if previousSchedule == "" && shouldSchedule {
		slog.InfoContext(ctx, "Starting Job", "name", jobName, "schedule", schedule)
	} else if previousSchedule != schedule || hadPreviousEntry != shouldSchedule {
		slog.InfoContext(ctx, "Job rescheduled", "name", jobName, "previousSchedule", previousSchedule, "newSchedule", schedule, "nextRun", nextRun)
	}

	slog.DebugContext(ctx, "Job scheduled", "name", jobName, "scheduled", shouldSchedule, "contextCanceled", ctx.Err() != nil)
	return nil
}

// addCronEntryInternal adds a job closure that always runs with the scheduler's
// lifecycle context. Callers must hold js.mu.
func (js *JobScheduler) addCronEntryInternal(job schedulertypes.Job, schedule string, parsedSchedule cron.Schedule) (cron.EntryID, *time.Time) {
	entryID := js.cron.Schedule(parsedSchedule, cron.FuncJob(func() {
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
