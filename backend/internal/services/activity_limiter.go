package services

import (
	"context"
	"sync"
	"time"
)

const (
	maxConcurrentActivitiesSettingKey = "maxConcurrentActivities"
	defaultMaxConcurrentActivities    = 5
	// slotWaitRecheckInterval bounds how long a queued waiter can miss a limit
	// increase made while no slots were being released.
	slotWaitRecheckInterval = 2 * time.Second
)

// activitySlotLimiter bounds how many queue-opted activities run concurrently
// per environment. The limit is read from settings on every acquisition
// attempt, while held slots are counted independently of it — so changing the
// limit mid-flight never miscounts running work: existing holders keep
// occupying capacity until they release, and waiters are re-evaluated against
// the new limit.
type activitySlotLimiter struct {
	settings *SettingsService

	mu    sync.Mutex
	state map[string]*activityEnvironmentSlots
}

type activityEnvironmentSlots struct {
	held int
	// wake is closed (and replaced) on every release to broadcast to waiters;
	// each re-checks against the current limit, so a raised limit can admit
	// more than one of them.
	wake chan struct{}
}

func newActivitySlotLimiterInternal(settings *SettingsService) *activitySlotLimiter {
	return &activitySlotLimiter{
		settings: settings,
		state:    map[string]*activityEnvironmentSlots{},
	}
}

func (l *activitySlotLimiter) limitInternal(ctx context.Context) int {
	return l.settings.GetIntSetting(ctx, maxConcurrentActivitiesSettingKey, defaultMaxConcurrentActivities)
}

func (l *activitySlotLimiter) stateForLockedInternal(environmentID string) *activityEnvironmentSlots {
	slots := l.state[environmentID]
	if slots == nil {
		slots = &activityEnvironmentSlots{wake: make(chan struct{})}
		l.state[environmentID] = slots
	}
	return slots
}

// tryAcquireInternal grabs a slot without blocking; ok=false means the caller
// should queue and block via acquireInternal. Held slots are counted even
// while the limit is unlimited, so enabling a limit later still sees the work
// already running.
func (l *activitySlotLimiter) tryAcquireInternal(ctx context.Context, environmentID string) (func(), bool) {
	if l == nil || l.settings == nil {
		return func() {}, true
	}
	limit := l.limitInternal(ctx)

	l.mu.Lock()
	defer l.mu.Unlock()
	slots := l.stateForLockedInternal(environmentID)
	if limit > 0 && slots.held >= limit {
		return nil, false
	}
	slots.held++
	return l.releaseOnceInternal(environmentID), true
}

// acquireInternal blocks until a slot frees or ctx is cancelled. The limit is
// re-read on every attempt so setting changes apply to queued waiters.
func (l *activitySlotLimiter) acquireInternal(ctx context.Context, environmentID string) (func(), error) {
	if l == nil || l.settings == nil {
		return func() {}, nil
	}

	for {
		limit := l.limitInternal(ctx)
		l.mu.Lock()
		slots := l.stateForLockedInternal(environmentID)
		if limit <= 0 || slots.held < limit {
			slots.held++
			l.mu.Unlock()
			return l.releaseOnceInternal(environmentID), nil
		}
		wake := slots.wake
		l.mu.Unlock()

		timer := time.NewTimer(slotWaitRecheckInterval)
		select {
		case <-wake:
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return nil, context.Cause(ctx)
		}
		timer.Stop()
	}
}

func (l *activitySlotLimiter) releaseOnceInternal(environmentID string) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			slots := l.stateForLockedInternal(environmentID)
			if slots.held > 0 {
				slots.held--
			}
			close(slots.wake)
			slots.wake = make(chan struct{})
			l.mu.Unlock()
		})
	}
}
