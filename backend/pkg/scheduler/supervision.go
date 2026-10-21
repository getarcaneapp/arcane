package scheduler

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
)

type watcherSupervisorInternal struct {
	watcher types.BusWatcher
	mu      sync.Mutex
	health  types.SchedulerWorkerHealth
	restart chan struct{}
	cancel  context.CancelFunc
}

func newWatcherSupervisor(watcher types.BusWatcher) *watcherSupervisorInternal {
	return &watcherSupervisorInternal{watcher: watcher, restart: make(chan struct{}, 1), health: types.SchedulerWorkerHealth{Status: "starting", UpdatedAt: time.Now()}}
}

func (s *watcherSupervisorInternal) setHealth(status string, err error, next *time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.health = types.SchedulerWorkerHealth{Status: status, NextRetry: next, UpdatedAt: time.Now()}
	if err != nil {
		s.health.LastError = err.Error()
	}
}

func (s *watcherSupervisorInternal) run(ctx context.Context) error {
	delay := 5 * time.Second
	unexpected := 0
	for ctx.Err() == nil {
		runCtx, cancel := context.WithCancel(ctx)
		s.mu.Lock()
		s.cancel = cancel
		s.mu.Unlock()
		s.setHealth("running", nil, nil)
		started := time.Now()
		panicked, err := s.start(runCtx)
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
			s.setHealth("needs_attention", err, nil)
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
			return fmt.Errorf("generate watcher retry delay: %w", jitterErr)
		}
		next := time.Now().Add(delay/2 + time.Duration(jitter.Int64()))
		s.setHealth("retrying", err, &next)
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
	s.setHealth("stopped", nil, nil)
	return ctx.Err()
}

func (s *watcherSupervisorInternal) start(ctx context.Context) (panicked bool, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("worker panic: %v", recovered)
			panicked = true
		}
	}()
	return false, s.watcher.Start(ctx)
}

func (js *jobSchedulerInternal) Submit(ctx context.Context, request types.SchedulerRequest) (types.SchedulerRun, error) {
	if js.context.Err() != nil {
		return types.SchedulerRun{}, errJobSchedulerStoppedInternal
	}
	return js.coordinator.Submit(ctx, request)
}

func (js *jobSchedulerInternal) ListRegisteredJobs() []types.SchedulerJob {
	js.mu.RLock()
	defer js.mu.RUnlock()
	jobs := make([]types.SchedulerJob, 0, len(js.jobsByID))
	for _, job := range js.jobsByID {
		jobs = append(jobs, job)
	}
	return jobs
}

func (js *jobSchedulerInternal) RestartWatcher(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	js.mu.RLock()
	supervisor, ok := js.supervisors[id]
	stopping := js.stopping
	js.mu.RUnlock()
	if stopping {
		return errJobSchedulerStoppedInternal
	}
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

func (js *jobSchedulerInternal) WatcherHealth(id string) (types.SchedulerWorkerHealth, bool) {
	js.mu.RLock()
	supervisor, ok := js.supervisors[id]
	js.mu.RUnlock()
	if !ok {
		return types.SchedulerWorkerHealth{}, false
	}
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	health := supervisor.health
	if watcher, localOk := supervisor.watcher.(*ImageUpdateWatcher); localOk && watcher.eventDegraded.Load() && health.Status == "running" {
		health.Status = "degraded"
		health.LastError = "Docker image event subscription unavailable; polling remains active"
	}
	return health, true
}
