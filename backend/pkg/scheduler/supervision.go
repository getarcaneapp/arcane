package scheduler

import (
	"context"
	"crypto/rand"
	"math/big"
	"sync"
	"time"

	"emperror.dev/errors"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
)

type watcherSupervisorInternal struct {
	watcher schedulertypes.BusWatcher
	mu      sync.Mutex
	health  schedulertypes.WorkerHealth
	restart chan struct{}
	cancel  context.CancelFunc
}

func newWatcherSupervisorInternal(watcher schedulertypes.BusWatcher) *watcherSupervisorInternal {
	return &watcherSupervisorInternal{watcher: watcher, restart: make(chan struct{}, 1), health: schedulertypes.WorkerHealth{Status: "starting", UpdatedAt: time.Now()}}
}

func (s *watcherSupervisorInternal) setHealthInternal(status string, err error, next *time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.health = schedulertypes.WorkerHealth{Status: status, NextRetry: next, UpdatedAt: time.Now()}
	if err != nil {
		s.health.LastError = err.Error()
	}
}

func (s *watcherSupervisorInternal) runInternal(ctx context.Context) error {
	delay := 5 * time.Second
	unexpected := 0
	for ctx.Err() == nil {
		runCtx, cancel := context.WithCancel(ctx)
		s.mu.Lock()
		s.cancel = cancel
		s.mu.Unlock()
		s.setHealthInternal("running", nil, nil)
		started := time.Now()
		panicked, err := s.startInternal(runCtx)
		cancel()
		if ctx.Err() != nil {
			break
		}
		select {
		case <-s.restart:
			unexpected = 0
			delay = 5 * time.Second
			continue
		default:
		}
		if panicked || err == nil {
			unexpected++
		}
		if err == nil {
			err = errors.New("worker exited unexpectedly")
		}
		if unexpected >= 3 {
			s.setHealthInternal("needs_attention", err, nil)
			select {
			case <-ctx.Done():
			case <-s.restart:
				unexpected = 0
				delay = 5 * time.Second
			}
			continue
		}
		if time.Since(started) > 5*time.Minute {
			delay = 5 * time.Second
			unexpected = 0
		}
		jitter, jitterErr := rand.Int(rand.Reader, big.NewInt(int64(delay/2)))
		if jitterErr != nil {
			return errors.WrapIf(jitterErr, "generate watcher retry delay")
		}
		next := time.Now().Add(delay/2 + time.Duration(jitter.Int64()))
		s.setHealthInternal("retrying", err, &next)
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
		case <-s.restart:
			unexpected = 0
			delay = 5 * time.Second
		case <-timer.C:
			delay = min(2*delay, 5*time.Minute)
		}
		timer.Stop()
	}
	s.setHealthInternal("stopped", nil, nil)
	return ctx.Err()
}

func (s *watcherSupervisorInternal) startInternal(ctx context.Context) (panicked bool, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = errors.Errorf("worker panic: %v", recovered)
			panicked = true
		}
	}()
	return false, s.watcher.Start(ctx)
}

func (js *jobSchedulerInternal) SetDispatcher(dispatcher schedulertypes.Dispatcher) {
	js.dispatchMu.Lock()
	defer js.dispatchMu.Unlock()
	js.dispatcher = dispatcher
}

func (js *jobSchedulerInternal) dispatcherInternal() schedulertypes.Dispatcher {
	js.dispatchMu.RLock()
	defer js.dispatchMu.RUnlock()
	return js.dispatcher
}

func (js *jobSchedulerInternal) Submit(ctx context.Context, request schedulertypes.Request) (schedulertypes.Run, error) {
	if js.context.Err() != nil {
		return schedulertypes.Run{}, errJobSchedulerStoppedInternal
	}
	dispatcher := js.dispatcherInternal()
	if dispatcher == nil {
		return schedulertypes.Run{}, errors.New("durable job dispatcher unavailable")
	}
	return dispatcher.Submit(ctx, request)
}

func (js *jobSchedulerInternal) checkpointInternal(ctx context.Context, jobID, schedule string, next time.Time) error {
	dispatcher := js.dispatcherInternal()
	if dispatcher == nil {
		return errors.New("durable job dispatcher unavailable")
	}
	return dispatcher.Checkpoint(ctx, jobID, schedule, next)
}

func (js *jobSchedulerInternal) ListRegisteredJobs() []schedulertypes.Job {
	snapshot, ok := js.state.Load()
	if !ok {
		return nil
	}
	jobs := make([]schedulertypes.Job, 0, len(snapshot.jobsByID))
	for _, job := range snapshot.jobsByID {
		jobs = append(jobs, job)
	}
	return jobs
}

func (js *jobSchedulerInternal) RestartWatcher(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	snapshot, ok := js.state.Load()
	if !ok || snapshot.stopping {
		return errJobSchedulerStoppedInternal
	}
	supervisor, ok := snapshot.supervisors[id]
	if !ok {
		return errors.New("watcher not found")
	}
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	select {
	case supervisor.restart <- struct{}{}:
	default:
	}
	if supervisor.cancel != nil {
		supervisor.cancel()
	}
	return nil
}

func (js *jobSchedulerInternal) WatcherHealth(id string) (schedulertypes.WorkerHealth, bool) {
	snapshot, ok := js.state.Load()
	if !ok {
		return schedulertypes.WorkerHealth{}, false
	}
	supervisor, ok := snapshot.supervisors[id]
	if !ok {
		return schedulertypes.WorkerHealth{}, false
	}
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	health := supervisor.health
	if watcher, ok := supervisor.watcher.(*ImageUpdateWatcher); ok && watcher.eventDegraded.Load() && health.Status == "running" {
		health.Status = "degraded"
		health.LastError = "Docker image event subscription unavailable; polling remains active"
	}
	return health, true
}
