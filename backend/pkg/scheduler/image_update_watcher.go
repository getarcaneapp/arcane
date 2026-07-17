package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/types/v2/containerregistry"
	"github.com/getarcaneapp/arcane/types/v2/imageupdate"
	"github.com/moby/moby/api/types/events"
	"go.getarcane.app/streams/bus"
)

const (
	imageUpdateWatcherDebounce      = 2 * time.Second
	imageUpdateWatcherBackfillRetry = 5 * time.Second
)

type imageUpdateScannerInternal interface {
	CheckAllImages(ctx context.Context, limit int, externalCreds []containerregistry.Credential) (map[string]*imageupdate.Response, error)
}

type registryCredentialLoaderInternal interface {
	GetEnabledRegistryCredentials(ctx context.Context) ([]containerregistry.Credential, error)
}

type pollingSettingReaderInternal interface {
	GetBoolSetting(ctx context.Context, key string, fallback bool) bool
}

type dockerEventBusProviderInternal interface {
	EventBus() *bus.DockerEventBus
}

type projectImageRefsBackfillerInternal interface {
	BackfillProjectImageRefs(ctx context.Context) (int, error)
}

type imageUpdateScanRunInternal struct {
	done chan struct{}
}

// ImageUpdateWatcher continuously reconciles image update state after Docker image changes.
type ImageUpdateWatcher struct {
	imageUpdateService imageUpdateScannerInternal
	settingsService    pollingSettingReaderInternal
	environmentService registryCredentialLoaderInternal
	dockerService      dockerEventBusProviderInternal
	projectService     projectImageRefsBackfillerInternal
	triggerCh          chan struct{}
	debounce           time.Duration
	backfillRetry      time.Duration
	metadataReady      chan struct{}
	metadataReadyOnce  sync.Once
	activeRun          atomic.Pointer[imageUpdateScanRunInternal]
}

// NewImageUpdateWatcher constructs the image update watcher from the existing services.
func NewImageUpdateWatcher(imageUpdateService *services.ImageUpdateService, settingsService *services.SettingsService, environmentService *services.EnvironmentService, dockerService *services.DockerClientService, projectService *services.ProjectService) *ImageUpdateWatcher {
	return &ImageUpdateWatcher{
		imageUpdateService: imageUpdateService,
		settingsService:    settingsService,
		environmentService: environmentService,
		dockerService:      dockerService,
		projectService:     projectService,
		triggerCh:          make(chan struct{}, 1),
		debounce:           imageUpdateWatcherDebounce,
		backfillRetry:      imageUpdateWatcherBackfillRetry,
		metadataReady:      make(chan struct{}),
	}
}

// Name identifies the watcher in scheduler lifecycle logs.
func (w *ImageUpdateWatcher) Name() string {
	return "image-polling"
}

// Start subscribes to Docker image events, prepares project metadata, and runs scans until ctx is canceled.
func (w *ImageUpdateWatcher) Start(ctx context.Context) error {
	if w == nil || w.dockerService == nil || w.dockerService.EventBus() == nil {
		return errors.New("docker event bus unavailable")
	}
	if w.projectService == nil || w.metadataReady == nil {
		return errors.New("project image metadata backfiller unavailable")
	}

	eventCh, unsubscribe := w.dockerService.EventBus().Subscribe(events.ImageEventType, bus.WithSubscriberBuffer(16))
	defer unsubscribe()

	var listener sync.WaitGroup
	listenerStarted := make(chan struct{})
	listener.Go(func() {
		close(listenerStarted)
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-eventCh:
				if !ok {
					return
				}
				w.Trigger()
			}
		}
	})

	select {
	case <-ctx.Done():
		listener.Wait()
		return nil
	case <-listenerStarted:
	}

	slog.InfoContext(ctx, "image update watcher started")
	defer func() {
		listener.Wait()
		slog.InfoContext(ctx, "image update watcher stopped")
	}()

	if !w.backfillProjectImageRefsInternal(ctx) {
		return nil
	}

	w.Trigger()
	w.runTriggeredScansInternal(ctx)
	return nil
}

// Trigger queues an image scan without blocking the Docker event publisher.
func (w *ImageUpdateWatcher) Trigger() {
	if w == nil {
		return
	}
	select {
	case w.triggerCh <- struct{}{}:
	default:
	}
}

// RunNow performs the same full-host image scan used by automatic watcher triggers.
func (w *ImageUpdateWatcher) RunNow(ctx context.Context) error {
	if w == nil || w.settingsService == nil || w.imageUpdateService == nil || w.environmentService == nil || w.metadataReady == nil {
		return errors.New("image update watcher is not initialized")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-w.metadataReady:
	}

	run := &imageUpdateScanRunInternal{done: make(chan struct{})}
	for {
		activeRun := w.activeRun.Load()
		if activeRun == nil && w.activeRun.CompareAndSwap(nil, run) {
			break
		}
		if activeRun == nil {
			continue
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-activeRun.done:
		}
	}
	defer func() {
		w.activeRun.Store(nil)
		close(run.done)
	}()

	if err := ctx.Err(); err != nil {
		return err
	}
	if !w.settingsService.GetBoolSetting(ctx, "pollingEnabled", true) {
		slog.DebugContext(ctx, "image update watcher disabled; skipping image scan")
		return nil
	}

	slog.InfoContext(ctx, "image scan run started")

	creds, err := w.environmentService.GetEnabledRegistryCredentials(ctx)
	if err != nil {
		slog.WarnContext(ctx, "failed to load registry credentials for image scan", "error", err.Error())
		creds = nil
	}

	results, err := w.imageUpdateService.CheckAllImages(ctx, 0, creds)
	if err != nil {
		return fmt.Errorf("image scan failed: %w", err)
	}

	total := len(results)
	updates := 0
	scanErrors := 0
	for _, result := range results {
		if result == nil {
			continue
		}
		if result.Error != "" {
			scanErrors++
			continue
		}
		if result.HasUpdate {
			updates++
		}
	}

	slog.InfoContext(ctx, "image scan run completed", "checked", total, "updates", updates, "errors", scanErrors)
	return nil
}

func (w *ImageUpdateWatcher) backfillProjectImageRefsInternal(ctx context.Context) bool {
	retryDelay := w.backfillRetry
	if retryDelay <= 0 {
		retryDelay = imageUpdateWatcherBackfillRetry
	}

	for attempt := 1; ; attempt++ {
		if ctx.Err() != nil {
			return false
		}

		startedAt := time.Now()
		count, err := w.projectService.BackfillProjectImageRefs(ctx)
		duration := time.Since(startedAt)
		if err == nil {
			slog.InfoContext(ctx, "project image metadata backfill completed", "projects", count, "duration", duration, "attempt", attempt)
			w.metadataReadyOnce.Do(func() { close(w.metadataReady) })
			return true
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			return false
		}

		slog.WarnContext(ctx, "project image metadata backfill failed; retrying", "projects", count, "duration", duration, "attempt", attempt, "retryIn", retryDelay, "error", err)
		timer := time.NewTimer(retryDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-timer.C:
		}
	}
}

func (w *ImageUpdateWatcher) runTriggeredScansInternal(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.triggerCh:
		}

		if !w.waitForDebounceInternal(ctx) {
			return
		}
		if err := w.RunNow(ctx); err != nil && !errors.Is(err, context.Canceled) {
			slog.ErrorContext(ctx, "image update watcher scan failed", "error", err)
		}
	}
}

func (w *ImageUpdateWatcher) waitForDebounceInternal(ctx context.Context) bool {
	timer := time.NewTimer(w.debounce)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return false
		case <-w.triggerCh:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(w.debounce)
		case <-timer.C:
			return true
		}
	}
}
