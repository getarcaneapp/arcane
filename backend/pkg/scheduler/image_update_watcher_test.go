package scheduler

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/types/v2/containerregistry"
	"github.com/getarcaneapp/arcane/types/v2/imageupdate"
	"github.com/moby/moby/api/types/events"
	"github.com/stretchr/testify/require"
	"go.getarcane.app/streams/bus"
)

type imageUpdateScannerFakeInternal struct {
	mu        sync.Mutex
	calls     int
	active    int
	maxActive int
	errors    []error
	startedCh chan int
	releaseCh <-chan struct{}
}

func (s *imageUpdateScannerFakeInternal) CheckAllImages(ctx context.Context, _ int, _ []containerregistry.Credential) (map[string]*imageupdate.Response, error) {
	s.mu.Lock()
	s.calls++
	call := s.calls
	s.active++
	s.maxActive = max(s.maxActive, s.active)
	var err error
	if call <= len(s.errors) {
		err = s.errors[call-1]
	}
	startedCh := s.startedCh
	releaseCh := s.releaseCh
	s.mu.Unlock()

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

	s.mu.Lock()
	s.active--
	s.mu.Unlock()

	return map[string]*imageupdate.Response{}, err
}

func (s *imageUpdateScannerFakeInternal) countInternal() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *imageUpdateScannerFakeInternal) maxActiveInternal() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.maxActive
}

type pollingSettingReaderFakeInternal struct {
	mu       sync.RWMutex
	enabled  bool
	schedule string
}

func (s *pollingSettingReaderFakeInternal) GetBoolSetting(context.Context, string, bool) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.enabled
}

func (s *pollingSettingReaderFakeInternal) GetStringSetting(_ context.Context, _ string, defaultValue string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.schedule == "" {
		return defaultValue
	}
	return s.schedule
}

func (s *pollingSettingReaderFakeInternal) setEnabledInternal(enabled bool) {
	s.mu.Lock()
	s.enabled = enabled
	s.mu.Unlock()
}

type registryCredentialLoaderFakeInternal struct{}

func (registryCredentialLoaderFakeInternal) GetEnabledRegistryCredentials(context.Context) ([]containerregistry.Credential, error) {
	return nil, nil
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

func (b *projectImageRefsBackfillerFakeInternal) countInternal() int {
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

func (b *lockedBufferInternal) stringInternal() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.String()
}

func newImageUpdateWatcherForTestInternal(scanner imageUpdateScannerInternal, settings pollingSettingReaderInternal, eventBus *bus.DockerEventBus, backfiller projectImageRefsBackfillerInternal) *ImageUpdateWatcher {
	if backfiller == nil {
		backfiller = &projectImageRefsBackfillerFakeInternal{}
	}
	return &ImageUpdateWatcher{
		imageUpdateService: scanner,
		settingsService:    settings,
		environmentService: registryCredentialLoaderFakeInternal{},
		dockerService:      dockerEventBusProviderFakeInternal{eventBus: eventBus},
		projectService:     backfiller,
		triggerCh:          make(chan struct{}, 1),
		scheduleRefreshCh:  make(chan struct{}, 1),
		location:           time.UTC,
		debounce:           10 * time.Millisecond,
		backfillRetry:      10 * time.Millisecond,
		metadataReady:      make(chan struct{}),
	}
}

func markImageUpdateWatcherMetadataReadyForTestInternal(watcher *ImageUpdateWatcher) {
	watcher.metadataReadyOnce.Do(func() { close(watcher.metadataReady) })
}

func startImageUpdateWatcherForTestInternal(t *testing.T, watcher *ImageUpdateWatcher) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- watcher.Start(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		require.NoError(t, <-errCh)
	})
	return cancel, errCh
}

func TestImageUpdateWatcher_StartScansAtStartupAndCoalescesAllImageEvents(t *testing.T) {
	scanner := &imageUpdateScannerFakeInternal{}
	settings := &pollingSettingReaderFakeInternal{enabled: true}
	eventBus := bus.NewDockerEventBus()
	watcher := newImageUpdateWatcherForTestInternal(scanner, settings, eventBus, nil)
	startImageUpdateWatcherForTestInternal(t, watcher)

	require.Eventually(t, func() bool { return scanner.countInternal() == 1 }, time.Second, 5*time.Millisecond)

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

	require.Eventually(t, func() bool { return scanner.countInternal() == 2 }, time.Second, 5*time.Millisecond)
	time.Sleep(30 * time.Millisecond)
	require.Equal(t, 2, scanner.countInternal())
}

func TestImageUpdateWatcher_EventDuringScanQueuesOneSerializedFollowUp(t *testing.T) {
	releaseCh := make(chan struct{})
	scanner := &imageUpdateScannerFakeInternal{
		startedCh: make(chan int, 4),
		releaseCh: releaseCh,
	}
	settings := &pollingSettingReaderFakeInternal{enabled: true}
	eventBus := bus.NewDockerEventBus()
	watcher := newImageUpdateWatcherForTestInternal(scanner, settings, eventBus, nil)
	startImageUpdateWatcherForTestInternal(t, watcher)

	require.Equal(t, 1, <-scanner.startedCh)
	for range 20 {
		eventBus.Publish(events.Message{Type: events.ImageEventType, Action: events.ActionTag})
	}
	close(releaseCh)

	require.Equal(t, 2, <-scanner.startedCh)
	time.Sleep(30 * time.Millisecond)
	require.Equal(t, 2, scanner.countInternal())
	require.Equal(t, 1, scanner.maxActiveInternal())
}

func TestImageUpdateWatcher_DisabledTriggersAreSkippedUntilEnabled(t *testing.T) {
	scanner := &imageUpdateScannerFakeInternal{}
	settings := &pollingSettingReaderFakeInternal{enabled: false}
	eventBus := bus.NewDockerEventBus()
	watcher := newImageUpdateWatcherForTestInternal(scanner, settings, eventBus, nil)
	startImageUpdateWatcherForTestInternal(t, watcher)

	time.Sleep(30 * time.Millisecond)
	require.Zero(t, scanner.countInternal())

	eventBus.Publish(events.Message{Type: events.ImageEventType, Action: events.ActionPull})
	time.Sleep(30 * time.Millisecond)
	require.Zero(t, scanner.countInternal())

	settings.setEnabledInternal(true)
	watcher.Trigger()
	require.Eventually(t, func() bool { return scanner.countInternal() == 1 }, time.Second, 5*time.Millisecond)
}

func TestImageUpdateWatcher_ScanErrorDoesNotStopFutureEvents(t *testing.T) {
	scanner := &imageUpdateScannerFakeInternal{errors: []error{errors.New("registry unavailable")}}
	settings := &pollingSettingReaderFakeInternal{enabled: true}
	eventBus := bus.NewDockerEventBus()
	watcher := newImageUpdateWatcherForTestInternal(scanner, settings, eventBus, nil)
	startImageUpdateWatcherForTestInternal(t, watcher)

	require.Eventually(t, func() bool { return scanner.countInternal() == 1 }, time.Second, 5*time.Millisecond)
	eventBus.Publish(events.Message{Type: events.ImageEventType, Action: events.ActionPull})
	require.Eventually(t, func() bool { return scanner.countInternal() == 2 }, time.Second, 5*time.Millisecond)
}

func TestImageUpdateWatcher_RunNowSerializesConcurrentCalls(t *testing.T) {
	releaseCh := make(chan struct{})
	scanner := &imageUpdateScannerFakeInternal{
		startedCh: make(chan int, 2),
		releaseCh: releaseCh,
	}
	settings := &pollingSettingReaderFakeInternal{enabled: true}
	watcher := newImageUpdateWatcherForTestInternal(scanner, settings, bus.NewDockerEventBus(), nil)
	markImageUpdateWatcherMetadataReadyForTestInternal(watcher)

	errCh := make(chan error, 2)
	go func() { errCh <- watcher.RunNow(context.Background()) }()
	require.Equal(t, 1, <-scanner.startedCh)
	go func() { errCh <- watcher.RunNow(context.Background()) }()

	time.Sleep(20 * time.Millisecond)
	require.Equal(t, 1, scanner.countInternal())
	close(releaseCh)
	require.Equal(t, 2, <-scanner.startedCh)
	require.NoError(t, <-errCh)
	require.NoError(t, <-errCh)
	require.Equal(t, 1, scanner.maxActiveInternal())
}

func TestImageUpdateWatcher_RunNowWaiterHonorsCancellation(t *testing.T) {
	releaseCh := make(chan struct{})
	scanner := &imageUpdateScannerFakeInternal{
		startedCh: make(chan int, 1),
		releaseCh: releaseCh,
	}
	settings := &pollingSettingReaderFakeInternal{enabled: true}
	watcher := newImageUpdateWatcherForTestInternal(scanner, settings, bus.NewDockerEventBus(), nil)
	markImageUpdateWatcherMetadataReadyForTestInternal(watcher)

	firstErrCh := make(chan error, 1)
	go func() { firstErrCh <- watcher.RunNow(context.Background()) }()
	require.Equal(t, 1, <-scanner.startedCh)

	waitingCtx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, watcher.RunNow(waitingCtx), context.Canceled)
	require.Equal(t, 1, scanner.countInternal())

	close(releaseCh)
	require.NoError(t, <-firstErrCh)
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
	settings := &pollingSettingReaderFakeInternal{enabled: true}
	eventBus := bus.NewDockerEventBus()
	watcher := newImageUpdateWatcherForTestInternal(scanner, settings, eventBus, backfiller)
	startImageUpdateWatcherForTestInternal(t, watcher)

	<-backfillStarted
	burstStartedAt := time.Now()
	for range eventCount {
		eventBus.Publish(events.Message{Type: events.ImageEventType, Action: events.ActionPull})
	}
	require.Never(t, func() bool { return scanner.countInternal() > 0 }, 30*time.Millisecond, 5*time.Millisecond)

	close(releaseBackfill)
	require.Eventually(t, func() bool { return scanner.countInternal() == 1 }, time.Second, 5*time.Millisecond)
	time.Sleep(30 * time.Millisecond)
	require.Equal(t, 1, scanner.countInternal())

	logs := logBuffer.stringInternal()
	require.True(t, strings.Contains(logs, "project image metadata backfill completed"), logs)
	require.True(t, strings.Contains(logs, "projects=2500"), logs)
	require.True(t, strings.Contains(logs, "duration="), logs)
	t.Logf("coalesced %d image events into one scan after backfilling %d projects in %s", eventCount, projectCount, time.Since(burstStartedAt))
}

func TestImageUpdateWatcher_BackfillFailureRetriesBeforeScanning(t *testing.T) {
	secondAttemptStarted := make(chan struct{})
	releaseSecondAttempt := make(chan struct{})
	backfiller := &projectImageRefsBackfillerFakeInternal{
		run: func(ctx context.Context, call int) (int, error) {
			if call == 1 {
				return 0, errors.New("database unavailable")
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
	watcher := newImageUpdateWatcherForTestInternal(scanner, settings, bus.NewDockerEventBus(), backfiller)
	startImageUpdateWatcherForTestInternal(t, watcher)

	select {
	case <-secondAttemptStarted:
	case <-time.After(time.Second):
		t.Fatal("backfill was not retried")
	}
	require.Zero(t, scanner.countInternal())
	close(releaseSecondAttempt)

	require.Eventually(t, func() bool { return scanner.countInternal() == 1 }, time.Second, 5*time.Millisecond)
	require.Equal(t, 2, backfiller.countInternal())
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
	watcher := newImageUpdateWatcherForTestInternal(scanner, settings, bus.NewDockerEventBus(), backfiller)
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- watcher.Start(ctx) }()

	<-backfillStarted
	cancel()
	select {
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("watcher did not stop after cancellation")
	}
	require.Zero(t, scanner.countInternal())
}

func TestImageUpdateWatcher_ScheduledPollTriggersScanWithoutEvents(t *testing.T) {
	scanner := &imageUpdateScannerFakeInternal{}
	settings := &pollingSettingReaderFakeInternal{enabled: true, schedule: "* * * * * *"}
	watcher := newImageUpdateWatcherForTestInternal(scanner, settings, bus.NewDockerEventBus(), nil)
	startImageUpdateWatcherForTestInternal(t, watcher)

	require.Eventually(t, func() bool { return scanner.countInternal() == 1 }, time.Second, 5*time.Millisecond)
	require.Eventually(t, func() bool { return scanner.countInternal() >= 2 }, 3*time.Second, 10*time.Millisecond)
}

func TestImageUpdateWatcher_RunNowWaitsForMetadataReadiness(t *testing.T) {
	scanner := &imageUpdateScannerFakeInternal{}
	settings := &pollingSettingReaderFakeInternal{enabled: true}
	watcher := newImageUpdateWatcherForTestInternal(scanner, settings, bus.NewDockerEventBus(), nil)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, watcher.RunNow(ctx), context.DeadlineExceeded)
	require.Zero(t, scanner.countInternal())
}
