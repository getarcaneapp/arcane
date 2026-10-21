package scheduler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/moby/moby/api/types/events"
	"github.com/stretchr/testify/require"
	"go.getarcane.app/streams/bus"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/jobcontext"
)

type imageUpdateScannerFakeInternal struct {
	mu        sync.Mutex
	calls     int
	active    int
	maxActive int
	errors    []error
	panics    []bool
	startedCh chan int
	releaseCh <-chan struct{}
}

func (s *imageUpdateScannerFakeInternal) RunImageCheck(ctx context.Context, _ types.ImageUpdateCheckRequest) (types.SchedulerOutcome, error) {
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

func (s *imageUpdateScannerFakeInternal) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *imageUpdateScannerFakeInternal) peakActive() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.maxActive
}

type pollingSettingReaderFakeInternal struct {
	mu                  sync.RWMutex
	enabled             bool
	eventWatcherEnabled bool
	schedule            string
}

func (s *pollingSettingReaderFakeInternal) GetBoolSetting(_ context.Context, key string, fallback bool) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	switch key {
	case "pollingEnabled":
		return s.enabled
	case "imageEventWatcherEnabled":
		return s.eventWatcherEnabled
	default:
		return fallback
	}
}

func (s *pollingSettingReaderFakeInternal) GetStringSetting(_ context.Context, _, defaultValue string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.schedule == "" {
		return defaultValue
	}
	return s.schedule
}

func (s *pollingSettingReaderFakeInternal) setEnabled(enabled bool) {
	s.mu.Lock()
	s.enabled = enabled
	s.mu.Unlock()
}

type dockerEventBusProviderFakeInternal struct {
	eventBus *bus.DockerEventBus
}

func (p dockerEventBusProviderFakeInternal) EventBus() *bus.DockerEventBus {
	return p.eventBus
}

type projectImageRefsBackfillerFakeInternal struct {
	mu      sync.Mutex
	calls   int
	run     func(ctx context.Context, call int) (int, error)
	started chan int
}

func (b *projectImageRefsBackfillerFakeInternal) BackfillProjectImageRefs(ctx context.Context) (int, error) {
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

func (b *projectImageRefsBackfillerFakeInternal) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls
}

type lockedBufferInternal struct {
	mu sync.Mutex
	bytes.Buffer
}

func (b *lockedBufferInternal) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.Write(p)
}

func (b *lockedBufferInternal) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.String()
}

func newImageUpdateWatcherForTest(t *testing.T,
	scanner imageUpdateScannerInternal,
	settings pollingSettingReaderInternal,
	eventBus *bus.DockerEventBus,
	backfiller projectImageRefsBackfillerInternal,
) *ImageUpdateWatcher {
	t.Helper()
	if backfiller == nil {
		backfiller = &projectImageRefsBackfillerFakeInternal{}
	}
	watcher := &ImageUpdateWatcher{
		imageUpdateService: scanner,
		settingsService:    settings,
		dockerService:      dockerEventBusProviderFakeInternal{eventBus: eventBus},
		projectService:     backfiller,
		trigger:            make(chan struct{}, 1),
		scheduleRefresh:    make(chan struct{}, 1),
		location:           time.UTC,
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
	scanner := &imageUpdateScannerFakeInternal{}
	settings := &pollingSettingReaderFakeInternal{enabled: true, eventWatcherEnabled: true}
	eventBus := bus.NewDockerEventBus()
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
	scanner := &imageUpdateScannerFakeInternal{}
	settings := &pollingSettingReaderFakeInternal{enabled: true}
	eventBus := bus.NewDockerEventBus()
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
	scanner := &imageUpdateScannerFakeInternal{
		startedCh: make(chan int, 4),
		releaseCh: releaseCh,
	}
	settings := &pollingSettingReaderFakeInternal{enabled: true, eventWatcherEnabled: true}
	eventBus := bus.NewDockerEventBus()
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
	scanner := &imageUpdateScannerFakeInternal{}
	settings := &pollingSettingReaderFakeInternal{enabled: false}
	eventBus := bus.NewDockerEventBus()
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
	scanner := &imageUpdateScannerFakeInternal{errors: []error{errors.New("registry unavailable")}}
	settings := &pollingSettingReaderFakeInternal{enabled: true, eventWatcherEnabled: true}
	eventBus := bus.NewDockerEventBus()
	watcher := newImageUpdateWatcherForTest(t, scanner, settings, eventBus, nil)
	startImageUpdateWatcherForTest(t, watcher)

	require.Eventually(t, func() bool { return scanner.count() == 1 }, time.Second, 5*time.Millisecond)
	eventBus.Publish(events.Message{Type: events.ImageEventType, Action: events.ActionPull})
	require.Eventually(t, func() bool { return scanner.count() == 2 }, time.Second, 5*time.Millisecond)
}

func TestImageUpdateWatcher_RunNowReturnsInProgressErrorDuringActiveScan(t *testing.T) {
	releaseCh := make(chan struct{})
	scanner := &imageUpdateScannerFakeInternal{
		startedCh: make(chan int, 1),
		releaseCh: releaseCh,
	}
	settings := &pollingSettingReaderFakeInternal{enabled: true}
	watcher := newImageUpdateWatcherForTest(t, scanner, settings, bus.NewDockerEventBus(), nil)
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

func TestImageUpdateWatcher_RunNowReturnsCompletedResult(t *testing.T) {
	scanner := &imageUpdateScannerFakeInternal{}
	settings := &pollingSettingReaderFakeInternal{enabled: true}
	watcher := newImageUpdateWatcherForTest(t, scanner, settings, bus.NewDockerEventBus(), nil)
	startImageUpdateWatcherForTest(t, watcher)

	require.Eventually(t, func() bool { return scanner.count() == 1 }, time.Second, time.Millisecond)
	require.Eventually(t, func() bool {
		return watcher.RunNow(t.Context()) == nil
	}, time.Second, time.Millisecond)
	for range 10 {
		require.NoError(t, watcher.RunNow(t.Context()))
	}
}

func TestImageUpdateWatcher_RunNowReturnsContainedScanPanic(t *testing.T) {
	scanner := &imageUpdateScannerFakeInternal{panics: []bool{false, true}}
	settings := &pollingSettingReaderFakeInternal{enabled: true}
	watcher := newImageUpdateWatcherForTest(t, scanner, settings, bus.NewDockerEventBus(), nil)
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
	scanner := &imageUpdateScannerFakeInternal{
		startedCh: make(chan int, 4),
		releaseCh: releaseCh,
	}
	settings := &pollingSettingReaderFakeInternal{enabled: true, eventWatcherEnabled: true}
	eventBus := bus.NewDockerEventBus()
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

func TestImageUpdateWatcher_BackfillGatesFirstScanAndCoalescesEventBurst(t *testing.T) {
	const (
		projectCount = 2500
		eventCount   = 10000
	)

	var logBuffer lockedBufferInternal
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuffer, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	backfillStarted := make(chan struct{})
	releaseBackfill := make(chan struct{})
	backfiller := &projectImageRefsBackfillerFakeInternal{
		run: func(ctx context.Context, _ int) (int, error) {
			close(backfillStarted)
			select {
			case <-ctx.Done():
				return 0, ctx.Err()
			case <-releaseBackfill:
				return projectCount, nil
			}
		},
	}
	scanner := &imageUpdateScannerFakeInternal{}
	settings := &pollingSettingReaderFakeInternal{enabled: true, eventWatcherEnabled: true}
	eventBus := bus.NewDockerEventBus()
	watcher := newImageUpdateWatcherForTest(t, scanner, settings, eventBus, backfiller)
	startImageUpdateWatcherForTest(t, watcher)

	<-backfillStarted
	burstStartedAt := time.Now()
	for range eventCount {
		eventBus.Publish(events.Message{Type: events.ImageEventType, Action: events.ActionPull})
	}
	require.Never(t, func() bool { return scanner.count() > 0 }, 30*time.Millisecond, 5*time.Millisecond)

	close(releaseBackfill)
	require.Eventually(t, func() bool { return scanner.count() == 1 }, time.Second, 5*time.Millisecond)
	time.Sleep(30 * time.Millisecond)
	require.Equal(t, 1, scanner.count())

	logs := logBuffer.String()
	require.Contains(t, logs, "project image metadata backfill completed", logs)
	require.Contains(t, logs, "projects=2500", logs)
	require.Contains(t, logs, "duration=", logs)
	t.Logf("coalesced %d image events into one scan after backfilling %d projects in %s", eventCount, projectCount, time.Since(burstStartedAt))
}

func TestImageUpdateWatcher_BackfillRetriesContainedPanic(t *testing.T) {
	backfiller := &projectImageRefsBackfillerFakeInternal{
		run: func(_ context.Context, call int) (int, error) {
			if call == 1 {
				panic("deliberate backfill panic")
			}
			return 1, nil
		},
	}
	scanner := &imageUpdateScannerFakeInternal{}
	settings := &pollingSettingReaderFakeInternal{enabled: true}
	watcher := newImageUpdateWatcherForTest(t, scanner, settings, bus.NewDockerEventBus(), backfiller)
	startImageUpdateWatcherForTest(t, watcher)

	require.Eventually(t, func() bool { return backfiller.count() == 2 }, time.Second, time.Millisecond)
	require.Eventually(t, func() bool { return scanner.count() == 1 }, time.Second, time.Millisecond)
}

func TestImageUpdateWatcher_BackfillFailureRetriesBeforeScanning(t *testing.T) {
	secondAttemptStarted := make(chan struct{})
	releaseSecondAttempt := make(chan struct{})
	backfiller := &projectImageRefsBackfillerFakeInternal{
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
	scanner := &imageUpdateScannerFakeInternal{}
	settings := &pollingSettingReaderFakeInternal{enabled: true}
	watcher := newImageUpdateWatcherForTest(t, scanner, settings, bus.NewDockerEventBus(), backfiller)
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
	backfiller := &projectImageRefsBackfillerFakeInternal{
		run: func(ctx context.Context, _ int) (int, error) {
			close(backfillStarted)
			<-ctx.Done()
			return 0, ctx.Err()
		},
	}
	scanner := &imageUpdateScannerFakeInternal{}
	settings := &pollingSettingReaderFakeInternal{enabled: true}
	watcher := newImageUpdateWatcherForTest(t, scanner, settings, bus.NewDockerEventBus(), backfiller)
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
	scanner := &imageUpdateScannerFakeInternal{}
	settings := &pollingSettingReaderFakeInternal{enabled: true, schedule: "* * * * * *"}
	watcher := newImageUpdateWatcherForTest(t, scanner, settings, bus.NewDockerEventBus(), nil)
	jobScheduler, err := NewJobScheduler(t.Context(), watcher.coordinator, time.UTC)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, jobScheduler.Stop(context.WithoutCancel(t.Context()))) })
	require.NoError(t, jobScheduler.RegisterBusWatcher(watcher, true))

	require.Eventually(t, func() bool { return scanner.count() == 1 }, time.Second, 5*time.Millisecond)
	require.Eventually(t, func() bool {
		record, scheduleStateErr := watcher.coordinator.ScheduleState(t.Context(), watcher.Name())
		return scheduleStateErr == nil && record.Schedule == settings.schedule && !record.NextRun.IsZero()
	}, time.Second, 5*time.Millisecond)
	require.NoError(t, jobScheduler.StartScheduler(t.Context()))
	record, err := watcher.coordinator.ScheduleState(t.Context(), watcher.Name())
	require.NoError(t, err)
	require.Equal(t, settings.schedule, record.Schedule)
	require.False(t, record.NextRun.IsZero())
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
	scanner := &imageUpdateScannerFakeInternal{}
	settings := &pollingSettingReaderFakeInternal{enabled: true}
	eventBus := bus.NewDockerEventBus()
	watcher := newImageUpdateWatcherForTest(t, scanner, settings, eventBus, nil)
	startImageUpdateWatcherForTest(t, watcher)

	require.Eventually(t, func() bool { return scanner.count() == 1 }, time.Second, 5*time.Millisecond)
	eventBus.Close()
	watcher.Trigger()

	require.Eventually(t, func() bool { return scanner.count() >= 2 }, time.Second, 5*time.Millisecond)
	require.True(t, watcher.running.Load())
}

func TestImageUpdateWatcher_SupervisorRestartsAfterCancellation(t *testing.T) {
	scanner := &imageUpdateScannerFakeInternal{}
	watcher := newImageUpdateWatcherForTest(t, scanner, &pollingSettingReaderFakeInternal{enabled: true}, bus.NewDockerEventBus(), nil)
	supervisor := newWatcherSupervisor(watcher)
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
	scanner := &imageUpdateScannerFakeInternal{}
	settings := &pollingSettingReaderFakeInternal{enabled: true}
	watcher := newImageUpdateWatcherForTest(t, scanner, settings, bus.NewDockerEventBus(), nil)

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, watcher.RunNow(ctx), context.DeadlineExceeded)
	require.Zero(t, scanner.count())
}
