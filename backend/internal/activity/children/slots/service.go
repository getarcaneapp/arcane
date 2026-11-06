package slots

import (
	"context"
	"sync"
	"time"

	"github.com/samber/mo"

	"getarcane.dev/arcane/v3/internal/settings"
)

type (
	Service struct {
		settings *settings.SettingsService

		state    map[string]*environmentSlots
		releases map[string]func()

		mu         sync.Mutex
		releasesMu sync.Mutex
	}

	environmentSlots struct {
		wake chan struct{}
		held int
	}
)

const (
	maxConcurrentActivitiesSettingKey = "maxConcurrentActivities"
	slotWaitRecheckInterval           = 2 * time.Second
)

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

func (s *Service) TryAcquire(ctx context.Context, environmentID string) mo.Option[func()] {
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

func (s *Service) Await(ctx context.Context, activityID, environmentID string) (bool, error) {
	s.releasesMu.Lock()
	_, held := s.releases[activityID]
	s.releasesMu.Unlock()
	if held {
		return false, nil
	}

	for {
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
}

func (s *Service) Register(activityID string, release func()) {
	s.releasesMu.Lock()
	if existing, ok := s.releases[activityID]; ok {
		existing()
	}
	s.releases[activityID] = release
	s.releasesMu.Unlock()
}

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
