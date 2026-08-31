package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/imageupdate"
	"github.com/getarcaneapp/arcane/backend/v2/internal/project"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/backend/v2/internal/actors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	dockerutil "github.com/getarcaneapp/arcane/backend/v2/pkg/dockerutil"
	"github.com/getarcaneapp/arcane/types/v2/containerregistry"
	imageupdatetypes "github.com/getarcaneapp/arcane/types/v2/imageupdate"
	"github.com/moby/moby/api/types/events"
	"go.getarcane.app/streams/bus"
)

const (
	imageUpdateWatcherDebounce        = 2 * time.Second
	imageUpdateWatcherBackfillRetry   = 5 * time.Second
	imageUpdateWatcherDefaultSchedule = "0 0 * * * *"
	imageUpdateWatcherStopTimeout     = 30 * time.Second
	imageUpdateWatcherActorID         = "image-update-watcher"
)

type imageUpdateScannerInternal interface {
	CheckAllImages(ctx context.Context, limit int, externalCreds []containerregistry.Credential) (map[string]*imageupdatetypes.Response, error)
}

type registryCredentialLoaderInternal interface {
	GetEnabledRegistryCredentials(ctx context.Context) ([]containerregistry.Credential, error)
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

type imageUpdateMessageKindInternal uint8

const (
	imageUpdateTriggerMessageInternal imageUpdateMessageKindInternal = iota
	imageUpdateScheduleRefreshMessageInternal
	imageUpdateBackfillCompletedMessageInternal
	imageUpdateDebounceElapsedMessageInternal
	imageUpdateScheduledPollMessageInternal
	imageUpdateScanCompletedMessageInternal
	imageUpdateManualScanRequestMessageInternal
	imageUpdateManualScanAdmissionMessageInternal
)

type imageUpdateScanCompletionInternal struct {
	automatic         bool
	triggerGeneration uint64
	done              *actors.Promise[error]
}

// ImageUpdateWatcher continuously reconciles image update state after Docker image changes.
type ImageUpdateWatcher struct {
	imageUpdateService imageUpdateScannerInternal
	settingsService    pollingSettingReaderInternal
	environmentService registryCredentialLoaderInternal
	dockerService      dockerEventBusProviderInternal
	projectService     projectImageRefsBackfillerInternal
	actorRuntime       *actors.Runtime
	actorProcess       atomic.Pointer[actors.Actor]
	stopping           atomic.Bool
	triggerIngress     *actors.Ingress[imageUpdateMessageKindInternal, time.Time]
	scheduleIngress    *actors.Ingress[imageUpdateMessageKindInternal, actors.NoPayload]
	location           *time.Location
	debounce           time.Duration
	backfillRetry      time.Duration
	metadataReady      chan struct{}
	metadataReadyOnce  sync.Once
	started            chan struct{}
	startedOnce        sync.Once
	stopped            chan struct{}
	stoppedOnce        sync.Once
}

// NewImageUpdateWatcher constructs the image update watcher from the existing services.
func NewImageUpdateWatcher(runtime *actors.Runtime, cfg *config.Config, imageUpdateService *imageupdate.ImageUpdateService, settingsService *settings.SettingsService, environmentService *environment.EnvironmentService, dockerService *docker.DockerClientService, projectService *project.ProjectService) (*ImageUpdateWatcher, error) {
	if runtime == nil || imageUpdateService == nil || settingsService == nil || environmentService == nil || dockerService == nil || projectService == nil {
		return nil, errors.New("image update watcher dependencies unavailable")
	}
	location := time.UTC
	if cfg != nil {
		location = cfg.GetLocation()
	}
	return &ImageUpdateWatcher{
		imageUpdateService: imageUpdateService,
		settingsService:    settingsService,
		environmentService: environmentService,
		dockerService:      dockerService,
		projectService:     projectService,
		actorRuntime:       runtime,
		triggerIngress:     actors.NewIngress[imageUpdateMessageKindInternal, time.Time](imageUpdateTriggerMessageInternal),
		scheduleIngress:    actors.NewIngress[imageUpdateMessageKindInternal, actors.NoPayload](imageUpdateScheduleRefreshMessageInternal),
		location:           location,
		debounce:           imageUpdateWatcherDebounce,
		backfillRetry:      imageUpdateWatcherBackfillRetry,
		metadataReady:      make(chan struct{}),
		started:            make(chan struct{}),
		stopped:            make(chan struct{}),
	}, nil
}

// Name identifies the watcher in scheduler lifecycle logs.
func (w *ImageUpdateWatcher) Name() string {
	return "image-polling"
}

// Start subscribes to Docker image events and owns the watcher actor until ctx is canceled.
func (w *ImageUpdateWatcher) Start(ctx context.Context) error {
	if w.stopping.Load() {
		return errors.New("image update watcher cannot be restarted after stop")
	}
	if w.actorProcess.Load() != nil {
		return errors.New("image update watcher already started")
	}
	if w.debounce <= 0 {
		w.debounce = imageUpdateWatcherDebounce
	}
	if w.backfillRetry <= 0 {
		w.backfillRetry = imageUpdateWatcherBackfillRetry
	}

	eventBus := w.dockerService.EventBus()
	if eventBus == nil {
		return errors.New("docker event bus unavailable")
	}
	eventCh, unsubscribe := eventBus.Subscribe(events.ImageEventType, bus.WithSubscriberBuffer(16))
	defer unsubscribe()

	process, err := actors.NewActor(
		ctx,
		w.actorRuntime,
		"scheduler",
		imageUpdateWatcherActorID,
		3,
		func() actors.Behavior {
			state := &imageUpdateWatcherActorInternal{watcher: w}
			return actors.Behavior{
				Initialize: state.initializeInternal,
				Handle:     state.receiveInternal,
				Cleanup:    state.cleanupInternal,
			}
		},
		w.triggerIngress,
		w.scheduleIngress,
	)
	if err != nil {
		return errors.WrapIf(err, "start image update watcher actor")
	}
	w.actorProcess.Store(process)
	w.startedOnce.Do(func() { close(w.started) })

	slog.InfoContext(ctx, "image update watcher started")
	defer func() {
		w.actorProcess.CompareAndSwap(process, nil)
		stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), imageUpdateWatcherStopTimeout)
		defer cancel()
		if err := process.Stop(stopCtx); err != nil {
			slog.ErrorContext(context.WithoutCancel(ctx), "failed to stop image update watcher actor", "error", err)
		}
		slog.InfoContext(context.WithoutCancel(ctx), "image update watcher stopped")
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-process.Done():
			if ctx.Err() != nil || w.stopping.Load() {
				return nil
			}
			return errors.New("image update watcher actor stopped unexpectedly")
		case message, ok := <-eventCh:
			if !ok {
				slog.WarnContext(ctx, "docker image event subscription closed; scheduled image polling remains active")
				eventCh = nil
				continue
			}
			if message.Action == dockerutil.ImageStateResyncAction || !w.settingsService.GetBoolSetting(ctx, "imageEventWatcherEnabled", false) {
				continue
			}
			w.Trigger()
		}
	}
}

// Stop terminates the watcher actor within the caller's lifecycle deadline.
func (w *ImageUpdateWatcher) Stop(ctx context.Context) error {
	if ctx == nil {
		return errors.New("image update watcher stop context unavailable")
	}
	w.stopping.Store(true)
	w.stoppedOnce.Do(func() { close(w.stopped) })
	process := w.actorProcess.Load()
	if process == nil {
		return nil
	}
	return process.Stop(ctx)
}

// Trigger records a trailing-edge image scan without blocking the event publisher.
func (w *ImageUpdateWatcher) Trigger() {
	w.triggerIngress.Send(time.Now())
}

// RefreshSchedule wakes the actor so it re-reads pollingInterval.
func (w *ImageUpdateWatcher) RefreshSchedule() {
	w.scheduleIngress.Send(actors.NoPayload{})
}

// RunNow requests immediate admission from the watcher actor and waits for that scan.
// An active scan is refused immediately instead of being queued.
func (w *ImageUpdateWatcher) RunNow(ctx context.Context) error {
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

	process := w.actorProcess.Load()
	if process == nil {
		return errors.New("image update watcher is not running")
	}

	admission, err := process.Request[imageUpdateMessageKindInternal, context.Context, *actors.Promise[error]](
		ctx,
		actors.Message[imageUpdateMessageKindInternal, context.Context]{Kind: imageUpdateManualScanRequestMessageInternal, Value: ctx},
	)
	if err != nil {
		return errors.WrapIf(err, "request image scan admission")
	}
	if admission.Kind != imageUpdateManualScanAdmissionMessageInternal {
		return errors.New("invalid image scan admission response")
	}
	if admission.Err != nil {
		return admission.Err
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-admission.Value.Done():
		return err
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
	creds, err := w.environmentService.GetEnabledRegistryCredentials(ctx)
	if err != nil {
		slog.WarnContext(ctx, "failed to load registry credentials for image scan", "error", err.Error())
		creds = nil
	}

	results, err := w.imageUpdateService.CheckAllImages(ctx, 0, creds)
	if err != nil {
		return errors.WrapIf(err, "image scan failed")
	}

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
	slog.InfoContext(ctx, "image scan run completed", "checked", len(results), "updates", updates, "errors", scanErrors)
	return nil
}

func (w *ImageUpdateWatcher) backfillProjectImageRefsInternal(ctx context.Context, attempt int) (actors.NoPayload, error) {
	startedAt := time.Now()
	count, err := w.projectService.BackfillProjectImageRefs(ctx)
	duration := time.Since(startedAt)
	if err == nil {
		slog.InfoContext(ctx, "project image metadata backfill completed", "projects", count, "duration", duration, "attempt", attempt)
		return actors.NoPayload{}, nil
	}
	if ctx.Err() == nil {
		slog.WarnContext(ctx, "project image metadata backfill failed; retrying", "projects", count, "duration", duration, "attempt", attempt, "retryIn", w.backfillRetry, "error", err)
	}
	return actors.NoPayload{}, err
}

type imageUpdateWatcherActorInternal struct {
	watcher           *ImageUpdateWatcher
	scanRunning       bool
	automaticPending  bool
	triggerGeneration uint64
	metadataReady     bool
	debounceTimer     actors.Timer[imageUpdateMessageKindInternal]
	scheduleTimer     actors.Timer[imageUpdateMessageKindInternal]
}

func (a *imageUpdateWatcherActorInternal) initializeInternal(ctx *actors.Context) {
	select {
	case <-a.watcher.metadataReady:
		a.metadataReady = true
	default:
		a.startBackfillInternal(ctx)
	}
	a.resetScheduleInternal(ctx)
}

func (a *imageUpdateWatcherActorInternal) cleanupInternal(*actors.Context) {
	a.debounceTimer.Stop()
	a.scheduleTimer.Stop()
}

func (a *imageUpdateWatcherActorInternal) receiveInternal(ctx *actors.Context, rawMessage any) {
	switch message := rawMessage.(type) {
	case actors.Message[imageUpdateMessageKindInternal, actors.NoPayload]:
		a.receiveSignalInternal(ctx, message)
	case actors.Message[imageUpdateMessageKindInternal, uint64]:
		a.receiveTimerInternal(ctx, message)
	case actors.Message[imageUpdateMessageKindInternal, context.Context]:
		if message.Kind != imageUpdateManualScanRequestMessageInternal {
			return
		}
		if a.scanRunning {
			ctx.Respond(actors.Message[imageUpdateMessageKindInternal, *actors.Promise[error]]{
				Kind: imageUpdateManualScanAdmissionMessageInternal,
				Err:  common.Classify(common.ErrImageScanInProgress, errors.New("an image update check is already in progress")),
			})
			return
		}
		done := actors.NewPromise[error]()
		a.startScanInternal(message.Value, ctx, false, 0, done)
		ctx.Respond(actors.Message[imageUpdateMessageKindInternal, *actors.Promise[error]]{Kind: imageUpdateManualScanAdmissionMessageInternal, Value: done})
	case actors.Message[imageUpdateMessageKindInternal, imageUpdateScanCompletionInternal]:
		defer message.Acknowledge()
		if message.Kind != imageUpdateScanCompletedMessageInternal {
			return
		}
		a.scanRunning = false
		if message.Value.automatic {
			a.watcher.triggerIngress.Acknowledge(message.Value.triggerGeneration)
			a.automaticPending = a.watcher.triggerIngress.Pending()
		}
		if message.Value.automatic && message.Err != nil && !errors.Is(message.Err, context.Canceled) {
			slog.ErrorContext(ctx.Context(), "image update watcher scan failed", "error", message.Err)
		}
		if a.automaticPending {
			a.debounceTimer.Reset(ctx, imageUpdateDebounceElapsedMessageInternal, a.watcher.debounce)
		}
		if message.Value.done != nil {
			message.Value.done.Resolve(message.Err)
		}
	}
}

func (a *imageUpdateWatcherActorInternal) receiveSignalInternal(ctx *actors.Context, message actors.Message[imageUpdateMessageKindInternal, actors.NoPayload]) {
	if message.Kind == imageUpdateTriggerMessageInternal {
		triggeredAt, generation := a.watcher.triggerIngress.Begin()
		a.automaticPending = true
		a.triggerGeneration = generation
		if a.metadataReady && !a.scanRunning {
			a.debounceTimer.Reset(ctx, imageUpdateDebounceElapsedMessageInternal, max(time.Until(triggeredAt.Add(a.watcher.debounce)), 0))
		}
		return
	}
	if message.Kind == imageUpdateScheduleRefreshMessageInternal {
		a.watcher.scheduleIngress.Take()
		a.resetScheduleInternal(ctx)
		return
	}
	if message.Kind != imageUpdateBackfillCompletedMessageInternal {
		return
	}
	defer message.Acknowledge()
	if message.Err != nil {
		slog.ErrorContext(ctx.Context(), "image update metadata backfill worker failed", "error", message.Err)
		return
	}
	a.metadataReady = true
	a.watcher.metadataReadyOnce.Do(func() { close(a.watcher.metadataReady) })
	a.watcher.Trigger()
}

func (a *imageUpdateWatcherActorInternal) receiveTimerInternal(ctx *actors.Context, message actors.Message[imageUpdateMessageKindInternal, uint64]) {
	if message.Kind == imageUpdateDebounceElapsedMessageInternal {
		if !a.debounceTimer.Current(message.Value) || !a.automaticPending || a.scanRunning {
			return
		}
		if remaining := time.Until(a.watcher.triggerIngress.Latest().Add(a.watcher.debounce)); remaining > 0 {
			a.debounceTimer.Reset(ctx, imageUpdateDebounceElapsedMessageInternal, remaining)
			return
		}
		a.startScanInternal(ctx.Context(), ctx, true, a.triggerGeneration, nil)
		return
	}
	if message.Kind != imageUpdateScheduledPollMessageInternal || !a.scheduleTimer.Current(message.Value) {
		return
	}
	a.watcher.Trigger()
	a.resetScheduleInternal(ctx)
}

func (a *imageUpdateWatcherActorInternal) startBackfillInternal(ctx *actors.Context) {
	if a.watcher.backfillRetry <= 0 {
		a.watcher.backfillRetry = imageUpdateWatcherBackfillRetry
	}
	attempt := 0
	worker := actors.Worker[imageUpdateMessageKindInternal, actors.NoPayload]{
		Actor:          ctx,
		WorkContext:    ctx.Context(),
		Label:          "image update metadata backfill worker",
		CompletionKind: imageUpdateBackfillCompletedMessageInternal,
		RetryDelay:     a.watcher.backfillRetry,
	}
	worker.Start(func(workerCtx context.Context) (actors.NoPayload, error) {
		attempt++
		return a.watcher.backfillProjectImageRefsInternal(workerCtx, attempt)
	})
}

func (a *imageUpdateWatcherActorInternal) startScanInternal(scanCtx context.Context, ctx *actors.Context, automatic bool, triggerGeneration uint64, done *actors.Promise[error]) {
	a.scanRunning = true
	worker := actors.Worker[imageUpdateMessageKindInternal, imageUpdateScanCompletionInternal]{
		Actor:          ctx,
		WorkContext:    scanCtx,
		Label:          "image update scan worker",
		CompletionKind: imageUpdateScanCompletedMessageInternal,
		PanicValue:     imageUpdateScanCompletionInternal{automatic: automatic, triggerGeneration: triggerGeneration, done: done},
		ActorStopped: func(_ imageUpdateScanCompletionInternal, err error) {
			done.Resolve(err)
		},
	}
	worker.Start(func(workerCtx context.Context) (imageUpdateScanCompletionInternal, error) {
		return imageUpdateScanCompletionInternal{automatic: automatic, triggerGeneration: triggerGeneration, done: done}, a.watcher.executeScanInternal(workerCtx)
	})
}

func (a *imageUpdateWatcherActorInternal) resetScheduleInternal(ctx *actors.Context) {
	spec := a.watcher.settingsService.GetStringSetting(ctx.Context(), "pollingInterval", imageUpdateWatcherDefaultSchedule)
	schedule, err := cronScheduleParser.Parse(spec)
	if err != nil {
		slog.WarnContext(ctx.Context(), "invalid pollingInterval cron expression; using default schedule", "pollingInterval", spec, "default", imageUpdateWatcherDefaultSchedule, "error", err)
		schedule, err = cronScheduleParser.Parse(imageUpdateWatcherDefaultSchedule)
		if err != nil {
			slog.ErrorContext(ctx.Context(), "failed to parse default image polling schedule", "error", err)
			return
		}
	}

	delay := time.Until(schedule.Next(time.Now().In(a.watcher.location)))
	a.scheduleTimer.Reset(ctx, imageUpdateScheduledPollMessageInternal, delay)
}
