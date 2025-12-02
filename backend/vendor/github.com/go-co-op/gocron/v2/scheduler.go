//go:generate mockgen -destination=mocks/scheduler.go -package=gocronmocks . Scheduler
package gocron

import (
	"context"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jonboulle/clockwork"
)

var _ Scheduler = (*scheduler)(nil)

// Scheduler defines the interface for the Scheduler.
type Scheduler interface {
	// Jobs returns all the jobs currently in the scheduler.
	Jobs() []Job
	// NewJob creates a new job in the Scheduler. The job is scheduled per the provided
	// definition when the Scheduler is started. If the Scheduler is already running
	// the job will be scheduled when the Scheduler is started.
	// If you set the first argument of your Task func to be a context.Context,
	// gocron will pass in a context (either the default Job context, or one
	// provided via WithContext) to the job and will cancel the context on shutdown.
	// This allows you to listen for and handle cancellation within your job.
	NewJob(JobDefinition, Task, ...JobOption) (Job, error)
	// RemoveByTags removes all jobs that have at least one of the provided tags.
	RemoveByTags(...string)
	// RemoveJob removes the job with the provided id.
	RemoveJob(uuid.UUID) error
	// Shutdown should be called when you no longer need
	// the Scheduler or Job's as the Scheduler cannot
	// be restarted after calling Shutdown. This is similar
	// to a Close or Cleanup method and is often deferred after
	// starting the scheduler.
	Shutdown() error
	// Start begins scheduling jobs for execution based
	// on each job's definition. Job's added to an already
	// running scheduler will be scheduled immediately based
	// on definition. Start is non-blocking.
	Start()
	// StopJobs stops the execution of all jobs in the scheduler.
	// This can be useful in situations where jobs need to be
	// paused globally and then restarted with Start().
	StopJobs() error
	// Update replaces the existing Job's JobDefinition with the provided
	// JobDefinition. The Job's Job.ID() remains the same.
	Update(uuid.UUID, JobDefinition, Task, ...JobOption) (Job, error)
	// JobsWaitingInQueue number of jobs waiting in Queue in case of LimitModeWait
	// In case of LimitModeReschedule or no limit it will be always zero
	JobsWaitingInQueue() int
}

// -----------------------------------------------
// -----------------------------------------------
// ----------------- Scheduler -------------------
// -----------------------------------------------
// -----------------------------------------------

type scheduler struct {
	// context used for shutting down
	shutdownCtx context.Context
	// cancel used to signal scheduler should shut down
	shutdownCancel context.CancelFunc
	// the executor, which actually runs the jobs sent to it via the scheduler
	exec executor
	// the map of jobs registered in the scheduler
	jobs map[uuid.UUID]internalJob
	// the location used by the scheduler for scheduling when relevant
	location *time.Location
	// whether the scheduler has been started or not
	started atomic.Bool
	// globally applied JobOption's set on all jobs added to the scheduler
	// note: individually set JobOption's take precedence.
	globalJobOptions []JobOption
	// the scheduler's logger
	logger Logger

	// used to tell the scheduler to start
	startCh chan struct{}
	// used to report that the scheduler has started
	startedCh chan struct{}
	// used to tell the scheduler to stop
	stopCh chan struct{}
	// used to report that the scheduler has stopped
	stopErrCh chan error
	// used to send all the jobs out when a request is made by the client
	allJobsOutRequest chan allJobsOutRequest
	// used to send a jobs out when a request is made by the client
	jobOutRequestCh chan *jobOutRequest
	// used to run a job on-demand when requested by the client
	runJobRequestCh chan runJobRequest
	// new jobs are received here
	newJobCh chan newJobIn
	// requests from the client to remove jobs by ID are received here
	removeJobCh chan uuid.UUID
	// requests from the client to remove jobs by tags are received here
	removeJobsByTagsCh chan []string
}

type newJobIn struct {
	ctx    context.Context
	cancel context.CancelFunc
	job    internalJob
}

type jobOutRequest struct {
	id      uuid.UUID
	outChan chan internalJob
}

type runJobRequest struct {
	id      uuid.UUID
	outChan chan error
}

type allJobsOutRequest struct {
	outChan chan []Job
}

// NewScheduler creates a new Scheduler instance.
// The Scheduler is not started until Start() is called.
//
// NewJob will add jobs to the Scheduler, but they will not
// be scheduled until Start() is called.
func NewScheduler(options ...SchedulerOption) (Scheduler, error) {
	schCtx, cancel := context.WithCancel(context.Background())

	exec := executor{
		stopCh:           make(chan struct{}),
		stopTimeout:      time.Second * 10,
		singletonRunners: nil,
		logger:           &noOpLogger{},
		clock:            clockwork.NewRealClock(),

		jobsIn:                 make(chan jobIn),
		jobsOutForRescheduling: make(chan uuid.UUID),
		jobUpdateNextRuns:      make(chan uuid.UUID),
		jobsOutCompleted:       make(chan uuid.UUID),
		jobOutRequest:          make(chan *jobOutRequest, 100),
		done:                   make(chan error, 1),
	}

	s := &scheduler{
		shutdownCtx:    schCtx,
		shutdownCancel: cancel,
		exec:           exec,
		jobs:           make(map[uuid.UUID]internalJob),
		location:       time.Local,
		logger:         &noOpLogger{},

		newJobCh:           make(chan newJobIn),
		removeJobCh:        make(chan uuid.UUID),
		removeJobsByTagsCh: make(chan []string),
		startCh:            make(chan struct{}),
		startedCh:          make(chan struct{}),
		stopCh:             make(chan struct{}),
		stopErrCh:          make(chan error, 1),
		jobOutRequestCh:    make(chan *jobOutRequest),
		runJobRequestCh:    make(chan runJobRequest),
		allJobsOutRequest:  make(chan allJobsOutRequest),
	}

	for _, option := range options {
		err := option(s)
		if err != nil {
			return nil, err
		}
	}

	go func() {
		s.logger.Info("gocron: new scheduler created")
		for {
			select {
			case id := <-s.exec.jobsOutForRescheduling:
				s.selectExecJobsOutForRescheduling(id)
			case id := <-s.exec.jobUpdateNextRuns:
				s.updateNextScheduled(id)
			case id := <-s.exec.jobsOutCompleted:
				s.selectExecJobsOutCompleted(id)

			case in := <-s.newJobCh:
				s.selectNewJob(in)

			case id := <-s.removeJobCh:
				s.selectRemoveJob(id)

			case tags := <-s.removeJobsByTagsCh:
				s.selectRemoveJobsByTags(tags)

			case out := <-s.exec.jobOutRequest:
				s.selectJobOutRequest(out)

			case out := <-s.jobOutRequestCh:
				s.selectJobOutRequest(out)

			case out := <-s.allJobsOutRequest:
				s.selectAllJobsOutRequest(out)

			case run := <-s.runJobRequestCh:
				s.selectRunJobRequest(run)

			case <-s.startCh:
				s.selectStart()

			case <-s.stopCh:
				s.stopScheduler()

			case <-s.shutdownCtx.Done():
				s.stopScheduler()
				return
			}
		}
	}()

	return s, nil
}

// -----------------------------------------------
// -----------------------------------------------
// --------- Scheduler Channel Methods -----------
// -----------------------------------------------
// -----------------------------------------------

// The scheduler's channel functions are broken out here
// to allow prioritizing within the select blocks. The idea
// being that we want to make sure that scheduling tasks
// are not blocked by requests from the caller for information
// about jobs.

func (s *scheduler) stopScheduler() {
	s.logger.Debug("gocron: stopping scheduler")

	if s.started.Load() {
		s.exec.stopCh <- struct{}{}
	}

	for _, j := range s.jobs {
		j.stop()
	}
	for _, j := range s.jobs {
		<-j.ctx.Done()
	}
	var err error
	if s.started.Load() {
		t := time.NewTimer(s.exec.stopTimeout + 1*time.Second)
		select {
		case err = <-s.exec.done:
			t.Stop()
		case <-t.C:
			err = ErrStopExecutorTimedOut
		}
	}
	for id, j := range s.jobs {
		oldCtx := j.ctx
		if j.parentCtx == nil {
			j.parentCtx = s.shutdownCtx
		}
		j.ctx, j.cancel = context.WithCancel(j.parentCtx)

		// also replace the old context with the new one in the parameters
		if len(j.parameters) > 0 && j.parameters[0] == oldCtx {
			j.parameters[0] = j.ctx
		}

		s.jobs[id] = j
	}

	s.stopErrCh <- err
	s.started.Store(false)
	s.logger.Debug("gocron: scheduler stopped")
}

func (s *scheduler) selectAllJobsOutRequest(out allJobsOutRequest) {
	outJobs := make([]Job, len(s.jobs))
	var counter int
	for _, j := range s.jobs {
		outJobs[counter] = s.jobFromInternalJob(j)
		counter++
	}
	slices.SortFunc(outJobs, func(a, b Job) int {
		aID, bID := a.ID().String(), b.ID().String()
		return strings.Compare(aID, bID)
	})
	select {
	case <-s.shutdownCtx.Done():
	case out.outChan <- outJobs:
	}
}

func (s *scheduler) selectRunJobRequest(run runJobRequest) {
	j, ok := s.jobs[run.id]
	if !ok {
		select {
		case run.outChan <- ErrJobNotFound:
		default:
		}
		return
	}
	select {
	case <-s.shutdownCtx.Done():
		select {
		case run.outChan <- ErrJobRunNowFailed:
		default:
		}
	case s.exec.jobsIn <- jobIn{
		id:            j.id,
		shouldSendOut: false,
	}:
		select {
		case run.outChan <- nil:
		default:
		}
	}
}

func (s *scheduler) selectRemoveJob(id uuid.UUID) {
	j, ok := s.jobs[id]
	if !ok {
		return
	}
	j.stop()
	delete(s.jobs, id)
}

// Jobs coming back from the executor to the scheduler that
// need to be evaluated for rescheduling.
func (s *scheduler) selectExecJobsOutForRescheduling(id uuid.UUID) {
	select {
	case <-s.shutdownCtx.Done():
		return
	default:
	}
	j, ok := s.jobs[id]
	if !ok {
		// the job was removed while it was running, and
		// so we don't need to reschedule it.
		return
	}

	if j.stopTimeReached(s.now()) {
		return
	}

	var scheduleFrom time.Time

	// If intervalFromCompletion is enabled, calculate the next run time
	// from when the job completed (lastRun) rather than when it was scheduled.
	if j.intervalFromCompletion {
		// Use the completion time (lastRun is set when the job completes)
		scheduleFrom = j.lastRun
		if scheduleFrom.IsZero() {
			// For the first run, use the start time or current time
			scheduleFrom = j.startTime
			if scheduleFrom.IsZero() {
				scheduleFrom = s.now()
			}
		}
	} else {
		// Default behavior: use the scheduled time
		if len(j.nextScheduled) > 0 {
			// always grab the last element in the slice as that is the furthest
			// out in the future and the time from which we want to calculate
			// the subsequent next run time.
			slices.SortStableFunc(j.nextScheduled, ascendingTime)
			scheduleFrom = j.nextScheduled[len(j.nextScheduled)-1]
		}

		if scheduleFrom.IsZero() {
			scheduleFrom = j.startTime
		}
	}

	next := j.next(scheduleFrom)
	if next.IsZero() {
		// the job's next function will return zero for OneTime jobs.
		// since they are one time only, they do not need rescheduling.
		return
	}

	if next.Before(s.now()) {
		// in some cases the next run time can be in the past, for example:
		// - the time on the machine was incorrect and has been synced with ntp
		// - the machine went to sleep, and woke up some time later
		// in those cases, we want to increment to the next run in the future
		// and schedule the job for that time.
		for next.Before(s.now()) {
			next = j.next(next)
		}
	}

	if slices.Contains(j.nextScheduled, next) {
		// if the next value is a duplicate of what's already in the nextScheduled slice, for example:
		// - the job is being rescheduled off the same next run value as before
		// increment to the next, next value
		for slices.Contains(j.nextScheduled, next) {
			next = j.next(next)
		}
	}

	// Clean up any existing timer to prevent leaks
	if j.timer != nil {
		j.timer.Stop()
		j.timer = nil // Ensure timer is cleared for GC
	}

	j.nextScheduled = append(j.nextScheduled, next)
	j.timer = s.exec.clock.AfterFunc(next.Sub(s.now()), func() {
		// set the actual timer on the job here and listen for
		// shut down events so that the job doesn't attempt to
		// run if the scheduler has been shutdown.
		select {
		case <-s.shutdownCtx.Done():
			return
		case s.exec.jobsIn <- jobIn{
			id:            j.id,
			shouldSendOut: true,
		}:
		}
	})
	// update the job with its new next and last run times and timer.
	s.jobs[id] = j
}

func (s *scheduler) updateNextScheduled(id uuid.UUID) {
	j, ok := s.jobs[id]
	if !ok {
		return
	}
	var newNextScheduled []time.Time
	for _, t := range j.nextScheduled {
		if t.Before(s.now()) {
			continue
		}
		newNextScheduled = append(newNextScheduled, t)
	}
	j.nextScheduled = newNextScheduled
	s.jobs[id] = j
}

func (s *scheduler) selectExecJobsOutCompleted(id uuid.UUID) {
	j, ok := s.jobs[id]
	if !ok {
		return
	}

	// if the job has nextScheduled time in the past,
	// we need to remove any that are in the past.
	var newNextScheduled []time.Time
	for _, t := range j.nextScheduled {
		if t.Before(s.now()) {
			continue
		}
		newNextScheduled = append(newNextScheduled, t)
	}
	j.nextScheduled = newNextScheduled

	// if the job has a limited number of runs set, we need to
	// check how many runs have occurred and stop running this
	// job if it has reached the limit.
	if j.limitRunsTo != nil {
		j.limitRunsTo.runCount = j.limitRunsTo.runCount + 1
		if j.limitRunsTo.runCount == j.limitRunsTo.limit {
			go func() {
				select {
				case <-s.shutdownCtx.Done():
					return
				case s.removeJobCh <- id:
				}
			}()
			return
		}
	}

	j.lastRun = s.now()
	s.jobs[id] = j
}

func (s *scheduler) selectJobOutRequest(out *jobOutRequest) {
	if j, ok := s.jobs[out.id]; ok {
		select {
		case out.outChan <- j:
		case <-s.shutdownCtx.Done():
		}
	}
	close(out.outChan)
}

func (s *scheduler) selectNewJob(in newJobIn) {
	j := in.job
	if s.started.Load() {
		next := j.startTime
		if j.startImmediately {
			next = s.now()
			select {
			case <-s.shutdownCtx.Done():
			case s.exec.jobsIn <- jobIn{
				id:            j.id,
				shouldSendOut: true,
			}:
			}
		} else {
			if next.IsZero() {
				next = j.next(s.now())
			}

			if next.Before(s.now()) {
				for next.Before(s.now()) {
					next = j.next(next)
				}
			}

			id := j.id
			j.timer = s.exec.clock.AfterFunc(next.Sub(s.now()), func() {
				select {
				case <-s.shutdownCtx.Done():
				case s.exec.jobsIn <- jobIn{
					id:            id,
					shouldSendOut: true,
				}:
				}
			})
		}
		j.startTime = next
		j.nextScheduled = append(j.nextScheduled, next)
	}

	s.jobs[j.id] = j
	in.cancel()
}

func (s *scheduler) selectRemoveJobsByTags(tags []string) {
	for _, j := range s.jobs {
		for _, tag := range tags {
			if slices.Contains(j.tags, tag) {
				j.stop()
				delete(s.jobs, j.id)
				break
			}
		}
	}
}

func (s *scheduler) selectStart() {
	s.logger.Debug("gocron: scheduler starting")
	go s.exec.start()

	s.started.Store(true)
	for id, j := range s.jobs {
		next := j.startTime
		if j.startImmediately {
			next = s.now()
			select {
			case <-s.shutdownCtx.Done():
			case s.exec.jobsIn <- jobIn{
				id:            id,
				shouldSendOut: true,
			}:
			}
		} else {
			if next.IsZero() {
				next = j.next(s.now())
			}
			if next.Before(s.now()) {
				for next.Before(s.now()) {
					next = j.next(next)
				}
			}

			jobID := id
			j.timer = s.exec.clock.AfterFunc(next.Sub(s.now()), func() {
				select {
				case <-s.shutdownCtx.Done():
				case s.exec.jobsIn <- jobIn{
					id:            jobID,
					shouldSendOut: true,
				}:
				}
			})
		}
		j.startTime = next
		j.nextScheduled = append(j.nextScheduled, next)
		s.jobs[id] = j
	}
	select {
	case <-s.shutdownCtx.Done():
	case s.startedCh <- struct{}{}:
		s.logger.Info("gocron: scheduler started")
	}
}

// -----------------------------------------------
// -----------------------------------------------
// ------------- Scheduler Methods ---------------
// -----------------------------------------------
// -----------------------------------------------

func (s *scheduler) now() time.Time {
	return s.exec.clock.Now().In(s.location)
}

func (s *scheduler) jobFromInternalJob(in internalJob) job {
	return job{
		in.id,
		in.name,
		slices.Clone(in.tags),
		s.jobOutRequestCh,
		s.runJobRequestCh,
	}
}

func (s *scheduler) Jobs() []Job {
	outChan := make(chan []Job)
	select {
	case <-s.shutdownCtx.Done():
	case s.allJobsOutRequest <- allJobsOutRequest{outChan: outChan}:
	}

	var jobs []Job
	select {
	case <-s.shutdownCtx.Done():
	case jobs = <-outChan:
	}

	return jobs
}

func (s *scheduler) NewJob(jobDefinition JobDefinition, task Task, options ...JobOption) (Job, error) {
	return s.addOrUpdateJob(uuid.Nil, jobDefinition, task, options)
}

func (s *scheduler) verifyInterfaceVariadic(taskFunc reflect.Value, tsk task, variadicStart int) error {
	ifaceType := taskFunc.Type().In(variadicStart).Elem()
	for i := variadicStart; i < len(tsk.parameters); i++ {
		if !reflect.TypeOf(tsk.parameters[i]).Implements(ifaceType) {
			return ErrNewJobWrongTypeOfParameters
		}
	}
	return nil
}

func (s *scheduler) verifyVariadic(taskFunc reflect.Value, tsk task, variadicStart int) error {
	if err := s.verifyNonVariadic(taskFunc, tsk, variadicStart); err != nil {
		return err
	}
	parameterType := taskFunc.Type().In(variadicStart)
	parameterTypeKind := parameterType.Elem().Kind()
	if parameterTypeKind == reflect.Interface {
		return s.verifyInterfaceVariadic(taskFunc, tsk, variadicStart)
	}
	if parameterTypeKind == reflect.Pointer {
		parameterTypeKind = reflect.Indirect(reflect.ValueOf(parameterType)).Kind()
	}

	for i := variadicStart; i < len(tsk.parameters); i++ {
		argumentType := reflect.TypeOf(tsk.parameters[i])
		argumentTypeKind := argumentType.Kind()
		if argumentTypeKind == reflect.Interface || argumentTypeKind == reflect.Pointer {
			argumentTypeKind = argumentType.Elem().Kind()
		}
		if argumentTypeKind != parameterTypeKind {
			return ErrNewJobWrongTypeOfParameters
		}
	}
	return nil
}

func (s *scheduler) verifyNonVariadic(taskFunc reflect.Value, tsk task, length int) error {
	for i := 0; i < length; i++ {
		argumentType := reflect.TypeOf(tsk.parameters[i])
		t1 := argumentType.Kind()
		if t1 == reflect.Interface || t1 == reflect.Pointer {
			t1 = argumentType.Elem().Kind()
		}
		parameterType := taskFunc.Type().In(i)
		t2 := reflect.New(parameterType).Elem().Kind()
		if t2 == reflect.Interface || t2 == reflect.Pointer {
			t2 = reflect.Indirect(reflect.ValueOf(parameterType)).Kind()
		}
		if t1 != t2 {
			return ErrNewJobWrongTypeOfParameters
		}
	}
	return nil
}

func (s *scheduler) verifyParameterType(taskFunc reflect.Value, tsk task) error {
	taskFuncType := taskFunc.Type()
	isVariadic := taskFuncType.IsVariadic()
	if isVariadic {
		variadicStart := taskFuncType.NumIn() - 1
		return s.verifyVariadic(taskFunc, tsk, variadicStart)
	}
	expectedParameterLength := taskFuncType.NumIn()
	if len(tsk.parameters) != expectedParameterLength {
		return ErrNewJobWrongNumberOfParameters
	}
	return s.verifyNonVariadic(taskFunc, tsk, expectedParameterLength)
}

var contextType = reflect.TypeFor[context.Context]()

func (s *scheduler) addOrUpdateJob(id uuid.UUID, definition JobDefinition, taskWrapper Task, options []JobOption) (Job, error) {
	j := internalJob{}
	if id == uuid.Nil {
		j.id = uuid.New()
	} else {
		currentJob := requestJobCtx(s.shutdownCtx, id, s.jobOutRequestCh)
		if currentJob != nil && currentJob.id != uuid.Nil {
			select {
			case <-s.shutdownCtx.Done():
				return nil, nil
			case s.removeJobCh <- id:
				<-currentJob.ctx.Done()
			}
		}

		j.id = id
	}

	if taskWrapper == nil {
		return nil, ErrNewJobTaskNil
	}

	tsk := taskWrapper()
	taskFunc := reflect.ValueOf(tsk.function)
	for taskFunc.Kind() == reflect.Ptr {
		taskFunc = taskFunc.Elem()
	}

	if taskFunc.Kind() != reflect.Func {
		return nil, ErrNewJobTaskNotFunc
	}

	j.name = runtime.FuncForPC(taskFunc.Pointer()).Name()
	j.function = tsk.function
	j.parameters = tsk.parameters

	// apply global job options
	for _, option := range s.globalJobOptions {
		if err := option(&j, s.now()); err != nil {
			return nil, err
		}
	}

	// apply job specific options, which take precedence
	for _, option := range options {
		if err := option(&j, s.now()); err != nil {
			return nil, err
		}
	}

	if j.parentCtx == nil {
		j.parentCtx = s.shutdownCtx
	}
	j.ctx, j.cancel = context.WithCancel(j.parentCtx)

	if !taskFunc.IsZero() && taskFunc.Type().NumIn() > 0 {
		// if the first parameter is a context.Context and params have no context.Context, add current ctx to the params
		if taskFunc.Type().In(0) == contextType {
			if len(tsk.parameters) == 0 {
				tsk.parameters = []any{j.ctx}
				j.parameters = []any{j.ctx}
			} else if _, ok := tsk.parameters[0].(context.Context); !ok {
				tsk.parameters = append([]any{j.ctx}, tsk.parameters...)
				j.parameters = append([]any{j.ctx}, j.parameters...)
			}
		}
	}

	if err := s.verifyParameterType(taskFunc, tsk); err != nil {
		j.cancel()
		return nil, err
	}

	if err := definition.setup(&j, s.location, s.exec.clock.Now()); err != nil {
		j.cancel()
		return nil, err
	}

	newJobCtx, newJobCancel := context.WithCancel(context.Background())
	select {
	case <-s.shutdownCtx.Done():
		newJobCancel()
	case s.newJobCh <- newJobIn{
		ctx:    newJobCtx,
		cancel: newJobCancel,
		job:    j,
	}:
	}

	select {
	case <-newJobCtx.Done():
	case <-s.shutdownCtx.Done():
		newJobCancel()
	}

	out := s.jobFromInternalJob(j)
	return &out, nil
}

func (s *scheduler) RemoveByTags(tags ...string) {
	select {
	case <-s.shutdownCtx.Done():
	case s.removeJobsByTagsCh <- tags:
	}
}

func (s *scheduler) RemoveJob(id uuid.UUID) error {
	j := requestJobCtx(s.shutdownCtx, id, s.jobOutRequestCh)
	if j == nil || j.id == uuid.Nil {
		return ErrJobNotFound
	}
	select {
	case <-s.shutdownCtx.Done():
	case s.removeJobCh <- id:
	}

	return nil
}

func (s *scheduler) Start() {
	if s.started.Load() {
		s.logger.Warn("gocron: scheduler already started")
		return
	}

	select {
	case <-s.shutdownCtx.Done():
	case s.startCh <- struct{}{}:
		<-s.startedCh
	}
}

func (s *scheduler) StopJobs() error {
	select {
	case <-s.shutdownCtx.Done():
		return nil
	case s.stopCh <- struct{}{}:
	}

	t := time.NewTimer(s.exec.stopTimeout + 2*time.Second)
	select {
	case err := <-s.stopErrCh:
		t.Stop()
		return err
	case <-t.C:
		return ErrStopSchedulerTimedOut
	}
}

func (s *scheduler) Shutdown() error {
	s.logger.Debug("scheduler shutting down")

	s.shutdownCancel()
	if !s.started.Load() {
		return nil
	}

	t := time.NewTimer(s.exec.stopTimeout + 2*time.Second)
	select {
	case err := <-s.stopErrCh:
		t.Stop()
		return err
	case <-t.C:
		return ErrStopSchedulerTimedOut
	}
}

func (s *scheduler) Update(id uuid.UUID, jobDefinition JobDefinition, task Task, options ...JobOption) (Job, error) {
	return s.addOrUpdateJob(id, jobDefinition, task, options)
}

func (s *scheduler) JobsWaitingInQueue() int {
	if s.exec.limitMode != nil && s.exec.limitMode.mode == LimitModeWait {
		return len(s.exec.limitMode.in)
	}
	return 0
}

// -----------------------------------------------
// -----------------------------------------------
// ------------- Scheduler Options ---------------
// -----------------------------------------------
// -----------------------------------------------

// SchedulerOption defines the function for setting
// options on the Scheduler.
type SchedulerOption func(*scheduler) error

// WithClock sets the clock used by the Scheduler
// to the clock provided. See https://github.com/jonboulle/clockwork
func WithClock(clock clockwork.Clock) SchedulerOption {
	return func(s *scheduler) error {
		if clock == nil {
			return ErrWithClockNil
		}
		s.exec.clock = clock
		return nil
	}
}

// WithDistributedElector sets the elector to be used by multiple
// Scheduler instances to determine who should be the leader.
// Only the leader runs jobs, while non-leaders wait and continue
// to check if a new leader has been elected.
func WithDistributedElector(elector Elector) SchedulerOption {
	return func(s *scheduler) error {
		if elector == nil {
			return ErrWithDistributedElectorNil
		}
		s.exec.elector = elector
		return nil
	}
}

// WithDistributedLocker sets the locker to be used by multiple
// Scheduler instances to ensure that only one instance of each
// job is run.
// To disable this global locker for specific jobs, see
// WithDisabledDistributedJobLocker.
func WithDistributedLocker(locker Locker) SchedulerOption {
	return func(s *scheduler) error {
		if locker == nil {
			return ErrWithDistributedLockerNil
		}
		s.exec.locker = locker
		return nil
	}
}

// WithGlobalJobOptions sets JobOption's that will be applied to
// all jobs added to the scheduler. JobOption's set on the job
// itself will override if the same JobOption is set globally.
func WithGlobalJobOptions(jobOptions ...JobOption) SchedulerOption {
	return func(s *scheduler) error {
		s.globalJobOptions = jobOptions
		return nil
	}
}

// LimitMode defines the modes used for handling jobs that reach
// the limit provided in WithLimitConcurrentJobs
type LimitMode int

const (
	// LimitModeReschedule causes jobs reaching the limit set in
	// WithLimitConcurrentJobs or WithSingletonMode to be skipped
	// and rescheduled for the next run time rather than being
	// queued up to wait.
	LimitModeReschedule = 1

	// LimitModeWait causes jobs reaching the limit set in
	// WithLimitConcurrentJobs or WithSingletonMode to wait
	// in a queue until a slot becomes available to run.
	//
	// Note: this mode can produce unpredictable results as
	// job execution order isn't guaranteed. For example, a job that
	// executes frequently may pile up in the wait queue and be executed
	// many times back to back when the queue opens.
	//
	// Warning: do not use this mode if your jobs will continue to stack
	// up beyond the ability of the limit workers to keep up. An example of
	// what NOT to do:
	//
	//     s, _ := gocron.NewScheduler(gocron.WithLimitConcurrentJobs)
	//     s.NewJob(
	//         gocron.DurationJob(
	//				time.Second,
	//				Task{
	//					Function: func() {
	//						time.Sleep(10 * time.Second)
	//					},
	//				},
	//			),
	//      )
	LimitModeWait = 2
)

// WithLimitConcurrentJobs sets the limit and mode to be used by the
// Scheduler for limiting the number of jobs that may be running at
// a given time.
//
// Note: the limit mode selected for WithLimitConcurrentJobs takes initial
// precedence in the event you are also running a limit mode at the job level
// using WithSingletonMode.
//
// Warning: a single time consuming job can dominate your limit in the event
// you are running both the scheduler limit WithLimitConcurrentJobs(1, LimitModeWait)
// and a job limit WithSingletonMode(LimitModeReschedule).
func WithLimitConcurrentJobs(limit uint, mode LimitMode) SchedulerOption {
	return func(s *scheduler) error {
		if limit == 0 {
			return ErrWithLimitConcurrentJobsZero
		}
		s.exec.limitMode = &limitModeConfig{
			mode:          mode,
			limit:         limit,
			in:            make(chan jobIn, 1000),
			singletonJobs: make(map[uuid.UUID]struct{}),
		}
		if mode == LimitModeReschedule {
			s.exec.limitMode.rescheduleLimiter = make(chan struct{}, limit)
		}
		return nil
	}
}

// WithLocation sets the location (i.e. timezone) that the scheduler
// should operate within. In many systems time.Local is UTC.
// Default: time.Local
func WithLocation(location *time.Location) SchedulerOption {
	return func(s *scheduler) error {
		if location == nil {
			return ErrWithLocationNil
		}
		s.location = location
		return nil
	}
}

// WithLogger sets the logger to be used by the Scheduler.
func WithLogger(logger Logger) SchedulerOption {
	return func(s *scheduler) error {
		if logger == nil {
			return ErrWithLoggerNil
		}
		s.logger = logger
		s.exec.logger = logger
		return nil
	}
}

// WithStopTimeout sets the amount of time the Scheduler should
// wait gracefully for jobs to complete before returning when
// StopJobs() or Shutdown() are called.
// Default: 10 * time.Second
func WithStopTimeout(timeout time.Duration) SchedulerOption {
	return func(s *scheduler) error {
		if timeout <= 0 {
			return ErrWithStopTimeoutZeroOrNegative
		}
		s.exec.stopTimeout = timeout
		return nil
	}
}

// WithMonitor sets the metrics provider to be used by the Scheduler.
func WithMonitor(monitor Monitor) SchedulerOption {
	return func(s *scheduler) error {
		if monitor == nil {
			return ErrWithMonitorNil
		}
		s.exec.monitor = monitor
		return nil
	}
}

// WithMonitorStatus sets the metrics provider to be used by the Scheduler.
func WithMonitorStatus(monitor MonitorStatus) SchedulerOption {
	return func(s *scheduler) error {
		if monitor == nil {
			return ErrWithMonitorNil
		}
		s.exec.monitorStatus = monitor
		return nil
	}
}
