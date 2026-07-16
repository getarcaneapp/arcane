package scheduler

import (
	"context"
	"errors"
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
	mu      sync.RWMutex
	enabled bool
}

func (s *pollingSettingReaderFakeInternal) GetBoolSetting(context.Context, string, bool) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.enabled
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

func newImageUpdateWatcherForTestInternal(scanner imageUpdateScannerInternal, settings pollingSettingReaderInternal, eventBus *bus.DockerEventBus) *ImageUpdateWatcher {
	return &ImageUpdateWatcher{
		imageUpdateService: scanner,
		settingsService:    settings,
		environmentService: registryCredentialLoaderFakeInternal{},
		dockerService:      dockerEventBusProviderFakeInternal{eventBus: eventBus},
		triggerCh:          make(chan struct{}, 1),
		debounce:           10 * time.Millisecond,
	}
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
	watcher := newImageUpdateWatcherForTestInternal(scanner, settings, eventBus)
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
	watcher := newImageUpdateWatcherForTestInternal(scanner, settings, eventBus)
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
	watcher := newImageUpdateWatcherForTestInternal(scanner, settings, eventBus)
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
	watcher := newImageUpdateWatcherForTestInternal(scanner, settings, eventBus)
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
	watcher := newImageUpdateWatcherForTestInternal(scanner, settings, bus.NewDockerEventBus())

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
	watcher := newImageUpdateWatcherForTestInternal(scanner, settings, bus.NewDockerEventBus())

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
