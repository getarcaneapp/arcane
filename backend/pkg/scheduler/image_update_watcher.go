package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	imageupdatetypes "github.com/getarcaneapp/arcane/types/v2/imageupdate"
	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/moby/moby/api/types/events"
	"go.getarcane.app/streams/bus"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/imageupdate"
	"github.com/getarcaneapp/arcane/backend/v2/internal/project"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/jobcontext"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/runs"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
)

const (
	imageUpdateWatcherDebounce        = 2 * time.Second
	imageUpdateWatcherBackfillRetry   = 5 * time.Second
	imageUpdateWatcherDefaultSchedule = "0 0 * * * *"
)

type imageUpdateScannerInternal interface {
	RunImageCheck(ctx context.Context, request imageupdatetypes.CheckRequest) (schedulertypes.Outcome, error)
}

type pollingSettingReaderInternal interface {
	GetBoolSetting(ctx context.Context, key string, fallback bool) bool
	GetStringSetting(ctx context.Context, key, defaultValue string) string
}

type dockerEventBusProviderInternal interface {
	EventBus() *bus.DockerEventBus
}

type projectImageRefsBackfillerInternal interface {
	BackfillProjectImageRefs(ctx context.Context) (int, error)
}

// ImageUpdateWatcher debounces Docker events and admits durable image scans.
type ImageUpdateWatcher struct {
	imageUpdateService     imageUpdateScannerInternal
	settingsService        pollingSettingReaderInternal
	dockerService          dockerEventBusProviderInternal
	projectService         projectImageRefsBackfillerInternal
	dispatchMu             sync.RWMutex
	coordinator            *runs.Coordinator
	eventDegraded          atomic.Bool
	running                atomic.Bool
	scanRunning            atomic.Bool
	stopping               atomic.Bool
	triggerGeneration      atomic.Uint64
	acknowledgedGeneration atomic.Uint64
	triggeredAt            atomic.Int64
	trigger                chan struct{}
	scheduleRefresh        chan struct{}
	runMu                  sync.Mutex
	cancel                 context.CancelFunc
	done                   chan struct{}
	location               *time.Location
	debounce               time.Duration
	backfillRetry          time.Duration
	metadataReady          chan struct{}
	metadataReadyOnce      sync.Once
	started                chan struct{}
	startedOnce            sync.Once
	stopped                chan struct{}
	stoppedOnce            sync.Once
}

func NewImageUpdateWatcher(cfg *config.Config,
	imageUpdateService *imageupdate.ImageUpdateService,
	settingsService *settings.SettingsService,
	dockerService *docker.DockerClientService,
	projectService *project.ProjectService) (*ImageUpdateWatcher,
	error,
) {
	if imageUpdateService == nil || settingsService == nil || dockerService == nil || projectService == nil {
		return nil, errors.New("image update watcher dependencies unavailable")
	}
	location := time.UTC
	if cfg != nil {
		location = cfg.GetLocation()
	}
	return &ImageUpdateWatcher{
			imageUpdateService: imageUpdateService,
			settingsService:    settingsService,
			dockerService:      dockerService,
			projectService:     projectService,
			location:           location,
			debounce:           imageUpdateWatcherDebounce,
			backfillRetry:      imageUpdateWatcherBackfillRetry,
			metadataReady:      make(chan struct{}),
			started:            make(chan struct{}),
			stopped:            make(chan struct{}),
			trigger: make(chan struct{},
				1),
			scheduleRefresh: make(chan struct{},
				1),
		},
		nil
}

func (w *ImageUpdateWatcher) Name() string { return "image-polling" }

// Start owns the debounce loop and joins its workers before returning.
func (w *ImageUpdateWatcher) Start(ctx context.Context) error {
	if w.stopping.Load() {
		return errors.New("image update watcher cannot be restarted after stop")
	}
	if !w.running.CompareAndSwap(false, true) {
		return errors.New("image update watcher already started")
	}
	defer w.running.Store(false)
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	w.runMu.Lock()
	if w.stopping.Load() {
		w.runMu.Unlock()
		cancel()
		return errors.New("image update watcher stopped")
	}
	w.cancel = cancel
	w.done = done
	w.runMu.Unlock()
	defer cancel()
	defer close(done)
	return w.runEventLoopInternal(runCtx)
}

func (w *ImageUpdateWatcher) runEventLoopInternal(runCtx context.Context) error {
	eventBus := w.dockerService.EventBus()
	if eventBus == nil {
		return errors.New("docker event bus unavailable")
	}
	eventCh, unsubscribe := eventBus.Subscribe(events.ImageEventType, bus.WithSubscriberBuffer(16))
	defer unsubscribe()
	reconnect := time.NewTicker(5 * time.Second)
	defer reconnect.Stop()
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()
	var workers sync.WaitGroup
	defer workers.Wait()
	metadata := make(chan error, 1)
	submitted := make(chan error, 1)
	submitting := false
	workers.Go(func() { w.runMetadataBackfillInternal(runCtx, metadata) })
	w.startedOnce.Do(func() { close(w.started) })
	w.RefreshSchedule()
	ready := false
	for {
		select {
		case <-runCtx.Done():
			return nil
		case <-metadata:
			ready = true
			w.metadataReadyOnce.Do(func() { close(w.metadataReady) })
			w.Trigger()
		case <-w.trigger:
			if ready && !submitting {
				timer.Reset(w.debounce)
			}
		case <-timer.C:
			if !w.scanAdmissionReadyInternal(timer, ready, submitting) {
				continue
			}
			submitting = true
			generation := w.triggerGeneration.Load()
			workers.Go(func() { w.submitTriggeredScanInternal(runCtx, generation, submitted) })
		case err := <-submitted:
			submitting = false
			if err != nil && runCtx.Err() == nil {
				slog.ErrorContext(runCtx, "image scan admission failed", "error", err)
			}
			if w.triggerGeneration.Load() != w.acknowledgedGeneration.Load() {
				timer.Reset(w.debounce)
			}
		case <-w.scheduleRefresh:
			if err := w.refreshScheduleInternal(runCtx); err != nil {
				slog.ErrorContext(runCtx, "Failed to checkpoint image polling schedule", "error", err)
			}
		case <-reconnect.C:
			eventCh, unsubscribe = w.reconnectEventsInternal(eventCh, unsubscribe)
		case _, ok := <-eventCh:
			if !ok {
				eventCh = nil
				w.eventDegraded.Store(true)
				continue
			}
			if w.settingsService.GetBoolSetting(runCtx, "imageEventWatcherEnabled", false) {
				w.Trigger()
			}
		}
	}
}

func (w *ImageUpdateWatcher) reconnectEventsInternal(eventCh <-chan events.Message, unsubscribe func()) (<-chan events.Message, func()) {
	if eventCh != nil {
		return eventCh, unsubscribe
	}
	currentBus := w.dockerService.EventBus()
	if currentBus == nil {
		return eventCh, unsubscribe
	}
	unsubscribe()
	eventCh, unsubscribe = currentBus.Subscribe(events.ImageEventType, bus.WithSubscriberBuffer(16))
	w.eventDegraded.Store(false)
	return eventCh, unsubscribe
}

func (w *ImageUpdateWatcher) runMetadataBackfillInternal(ctx context.Context, metadata chan<- error) {
	for attempt := 1; ctx.Err() == nil; attempt++ {
		var err error
		func() {
			defer utils.RecoverToError(&err, "image metadata backfill")
			err = w.backfillProjectImageRefsInternal(ctx, attempt)
		}()
		if err == nil {
			select {
			case metadata <- nil:
			case <-ctx.Done():
			}
			return
		}
		delay := w.backfillRetry
		if delay <= 0 {
			delay = imageUpdateWatcherBackfillRetry
		}
		retry := time.NewTimer(delay)
		select {
		case <-retry.C:
		case <-ctx.Done():
			retry.Stop()
			return
		}
	}
}

func (w *ImageUpdateWatcher) scanAdmissionReadyInternal(timer *time.Timer, ready, submitting bool) bool {
	if !ready || submitting || w.triggerGeneration.Load() == w.acknowledgedGeneration.Load() {
		return false
	}
	if remaining := time.Until(time.Unix(0, w.triggeredAt.Load()).Add(w.debounce)); remaining > 0 {
		timer.Reset(remaining)
		return false
	}
	return true
}

func (w *ImageUpdateWatcher) submitTriggeredScanInternal(ctx context.Context, generation uint64, submitted chan<- error) {
	var err error
	func() {
		defer utils.RecoverToError(&err, "image scan admission")
		w.dispatchMu.RLock()
		coordinator := w.coordinator
		w.dispatchMu.RUnlock()
		if coordinator == nil {
			err = errors.New("durable job coordinator unavailable")
			return
		}
		_, err = coordinator.Submit(ctx, schedulertypes.Request{JobID: w.Name(), EnvironmentID: "0", Trigger: "watcher"})
	}()
	if err == nil {
		w.acknowledgedGeneration.Store(generation)
	}
	select {
	case submitted <- err:
	case <-ctx.Done():
	}
}

func (w *ImageUpdateWatcher) Stop(ctx context.Context) error {
	w.stopping.Store(true)
	w.stoppedOnce.Do(func() { close(w.stopped) })
	w.runMu.Lock()
	cancel, done := w.cancel, w.done
	w.runMu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *ImageUpdateWatcher) Trigger() {
	w.triggeredAt.Store(time.Now().UnixNano())
	w.triggerGeneration.Add(1)
	select {
	case w.trigger <- struct{}{}:
	default:
	}
}

func (w *ImageUpdateWatcher) RefreshSchedule() {
	select {
	case w.scheduleRefresh <- struct{}{}:
	default:
	}
}

func (w *ImageUpdateWatcher) RunNow(ctx context.Context) (err error) {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-w.stopped:
		return errors.New("image update watcher is not running")
	case <-w.started:
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-w.stopped:
		return errors.New("image update watcher is not running")
	case <-w.metadataReady:
	}
	if !w.running.Load() {
		return errors.New("image update watcher is not running")
	}
	if _, executing := jobcontext.Run(ctx); executing {
		if !w.scanRunning.CompareAndSwap(false, true) {
			return common.Classify(common.ErrImageScanInProgress, errors.New("an image update check is already in progress"))
		}
		defer w.scanRunning.Store(false)
		defer utils.RecoverToError(&err, "image update scan worker")
		return w.executeScanInternal(ctx)
	}
	if w.scanRunning.Load() {
		return common.Classify(common.ErrImageScanInProgress, errors.New("an image update check is already in progress"))
	}
	w.dispatchMu.RLock()
	coordinator := w.coordinator
	w.dispatchMu.RUnlock()
	if coordinator == nil {
		return errors.New("durable job coordinator unavailable")
	}
	run, err := coordinator.Submit(ctx, schedulertypes.Request{JobID: w.Name(), EnvironmentID: "0", Trigger: "manual"})
	if err != nil {
		return err
	}
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		current, getErr := coordinator.Get(ctx, run.EnvironmentID, run.JobID, run.ID)
		if getErr != nil {
			return getErr
		}
		if current.Status.Terminal() || current.Status == schedulertypes.NeedsAttention {
			if current.Status == schedulertypes.Succeeded {
				return nil
			}
			return &schedulertypes.OutcomeError{Outcome: current.Outcome}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		case <-w.stopped:
			return errors.New("image update watcher stopped")
		}
	}
}

func (w *ImageUpdateWatcher) executeScanInternal(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !w.settingsService.GetBoolSetting(ctx, "pollingEnabled", true) {
		slog.DebugContext(ctx, "image update watcher disabled; skipping image scan")
		return nil
	}

	slog.InfoContext(ctx, "image scan run started")
	outcome, err := w.imageUpdateService.RunImageCheck(ctx, imageupdatetypes.CheckRequest{All: true})
	if err != nil {
		return fmt.Errorf("image scan failed: %w", err)
	}
	slog.InfoContext(ctx, "image scan run completed", "status", outcome.Status, "message", outcome.Message)
	if outcome.Status != schedulertypes.Succeeded {
		return &schedulertypes.OutcomeError{Outcome: outcome}
	}
	return nil
}

func (w *ImageUpdateWatcher) backfillProjectImageRefsInternal(ctx context.Context, attempt int) error {
	started := time.Now()
	count, err := w.projectService.BackfillProjectImageRefs(ctx)
	if err == nil {
		slog.InfoContext(ctx, "project image metadata backfill completed", "projects", count, "duration", time.Since(started), "attempt", attempt)
	} else if ctx.Err() == nil {
		slog.WarnContext(ctx, "project image metadata backfill failed; retrying", "projects", count, "duration", time.Since(started), "attempt", attempt, "retryIn", w.backfillRetry, "error", err)
	}
	return err
}

func (w *ImageUpdateWatcher) refreshScheduleInternal(ctx context.Context) error {
	spec := w.settingsService.GetStringSetting(ctx, "pollingInterval", imageUpdateWatcherDefaultSchedule)
	schedule, err := cronScheduleParser.Parse(spec)
	if err != nil {
		slog.WarnContext(ctx, "invalid pollingInterval cron expression; using default schedule", "pollingInterval", spec, "error", err)
		spec = imageUpdateWatcherDefaultSchedule
		schedule, err = cronScheduleParser.Parse(spec)
		if err != nil {
			return err
		}
	}
	w.dispatchMu.RLock()
	coordinator := w.coordinator
	w.dispatchMu.RUnlock()
	if coordinator == nil {
		return errors.New("durable job coordinator unavailable")
	}
	return coordinator.Checkpoint(ctx, w.Name(), spec, schedule.Next(time.Now().In(w.location)))
}

func (w *ImageUpdateWatcher) SetCoordinator(coordinator *runs.Coordinator) {
	w.dispatchMu.Lock()
	w.coordinator = coordinator
	w.dispatchMu.Unlock()
}
