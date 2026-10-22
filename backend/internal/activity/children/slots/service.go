// Package slots bounds how many queue-opted activities run concurrently per
// environment and tracks which activity holds each slot.
package slots

import (
	"context"
	"sync"
	"time"

	"github.com/samber/mo"

	"github.com/getarcaneapp/arcane/backend/v3/internal/settings"
)

const (
	maxConcurrentActivitiesSettingKey = "maxConcurrentActivities"
	defaultMaxConcurrentActivities    = 5
	// slotWaitRecheckInterval bounds how long a queued waiter can miss a limit
	// increase made while no slots were being released.
	slotWaitRecheckInterval = 2 * time.Second
)

// Service reads the limit from settings on every acquisition attempt, while
// held slots are counted independently of it — so changing the limit
// mid-flight never miscounts running work: existing holders keep occupying
// capacity until they release, and waiters are re-evaluated against the new
// limit.
type Service struct {
	settings *settings.SettingsService

	mu    sync.Mutex
	state map[string]*environmentSlots

	// releases maps an activity ID to the release func of the slot it holds;
	// the slot is freed when the activity completes.
	releasesMu sync.Mutex
	releases   map[string]func()
}

type environmentSlots struct {
	held int
	// wake is closed (and replaced) on every release to broadcast to waiters;
	// each re-checks against the current limit, so a raised limit can admit
	// more than one of them.
	wake chan struct{}
}

// NewService returns a slot limiter sized from the local settings.
func NewService(localSettings *settings.SettingsService) *Service {
	return &Service{
		settings: localSettings,
		state:    map[string]*environmentSlots{},
		releases: map[string]func(){},
	}
}

func (s *Service) stateLocked(environmentID string) *environmentSlots {
	slots := s.state[environmentID]
	if slots == nil {
		slots = &environmentSlots{wake: make(chan struct{})}
		s.state[environmentID] = slots
	}
	return slots
}

// TryAcquire grabs a slot without blocking; None means the caller should
// queue and block via Await. Held slots are counted even while the limit is
// unlimited, so enabling a limit later still sees the work already running.
func (s *Service) TryAcquire(ctx context.Context, environmentID string) mo.Option[func()] {
	if s.settings == nil {
		return mo.Some(func() {})
	}
	limit := s.settings.GetIntSetting(ctx, maxConcurrentActivitiesSettingKey)

	s.mu.Lock()
	defer s.mu.Unlock()
	slots := s.stateLocked(environmentID)
	if limit > 0 && slots.held >= limit {
		return mo.None[func()]()
	}
	slots.held++
	return mo.Some(s.releaser(environmentID))
}

// Await blocks until activityID holds a slot or ctx is cancelled, reporting
// whether a slot was newly acquired. The limit is re-read on every attempt so
// setting changes apply to queued waiters.
func (s *Service) Await(ctx context.Context, activityID, environmentID string) (bool, error) {
	s.releasesMu.Lock()
	_, held := s.releases[activityID]
	s.releasesMu.Unlock()
	if held {
		return false, nil
	}

	for s.settings != nil {
		limit := s.settings.GetIntSetting(ctx, maxConcurrentActivitiesSettingKey)
		s.mu.Lock()
		slots := s.stateLocked(environmentID)
		if limit <= 0 || slots.held < limit {
			slots.held++
			s.mu.Unlock()
			s.Register(activityID, s.releaser(environmentID))
			return true, nil
		}
		wake := slots.wake
		s.mu.Unlock()

		timer := time.NewTimer(slotWaitRecheckInterval)
		select {
		case <-wake:
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return false, context.Cause(ctx)
		}
		timer.Stop()
	}
	s.Register(activityID, func() {})
	return true, nil
}

// Register records release as the slot held by activityID, freeing any slot
// it previously held.
func (s *Service) Register(activityID string, release func()) {
	s.releasesMu.Lock()
	if existing, ok := s.releases[activityID]; ok {
		existing()
	}
	s.releases[activityID] = release
	s.releasesMu.Unlock()
}

// Release frees the slot held by activityID, if any.
func (s *Service) Release(activityID string) {
	s.releasesMu.Lock()
	release, ok := s.releases[activityID]
	if ok {
		delete(s.releases, activityID)
	}
	s.releasesMu.Unlock()
	if ok {
		release()
	}
}

func (s *Service) releaser(environmentID string) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			slots := s.stateLocked(environmentID)
			if slots.held > 0 {
				slots.held--
			}
			close(slots.wake)
			slots.wake = make(chan struct{})
			s.mu.Unlock()
		})
	}
}
