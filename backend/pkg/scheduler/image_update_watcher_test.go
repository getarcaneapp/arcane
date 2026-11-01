package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/moby/moby/api/types/events"
	"github.com/stretchr/testify/require"
	"kit.getarcane.app/docker/streams/bus"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/scheduler/jobcontext"
)

type (
	imageUpdateScannerFake struct {
		startedCh chan int
		releaseCh <-chan struct{}
		errors    []error
		panics    []bool
		calls     int
		active    int
		maxActive int
		mu        sync.Mutex
	}

	pollingSettingReaderFake struct {
		schedule            string
		mu                  sync.RWMutex
		enabled             bool
		eventWatcherEnabled bool
	}

	dockerEventBusProviderFake struct {
		eventBus *bus.DockerEventBus
	}

	projectImageRefsBackfillerFake struct {
		run     func(ctx context.Context, call int) (int, error)
		started chan int
		calls   int
		mu      sync.Mutex
	}
)

func (s *imageUpdateScannerFake) RunImageCheck(ctx context.Context, _ types.ImageUpdateCheckRequest) (types.SchedulerOutcome, error) {
	s.mu.Lock()
	s.calls++
	call := s.calls
	s.active++
	s.maxActive = max(s.maxActive, s.active)
	var err error
	if call <= len(s.errors) {
		err = s.errors[call-1]
	}
	shouldPanic := call <= len(s.panics) && s.panics[call-1]
	startedCh := s.startedCh
	releaseCh := s.releaseCh
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.active--
		s.mu.Unlock()
	}()

	if startedCh != nil {
		select {
		case startedCh <- call:
		default:
		}
	}
	if releaseCh != nil {
		select {
		case <-ctx.Done():
			err = ctx.Err()
		case <-releaseCh:
		}
	}
	if shouldPanic {
		panic("deliberate image scan panic")
	}

	return types.SchedulerOutcome{Status: types.RunStatusSucceeded}, err
}

func (s *imageUpdateScannerFake) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *imageUpdateScannerFake) peakActive() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.maxActive
}

func (s *pollingSettingReaderFake) GetBoolSetting(_ context.Context, key string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	switch key {
	case "pollingEnabled":
		return s.enabled
	case "imageEventWatcherEnabled":
		return s.eventWatcherEnabled
	default:
		return false
	}
}

func (s *pollingSettingReaderFake) GetStringSetting(_ context.Context, _ string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.schedule
}

func (s *pollingSettingReaderFake) setEnabled(enabled bool) {
	s.mu.Lock()
	s.enabled = enabled
	s.mu.Unlock()
}

func (p dockerEventBusProviderFake) EventBus() *bus.DockerEventBus {
	return p.eventBus
}

func (b *projectImageRefsBackfillerFake) BackfillProjectImageRefs(ctx context.Context) (int, error) {
	b.mu.Lock()
	b.calls++
	call := b.calls
	run := b.run
	started := b.started
	b.mu.Unlock()

	if started != nil {
		select {
		case started <- call:
		default:
		}
	}
	if run == nil {
		return 0, nil
	}
	return run(ctx, call)
}

func (b *projectImageRefsBackfillerFake) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls
}

func newImageUpdateWatcherForTest(t *testing.T,
	scanner imageUpdateScanner,
	settings pollingSettingReader,
	eventBus *bus.DockerEventBus,
	backfiller projectImageRefsBackfiller,
) *ImageUpdateWatcher {
	t.Helper()
	if backfiller == nil {
		backfiller = &projectImageRefsBackfillerFake{}
	}
	watcher := &ImageUpdateWatcher{
		imageUpdateService: scanner,
		settingsService:    settings,
		dockerService:      dockerEventBusProviderFake{eventBus: eventBus},
		projectService:     backfiller,
		trigger:            make(chan struct{}, 1),
		scheduleRefresh:    make(chan struct{}, 1),
		debounce:           10 * time.Millisecond,
		backfillRetry:      10 * time.Millisecond,
		metadataReady:      make(chan struct{}),
		stopped:            make(chan struct{}),
	}
	coordinator, _ := newTestCoordinator(t, t.Context(), time.UTC, func(ctx context.Context, run types.SchedulerRun) (types.SchedulerOutcome, error) {
		err := watcher.RunNow(jobcontext.WithExecution(ctx, run, nil))
		if err != nil {
			return types.SchedulerOutcome{Status: types.RunStatusFailed, Message: err.Error()}, nil
		}
		return types.SchedulerOutcome{Status: types.RunStatusSucceeded}, nil
	})
	watcher.SetCoordinator(coordinator)
	return watcher
}

func startImageUpdateWatcherForTest(t *testing.T, watcher *ImageUpdateWatcher) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	errCh := make(chan error, 1)
	go func() {
		errCh <- watcher.Start(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		require.NoError(t, <-errCh)
	})
}

func TestImageUpdateWatcher_StartScansAtStartupAndCoalescesAllImageEvents(t *testing.T) {
	scanner := &imageUpdateScannerFake{}
	settings := &pollingSettingReaderFake{enabled: true, eventWatcherEnabled: true}
	eventBus := bus.NewDockerEventBus(bus.Config{})
	watcher := newImageUpdateWatcherForTest(t, scanner, settings, eventBus, nil)
	startImageUpdateWatcherForTest(t, watcher)

	require.Eventually(t, func() bool { return scanner.count() == 1 }, time.Second, 5*time.Millisecond)

	actions := []events.Action{
		events.ActionPull,
		events.ActionCreate,
		events.ActionCommit,
		events.ActionImport,
		events.ActionLoad,
		events.ActionTag,
		events.ActionUnTag,
		events.ActionPrune,
		events.ActionDelete,
		events.ActionPush,
		events.ActionSave,
		events.Action("future-image-action"),
	}
	for _, action := range actions {
		eventBus.Publish(events.Message{Type: events.ImageEventType, Action: action})
	}

	require.Eventually(t, func() bool { return scanner.count() == 2 }, time.Second, 5*time.Millisecond)
	time.Sleep(30 * time.Millisecond)
	require.Equal(t, 2, scanner.count())
}

func TestImageUpdateWatcher_EventTriggersAreOptIn(t *testing.T) {
	scanner := &imageUpdateScannerFake{}
	settings := &pollingSettingReaderFake{enabled: true}
	eventBus := bus.NewDockerEventBus(bus.Config{})
	watcher := newImageUpdateWatcherForTest(t, scanner, settings, eventBus, nil)
	startImageUpdateWatcherForTest(t, watcher)

	require.Eventually(t, func() bool { return scanner.count() == 1 }, time.Second, 5*time.Millisecond)
	eventBus.Publish(events.Message{Type: events.ImageEventType, Action: events.ActionPull})
	require.Never(t, func() bool { return scanner.count() > 1 }, 50*time.Millisecond, 5*time.Millisecond)
}

func TestImageUpdateWatcher_TrailingEdgeDebounceExtendsWithNewTriggers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		watcher := &ImageUpdateWatcher{debounce: 50 * time.Millisecond, trigger: make(chan struct{}, 1)}
		timer := time.NewTimer(watcher.debounce)
		defer timer.Stop()
		watcher.Trigger()
		time.Sleep(30 * time.Millisecond)
		watcher.Trigger()
		<-timer.C
		require.False(t, watcher.scanAdmissionReady(timer, true, false))
		time.Sleep(10 * time.Millisecond)
		require.False(t, watcher.scanAdmissionReady(timer, true, false))
		<-timer.C
		require.True(t, watcher.scanAdmissionReady(timer, true, false))
	})
}

func TestImageUpdateWatcher_EventDuringScanQueuesOneSerializedFollowUp(t *testing.T) {
	releaseCh := make(chan struct{})
	scanner := &imageUpdateScannerFake{
		startedCh: make(chan int, 4),
		releaseCh: releaseCh,
	}
	settings := &pollingSettingReaderFake{enabled: true, eventWatcherEnabled: true}
	eventBus := bus.NewDockerEventBus(bus.Config{})
	watcher := newImageUpdateWatcherForTest(t, scanner, settings, eventBus, nil)
	startImageUpdateWatcherForTest(t, watcher)

	require.Equal(t, 1, <-scanner.startedCh)
	for range 20 {
		eventBus.Publish(events.Message{Type: events.ImageEventType, Action: events.ActionTag})
	}
	close(releaseCh)

	require.Equal(t, 2, <-scanner.startedCh)
	time.Sleep(30 * time.Millisecond)
	require.Equal(t, 2, scanner.count())
	require.Equal(t, 1, scanner.peakActive())
}

func TestImageUpdateWatcher_DisabledTriggersAreSkippedUntilEnabled(t *testing.T) {
	scanner := &imageUpdateScannerFake{}
	settings := &pollingSettingReaderFake{enabled: false}
	eventBus := bus.NewDockerEventBus(bus.Config{})
	watcher := newImageUpdateWatcherForTest(t, scanner, settings, eventBus, nil)
	startImageUpdateWatcherForTest(t, watcher)

	time.Sleep(30 * time.Millisecond)
	require.Zero(t, scanner.count())

	eventBus.Publish(events.Message{Type: events.ImageEventType, Action: events.ActionPull})
	time.Sleep(30 * time.Millisecond)
	require.Zero(t, scanner.count())

	settings.setEnabled(true)
	watcher.Trigger()
	require.Eventually(t, func() bool { return scanner.count() == 1 }, time.Second, 5*time.Millisecond)
}

func TestImageUpdateWatcher_ScanErrorDoesNotStopFutureEvents(t *testing.T) {
	scanner := &imageUpdateScannerFake{errors: []error{errors.New("registry unavailable")}}
	settings := &pollingSettingReaderFake{enabled: true, eventWatcherEnabled: true}
	eventBus := bus.NewDockerEventBus(bus.Config{})
	watcher := newImageUpdateWatcherForTest(t, scanner, settings, eventBus, nil)
	startImageUpdateWatcherForTest(t, watcher)

	require.Eventually(t, func() bool { return scanner.count() == 1 }, time.Second, 5*time.Millisecond)
	eventBus.Publish(events.Message{Type: events.ImageEventType, Action: events.ActionPull})
	require.Eventually(t, func() bool { return scanner.count() == 2 }, time.Second, 5*time.Millisecond)
}

func TestImageUpdateWatcher_RunNowReturnsInProgressErrorDuringActiveScan(t *testing.T) {
	releaseCh := make(chan struct{})
	scanner := &imageUpdateScannerFake{
		startedCh: make(chan int, 1),
		releaseCh: releaseCh,
	}
	settings := &pollingSettingReaderFake{enabled: true}
	watcher := newImageUpdateWatcherForTest(t, scanner, settings, bus.NewDockerEventBus(bus.Config{}), nil)
	startImageUpdateWatcherForTest(t, watcher)
	require.Equal(t, 1, <-scanner.startedCh)

	require.ErrorIs(t, watcher.RunNow(t.Context()), common.ErrImageScanInProgress)
	require.Equal(t, 1, scanner.count())

	close(releaseCh)
	require.Eventually(t, func() bool {
		return watcher.RunNow(t.Context()) == nil
	}, time.Second, time.Millisecond)
	require.Equal(t, 2, scanner.count())
	require.Equal(t, 1, scanner.peakActive())
}

func TestImageUpdateWatcher_RunNowReturnsContainedScanPanic(t *testing.T) {
	scanner := &imageUpdateScannerFake{panics: []bool{false, true}}
	settings := &pollingSettingReaderFake{enabled: true}
	watcher := newImageUpdateWatcherForTest(t, scanner, settings, bus.NewDockerEventBus(bus.Config{}), nil)
	startImageUpdateWatcherForTest(t, watcher)
	// Wait for durable startup admission and the startup scan itself to finish,
	// not just for the scanner to start; RunNow rejects a scan still running.
	require.Eventually(t, func() bool {
		return scanner.count() == 1 && watcher.triggerGeneration.Load() == watcher.acknowledgedGeneration.Load() && !watcher.scanRunning.Load()
	}, time.Second, time.Millisecond)

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	require.ErrorContains(t, watcher.RunNow(ctx), "image update scan worker panicked")
	require.Equal(t, 2, scanner.count())
}

func TestImageUpdateWatcher_TriggeredScanRetriesAfterManualScanFinishes(t *testing.T) {
	releaseCh := make(chan struct{})
	scanner := &imageUpdateScannerFake{
		startedCh: make(chan int, 4),
		releaseCh: releaseCh,
	}
	settings := &pollingSettingReaderFake{enabled: true, eventWatcherEnabled: true}
	eventBus := bus.NewDockerEventBus(bus.Config{})
	watcher := newImageUpdateWatcherForTest(t, scanner, settings, eventBus, nil)
	startImageUpdateWatcherForTest(t, watcher)

	// Let the startup scan finish so the gate is free.
	require.Equal(t, 1, <-scanner.startedCh)
	releaseCh <- struct{}{}
	// A manual scan takes the gate, then a Docker event fires: the triggered
	// loop must wait out the manual scan and still run its own scan after.
	manualErrCh := make(chan error, 1)
	go func() {
		for {
			err := watcher.RunNow(t.Context())
			if errors.Is(err, common.ErrImageScanInProgress) {
				time.Sleep(time.Millisecond)
				continue
			}
			manualErrCh <- err
			return
		}
	}()
	require.Equal(t, 2, <-scanner.startedCh)
	eventBus.Publish(events.Message{Type: events.ImageEventType, Action: events.ActionPull})

	time.Sleep(30 * time.Millisecond)
	require.Equal(t, 2, scanner.count())
	releaseCh <- struct{}{}
	require.NoError(t, <-manualErrCh)

	require.Equal(t, 3, <-scanner.startedCh)
	releaseCh <- struct{}{}
	require.Equal(t, 1, scanner.peakActive())
}

func TestImageUpdateWatcher_BackfillRetriesContainedPanic(t *testing.T) {
	backfiller := &projectImageRefsBackfillerFake{
		run: func(_ context.Context, call int) (int, error) {
			if call == 1 {
				panic("deliberate backfill panic")
			}
			return 1, nil
		},
	}
	scanner := &imageUpdateScannerFake{}
	settings := &pollingSettingReaderFake{enabled: true}
	watcher := newImageUpdateWatcherForTest(t, scanner, settings, bus.NewDockerEventBus(bus.Config{}), backfiller)
	startImageUpdateWatcherForTest(t, watcher)

	require.Eventually(t, func() bool { return backfiller.count() == 2 }, time.Second, time.Millisecond)
	require.Eventually(t, func() bool { return scanner.count() == 1 }, time.Second, time.Millisecond)
}

func TestImageUpdateWatcher_BackfillFailureRetriesBeforeScanning(t *testing.T) {
	secondAttemptStarted := make(chan struct{})
	releaseSecondAttempt := make(chan struct{})
	backfiller := &projectImageRefsBackfillerFake{
		run: func(ctx context.Context, call int) (int, error) {
			if call == 1 {
				return 0, fmt.Errorf("database statement timeout: %w", context.DeadlineExceeded)
			}
			close(secondAttemptStarted)
			select {
			case <-ctx.Done():
				return 0, ctx.Err()
			case <-releaseSecondAttempt:
				return 42, nil
			}
		},
	}
	scanner := &imageUpdateScannerFake{}
	settings := &pollingSettingReaderFake{enabled: true}
	watcher := newImageUpdateWatcherForTest(t, scanner, settings, bus.NewDockerEventBus(bus.Config{}), backfiller)
	startImageUpdateWatcherForTest(t, watcher)

	select {
	case <-secondAttemptStarted:
	case <-time.After(time.Second):
		require.FailNow(t, "backfill was not retried")
	}
	require.Zero(t, scanner.count())
	close(releaseSecondAttempt)

	require.Eventually(t, func() bool { return scanner.count() == 1 }, time.Second, 5*time.Millisecond)
	require.Equal(t, 2, backfiller.count())
}

func TestImageUpdateWatcher_CancellationStopsBackfillWithoutScanning(t *testing.T) {
	backfillStarted := make(chan struct{})
	backfiller := &projectImageRefsBackfillerFake{
		run: func(ctx context.Context, _ int) (int, error) {
			close(backfillStarted)
			<-ctx.Done()
			return 0, ctx.Err()
		},
	}
	scanner := &imageUpdateScannerFake{}
	settings := &pollingSettingReaderFake{enabled: true}
	watcher := newImageUpdateWatcherForTest(t, scanner, settings, bus.NewDockerEventBus(bus.Config{}), backfiller)
	ctx, cancel := context.WithCancel(t.Context())
	errCh := make(chan error, 1)
	go func() { errCh <- watcher.Start(ctx) }()

	<-backfillStarted
	cancel()
	select {
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(time.Second):
		require.FailNow(t, "watcher did not stop after cancellation")
	}
	require.Zero(t, scanner.count())
}

func TestImageUpdateWatcher_ScheduledPollTriggersScanWithoutEvents(t *testing.T) {
	scanner := &imageUpdateScannerFake{}
	settings := &pollingSettingReaderFake{enabled: true, schedule: "@every 1s"}
	watcher := newImageUpdateWatcherForTest(t, scanner, settings, bus.NewDockerEventBus(bus.Config{}), nil)
	jobScheduler, err := NewJobScheduler(t.Context(), watcher.coordinator, time.UTC)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, jobScheduler.Stop(context.WithoutCancel(t.Context()))) })
	require.NoError(t, jobScheduler.RegisterBusWatcher(watcher, true))

	require.Eventually(t, func() bool { return scanner.count() == 1 }, time.Second, 5*time.Millisecond)
	require.Eventually(t, func() bool {
		record, nextRun, scheduleStateErr := watcher.coordinator.ScheduleState(t.Context(), watcher.Name())
		return scheduleStateErr == nil && record.Schedule == settings.schedule && nextRun != nil
	}, time.Second, 5*time.Millisecond)
	require.NoError(t, jobScheduler.StartScheduler(t.Context()))
	record, nextRun, err := watcher.coordinator.ScheduleState(t.Context(), watcher.Name())
	require.NoError(t, err)
	require.Equal(t, settings.schedule, record.Schedule)
	require.NotNil(t, nextRun)
	require.Eventually(t, func() bool { return scanner.count() >= 3 }, 5*time.Second, 10*time.Millisecond)
	require.Eventually(t, func() bool {
		history, listErr := watcher.coordinator.List(t.Context(), "0", watcher.Name(), 1, 10)
		if listErr != nil {
			return false
		}
		for _, run := range history.Runs {
			if run.Trigger == "scheduled" && run.Status == types.RunStatusSucceeded {
				return true
			}
		}
		return false
	}, time.Second, 5*time.Millisecond)
}

func TestImageUpdateWatcher_ClosedEventSubscriptionKeepsWatcherRunning(t *testing.T) {
	scanner := &imageUpdateScannerFake{}
	settings := &pollingSettingReaderFake{enabled: true}
	eventBus := bus.NewDockerEventBus(bus.Config{})
	watcher := newImageUpdateWatcherForTest(t, scanner, settings, eventBus, nil)
	startImageUpdateWatcherForTest(t, watcher)

	require.Eventually(t, func() bool { return scanner.count() == 1 }, time.Second, 5*time.Millisecond)
	eventBus.Close()
	watcher.Trigger()

	require.Eventually(t, func() bool { return scanner.count() >= 2 }, time.Second, 5*time.Millisecond)
	require.True(t, watcher.running.Load())
}

func TestImageUpdateWatcher_SupervisorRestartsAfterCancellation(t *testing.T) {
	scanner := &imageUpdateScannerFake{}
	watcher := newImageUpdateWatcherForTest(t, scanner, &pollingSettingReaderFake{enabled: true}, bus.NewDockerEventBus(bus.Config{}), nil)
	supervisor := &watcherSupervisor{watcher: watcher, restart: make(chan struct{}, 1), health: types.SchedulerWorkerHealth{Status: "starting", UpdatedAt: time.Now()}}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- supervisor.run(ctx) }()
	require.Eventually(t, func() bool { return watcher.running.Load() }, time.Second, time.Millisecond)
	supervisor.mu.Lock()
	supervisor.restart <- struct{}{}
	supervisor.cancel()
	supervisor.mu.Unlock()
	require.Eventually(t, func() bool { return scanner.count() > 0 }, time.Second, time.Millisecond)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
}

func TestImageUpdateWatcher_RunNowWaitsForMetadataReadiness(t *testing.T) {
	scanner := &imageUpdateScannerFake{}
	settings := &pollingSettingReaderFake{enabled: true}
	watcher := newImageUpdateWatcherForTest(t, scanner, settings, bus.NewDockerEventBus(bus.Config{}), nil)

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, watcher.RunNow(ctx), context.DeadlineExceeded)
	require.Zero(t, scanner.count())
}
