package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	"gorm.io/gorm"
)

const (
	defaultActivityRetentionDays = 30
	defaultActivityHistoryLimit  = 1000
	defaultActivityMessages      = 500
	staleImageUpdateCheckAge     = 6 * time.Hour
)

type ActivityService struct {
	db *database.DB

	subscribersMu sync.RWMutex
	subscribers   map[int]*activitySubscriber
	nextSubID     int

	// running maps an active activity ID to the cancel function of its work
	// context, so cancellation requests can interrupt in-flight work. Entries
	// are added by Track and removed when the activity is completed.
	runningMu sync.Mutex
	running   map[string]context.CancelCauseFunc

	// limiter bounds concurrent queue-opted activities per environment.
	// slotReleases maps an activity ID to the release func of the slot it
	// holds; the slot is freed when the activity completes.
	limiter      *activitySlotLimiter
	slotMu       sync.Mutex
	slotReleases map[string]func()
}

// ErrActivityNotCancelable indicates the activity has already reached a terminal
// state and can no longer be cancelled.
var ErrActivityNotCancelable = errors.New("activity is not cancelable")

// subscriberMessageQueueLimit bounds the per-subscriber backlog of "message"
// events; the oldest message is dropped (and flagged as missed) on overflow.
const subscriberMessageQueueLimit = 256

// activitySubscriber buffers stream events between publishers and one stream
// consumer. "activity" events are coalesced in place per activity ID (only the
// latest pending state matters to the UI), so bulk operations emitting rapid
// progress updates cannot overflow the subscriber and force full-snapshot
// resends. Other events keep arrival order in a FIFO bounded by
// subscriberMessageQueueLimit with drop-oldest on overflow.
type activitySubscriber struct {
	environmentID string
	ch            chan activitytypes.StreamEvent
	done          chan struct{}
	wake          chan struct{}

	mu              sync.Mutex
	missed          bool
	queue           []*pendingStreamEvent
	pendingActivity map[string]*pendingStreamEvent
	messageCount    int
}

type pendingStreamEvent struct {
	event activitytypes.StreamEvent
}

func newActivitySubscriberInternal(environmentID string, ch chan activitytypes.StreamEvent) *activitySubscriber {
	return &activitySubscriber{
		environmentID:   environmentID,
		ch:              ch,
		done:            make(chan struct{}),
		wake:            make(chan struct{}, 1),
		pendingActivity: map[string]*pendingStreamEvent{},
	}
}

func isCoalescableEventInternal(event activitytypes.StreamEvent) bool {
	return event.Type == "activity" && event.ActivityID != ""
}

func (sub *activitySubscriber) enqueue(event activitytypes.StreamEvent) {
	sub.mu.Lock()
	if isCoalescableEventInternal(event) {
		if pending, ok := sub.pendingActivity[event.ActivityID]; ok {
			pending.event = event
			sub.mu.Unlock()
			return
		}
	} else {
		if sub.messageCount >= subscriberMessageQueueLimit {
			sub.dropOldestMessageLockedInternal()
		}
		sub.messageCount++
	}
	entry := &pendingStreamEvent{event: event}
	sub.queue = append(sub.queue, entry)
	if isCoalescableEventInternal(event) {
		sub.pendingActivity[event.ActivityID] = entry
	}
	sub.mu.Unlock()

	select {
	case sub.wake <- struct{}{}:
	default:
	}
}

func (sub *activitySubscriber) dropOldestMessageLockedInternal() {
	for i, entry := range sub.queue {
		if !isCoalescableEventInternal(entry.event) {
			sub.queue = append(sub.queue[:i], sub.queue[i+1:]...)
			sub.messageCount--
			sub.missed = true
			slog.Warn("activity subscriber message buffer full; snapshot will be sent on next heartbeat", "environmentId", sub.environmentID)
			return
		}
	}
}

func (sub *activitySubscriber) nextInternal() (activitytypes.StreamEvent, bool) {
	sub.mu.Lock()
	defer sub.mu.Unlock()

	if len(sub.queue) == 0 {
		return activitytypes.StreamEvent{}, false
	}
	entry := sub.queue[0]
	sub.queue = sub.queue[1:]
	event := entry.event
	if isCoalescableEventInternal(event) {
		if sub.pendingActivity[event.ActivityID] == entry {
			delete(sub.pendingActivity, event.ActivityID)
		}
	} else {
		sub.messageCount--
	}
	return event, true
}

func (sub *activitySubscriber) pump() {
	defer close(sub.ch)
	for {
		event, ok := sub.nextInternal()
		if !ok {
			select {
			case <-sub.wake:
				continue
			case <-sub.done:
				return
			}
		}
		select {
		case sub.ch <- event:
		case <-sub.done:
			return
		}
	}
}

type StartActivityRequest = activitylib.StartRequest
type UpdateActivityRequest = activitylib.UpdateRequest
type AppendActivityMessageRequest = activitylib.AppendMessageRequest

func NewActivityService(db *database.DB, settingsService *SettingsService) *ActivityService {
	return &ActivityService{
		db:           db,
		subscribers:  map[int]*activitySubscriber{},
		running:      map[string]context.CancelCauseFunc{},
		limiter:      newActivitySlotLimiterInternal(settingsService),
		slotReleases: map[string]func(){},
	}
}

// Track derives a cancelable work context bound to activityID and registers its
// cancel function so RequestCancel can interrupt the work. The registration is
// released when the activity is completed (see CompleteActivity) or when the
// returned context is otherwise no longer needed. Implements activitylib.Tracker.
func (s *ActivityService) Track(ctx context.Context, activityID string) context.Context {
	activityID = strings.TrimSpace(activityID)
	if s == nil || activityID == "" {
		return ctx
	}

	workCtx, cancel := context.WithCancelCause(ctx)
	s.runningMu.Lock()
	if s.running == nil {
		s.running = map[string]context.CancelCauseFunc{}
	}
	if existing, ok := s.running[activityID]; ok {
		// Replace any stale registration to avoid leaking the prior context.
		existing(nil)
	}
	s.running[activityID] = cancel
	s.runningMu.Unlock()
	return workCtx
}

// RequestCancel cancels the work context registered for activityID, signalling
// activitylib.ErrCanceled as the cause. It returns whether a running activity
// was found in this process.
func (s *ActivityService) RequestCancel(activityID string) bool {
	activityID = strings.TrimSpace(activityID)
	if s == nil || activityID == "" {
		return false
	}

	s.runningMu.Lock()
	cancel, ok := s.running[activityID]
	s.runningMu.Unlock()
	if !ok {
		return false
	}
	cancel(activitylib.ErrCanceled)
	return true
}

// releaseCancelInternal removes and cancels the registration for activityID.
// Cancelling with a nil cause is a no-op if the context was already cancelled
// (the first cause wins), so a prior ErrCanceled cause is preserved.
func (s *ActivityService) releaseCancelInternal(activityID string) {
	activityID = strings.TrimSpace(activityID)
	if s == nil || activityID == "" {
		return
	}

	s.runningMu.Lock()
	cancel, ok := s.running[activityID]
	if ok {
		delete(s.running, activityID)
	}
	s.runningMu.Unlock()
	if ok {
		cancel(nil)
	}
}

func (s *ActivityService) checkInitInternal() error {
	if s == nil || s.db == nil {
		return errors.New("activity service not initialized")
	}
	return nil
}

func (s *ActivityService) StartActivity(ctx context.Context, req StartActivityRequest) (*activitytypes.Activity, error) {
	if err := s.checkInitInternal(); err != nil {
		return nil, err
	}

	now := time.Now()
	environmentID := strings.TrimSpace(req.EnvironmentID)
	if environmentID == "" {
		environmentID = "0"
	}

	var startedByUserID, startedByUsername, startedByDisplayName *string
	if req.StartedBy != nil {
		startedByUserID = utils.StringPtrFromTrimmed(req.StartedBy.ID)
		startedByUsername = utils.StringPtrFromTrimmed(req.StartedBy.Username)
		if req.StartedBy.DisplayName != nil {
			startedByDisplayName = utils.StringPtrFromTrimmed(*req.StartedBy.DisplayName)
		}
	}

	batchID := req.BatchID
	if batchID == nil {
		if contextBatchID := utils.ActivityBatchIDFromContext(ctx); contextBatchID != "" {
			batchID = &contextBatchID
		}
	}

	// Queue-opted activities take a concurrency slot up front when one is
	// free; otherwise they are created as queued and AwaitActivitySlot blocks
	// until a slot opens.
	status := models.ActivityStatusRunning
	var slotRelease func()
	if req.Queue {
		if release, ok := s.limiter.tryAcquireInternal(ctx, environmentID); ok {
			slotRelease = release
		} else {
			status = models.ActivityStatusQueued
		}
	}

	model := &models.Activity{
		EnvironmentID:        environmentID,
		BatchID:              copyPtrInternal(batchID),
		Type:                 req.Type,
		Status:               status,
		ResourceType:         copyPtrInternal(req.ResourceType),
		ResourceID:           copyPtrInternal(req.ResourceID),
		ResourceName:         copyPtrInternal(req.ResourceName),
		StartedByUserID:      startedByUserID,
		StartedByUsername:    startedByUsername,
		StartedByDisplayName: startedByDisplayName,
		Progress:             clampProgressPtrInternal(req.Progress),
		Step:                 strings.TrimSpace(req.Step),
		LatestMessage:        strings.TrimSpace(req.LatestMessage),
		StartedAt:            now,
		Metadata:             cloneJSONInternal(req.Metadata),
		BaseModel: models.BaseModel{
			CreatedAt: now,
		},
	}
	if model.Type == "" {
		model.Type = models.ActivityTypeAutoUpdate
	}

	if err := s.db.WithContext(ctx).Create(model).Error; err != nil {
		if slotRelease != nil {
			slotRelease()
		}
		return nil, fmt.Errorf("failed to create activity: %w", err)
	}
	if slotRelease != nil {
		s.registerSlotReleaseInternal(model.ID, slotRelease)
	}

	dto := activityToDTOInternal(model)
	s.publishActivityInternal(dto)
	return &dto, nil
}

func (s *ActivityService) registerSlotReleaseInternal(activityID string, release func()) {
	s.slotMu.Lock()
	if existing, ok := s.slotReleases[activityID]; ok {
		existing()
	}
	s.slotReleases[activityID] = release
	s.slotMu.Unlock()
}

func (s *ActivityService) releaseSlotInternal(activityID string) {
	if s == nil {
		return
	}
	s.slotMu.Lock()
	release, ok := s.slotReleases[activityID]
	if ok {
		delete(s.slotReleases, activityID)
	}
	s.slotMu.Unlock()
	if ok {
		release()
	}
}

// AwaitActivitySlot blocks until the queued activity holds a concurrency slot,
// then flips its status to running. It returns immediately when the activity
// already took a slot at creation. On cancellation the context cause is
// returned and the activity stays queued for its caller to finalize.
// Implements activitylib.SlotWaiter.
func (s *ActivityService) AwaitActivitySlot(ctx context.Context, activityID, environmentID string) error {
	if err := s.checkInitInternal(); err != nil {
		return err
	}
	activityID = strings.TrimSpace(activityID)
	if activityID == "" {
		return errors.New("activity id is required")
	}
	environmentID = strings.TrimSpace(environmentID)
	if environmentID == "" {
		environmentID = "0"
	}

	s.slotMu.Lock()
	_, held := s.slotReleases[activityID]
	s.slotMu.Unlock()
	if held {
		return nil
	}

	release, err := s.limiter.acquireInternal(ctx, environmentID)
	if err != nil {
		return err
	}
	s.registerSlotReleaseInternal(activityID, release)

	if _, updateErr := s.UpdateActivity(ctx, activityID, UpdateActivityRequest{Status: models.ActivityStatusRunning}); updateErr != nil {
		slog.Warn("failed to mark queued activity running", "activityId", activityID, "error", updateErr)
	}
	return nil
}

func (s *ActivityService) UpdateActivity(ctx context.Context, activityID string, req UpdateActivityRequest) (*activitytypes.Activity, error) {
	if err := s.checkInitInternal(); err != nil {
		return nil, err
	}
	activityID = strings.TrimSpace(activityID)
	if activityID == "" {
		return nil, errors.New("activity id is required")
	}

	updates := map[string]any{
		"updated_at": time.Now(),
	}
	if req.Status != "" {
		updates["status"] = req.Status
	}
	if req.Progress != nil {
		updates["progress"] = *clampProgressPtrInternal(req.Progress)
	}
	if req.Step != nil {
		updates["step"] = strings.TrimSpace(*req.Step)
	}
	if req.LatestMessage != nil {
		updates["latest_message"] = strings.TrimSpace(*req.LatestMessage)
	}
	if req.Error != nil {
		updates["error"] = strings.TrimSpace(*req.Error)
	}
	if req.Metadata != nil {
		updates["metadata"] = cloneJSONInternal(req.Metadata)
	}

	var model models.Activity
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&models.Activity{}).Where("id = ?", activityID).Updates(updates)
		if result.Error != nil {
			return fmt.Errorf("failed to update activity: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return errors.New("activity not found")
		}
		if err := tx.First(&model, "id = ?", activityID).Error; err != nil {
			return fmt.Errorf("failed to load updated activity: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	dto := activityToDTOInternal(&model)
	s.publishActivityInternal(dto)
	return &dto, nil
}

func (s *ActivityService) AppendMessage(ctx context.Context, activityID string, req AppendActivityMessageRequest) (*activitytypes.Message, error) {
	if err := s.checkInitInternal(); err != nil {
		return nil, err
	}
	activityID = strings.TrimSpace(activityID)
	if activityID == "" {
		return nil, errors.New("activity id is required")
	}

	messageText := strings.TrimSpace(req.Message)
	if messageText == "" {
		return nil, nil
	}
	if len(messageText) > 8192 {
		messageText = messageText[:8192]
	}

	level := req.Level
	if level == "" {
		level = models.ActivityMessageLevelInfo
	}

	now := time.Now()
	message := &models.ActivityMessage{
		ActivityID: activityID,
		Level:      level,
		Message:    messageText,
		Payload:    cloneJSONInternal(req.Payload),
		BaseModel: models.BaseModel{
			CreatedAt: now,
		},
	}

	var updated models.Activity
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(message).Error; err != nil {
			return fmt.Errorf("failed to append activity message: %w", err)
		}

		updates := map[string]any{
			"latest_message": messageText,
			"updated_at":     now,
		}
		if req.Progress != nil {
			updates["progress"] = *clampProgressPtrInternal(req.Progress)
		}
		if strings.TrimSpace(req.Step) != "" {
			updates["step"] = strings.TrimSpace(req.Step)
		}

		result := tx.Model(&models.Activity{}).Where("id = ?", activityID).Updates(updates)
		if result.Error != nil {
			return fmt.Errorf("failed to update activity latest message: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return errors.New("activity not found")
		}
		if err := tx.First(&updated, "id = ?", activityID).Error; err != nil {
			return fmt.Errorf("failed to load updated activity: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	dto := activityMessageToDTOInternal(message)
	s.publishMessageInternal(updated.EnvironmentID, dto)
	s.publishActivityInternal(activityToDTOInternal(&updated))
	return &dto, nil
}

func (s *ActivityService) CompleteActivity(ctx context.Context, activityID string, status models.ActivityStatus, finalMessage string, errMessage *string, finalStep ...string) (*activitytypes.Activity, error) {
	if err := s.checkInitInternal(); err != nil {
		return nil, err
	}
	if status == "" {
		status = models.ActivityStatusSuccess
	}
	if status != models.ActivityStatusSuccess && status != models.ActivityStatusFailed && status != models.ActivityStatusCancelled {
		status = models.ActivityStatusSuccess
	}

	activityID = strings.TrimSpace(activityID)
	if activityID == "" {
		return nil, errors.New("activity id is required")
	}

	// The activity is reaching a terminal state; release any cancel
	// registration and free its concurrency slot.
	s.releaseCancelInternal(activityID)
	s.releaseSlotInternal(activityID)

	// Detach from cancellation so the terminal write always lands — completion is
	// often triggered precisely because the work context was cancelled.
	ctx = context.WithoutCancel(ctx)

	now := time.Now()
	var model models.Activity
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&model, "id = ?", activityID).Error; err != nil {
			return fmt.Errorf("failed to load activity: %w", err)
		}

		updates := completeActivityUpdatesInternal(model.StartedAt, status, finalMessage, errMessage, finalStep, now)
		if err := tx.Model(&models.Activity{}).Where("id = ?", activityID).Updates(updates).Error; err != nil {
			return fmt.Errorf("failed to complete activity: %w", err)
		}
		if err := tx.First(&model, "id = ?", activityID).Error; err != nil {
			return fmt.Errorf("failed to load completed activity: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	if strings.TrimSpace(finalMessage) != "" {
		level := models.ActivityMessageLevelSuccess
		switch status {
		case models.ActivityStatusFailed:
			level = models.ActivityMessageLevelError
		case models.ActivityStatusCancelled:
			level = models.ActivityMessageLevelWarning
		case models.ActivityStatusQueued, models.ActivityStatusRunning, models.ActivityStatusSuccess:
		}
		activityCtx := utils.ActivityRuntimeContext(ctx, nil)
		if _, err := s.AppendMessage(activityCtx, activityID, AppendActivityMessageRequest{
			Level:   level,
			Message: finalMessage,
		}); err != nil {
			slog.DebugContext(ctx, "failed to append final activity message", "activityId", activityID, "error", err)
		}
		if err := s.db.WithContext(activityCtx).First(&model, "id = ?", activityID).Error; err != nil {
			slog.DebugContext(ctx, "failed to reload activity after appending message", "activityId", activityID, "error", err)
		}
	}

	dto := activityToDTOInternal(&model)
	s.publishActivityInternal(dto)
	return &dto, nil
}

// CancelActivity requests cancellation of a running or queued activity. When the
// activity's work is running in this process it interrupts it (the work finalizes
// its own terminal status); otherwise it marks the activity cancelled directly,
// but only if it is still active. Returns ErrActivityNotCancelable if the activity
// has already reached a terminal state, or gorm.ErrRecordNotFound if it is unknown.
func (s *ActivityService) CancelActivity(ctx context.Context, environmentID, activityID, requestedBy string) (*activitytypes.Activity, error) {
	if err := s.checkInitInternal(); err != nil {
		return nil, err
	}
	activityID = strings.TrimSpace(activityID)
	if activityID == "" {
		return nil, errors.New("activity id is required")
	}
	environmentID = strings.TrimSpace(environmentID)
	if environmentID == "" {
		environmentID = "0"
	}

	var model models.Activity
	if err := s.db.WithContext(ctx).Where("id = ? AND environment_id = ?", activityID, environmentID).First(&model).Error; err != nil {
		return nil, err
	}
	switch model.Status {
	case models.ActivityStatusSuccess, models.ActivityStatusFailed, models.ActivityStatusCancelled:
		return nil, ErrActivityNotCancelable
	case models.ActivityStatusQueued, models.ActivityStatusRunning:
		// Active states — cancellation can proceed.
	}

	requestedBy = strings.TrimSpace(requestedBy)
	if requestedBy == "" {
		requestedBy = "a user"
	}
	writeCtx := utils.ActivityRuntimeContext(ctx, nil)
	if _, err := s.AppendMessage(writeCtx, activityID, AppendActivityMessageRequest{
		Level:   models.ActivityMessageLevelWarning,
		Message: "Cancellation requested by " + requestedBy,
	}); err != nil {
		slog.DebugContext(ctx, "failed to append cancellation message", "activityId", activityID, "error", err)
	}

	if s.RequestCancel(activityID) {
		// The running work observes the cancelled context and writes its own
		// terminal status, which reaches clients via the activity stream. Return
		// the pre-cancel snapshot rather than reloading here: the worker has not
		// finished unwinding yet, so a reload would still report "running".
		return new(activityToDTOInternal(&model)), nil
	}

	// Untracked work (e.g. after a process restart, or a queued activity with no
	// runner): finalize directly, but only if it is still active to avoid
	// clobbering a concurrently-completing activity.
	now := time.Now()
	var finalized models.Activity
	if err := s.db.WithContext(writeCtx).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&finalized, "id = ? AND environment_id = ?", activityID, environmentID).Error; err != nil {
			return err
		}
		updates := completeActivityUpdatesInternal(finalized.StartedAt, models.ActivityStatusCancelled, cancelledMessageInternal, nil, nil, now)
		result := tx.Model(&models.Activity{}).
			Where("id = ? AND status IN ?", activityID, []models.ActivityStatus{models.ActivityStatusQueued, models.ActivityStatusRunning}).
			Updates(updates)
		if result.Error != nil {
			return fmt.Errorf("failed to cancel activity: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return ErrActivityNotCancelable
		}
		if err := tx.First(&finalized, "id = ? AND environment_id = ?", activityID, environmentID).Error; err != nil {
			return fmt.Errorf("failed to load cancelled activity: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	dto := activityToDTOInternal(&finalized)
	s.publishActivityInternal(dto)
	return &dto, nil
}

const cancelledMessageInternal = "Cancelled by user"

// FailStaleImageUpdateChecks marks image update checks that were left running
// across a prior process lifetime as failed. It intentionally scopes cleanup to
// old image-update-check activities so startup repair cannot affect other work.
func (s *ActivityService) FailStaleImageUpdateChecks(ctx context.Context) (int64, error) {
	if s == nil || s.db == nil {
		return 0, nil
	}

	cutoff := time.Now().Add(-staleImageUpdateCheckAge)
	var staleChecks []models.Activity
	if err := s.db.WithContext(ctx).
		Where("type = ? AND status = ? AND started_at < ?", models.ActivityTypeImageUpdateCheck, models.ActivityStatusRunning, cutoff).
		Find(&staleChecks).Error; err != nil {
		return 0, fmt.Errorf("find stale image update checks: %w", err)
	}

	const message = "Image update check failed because it was stale after Arcane restarted"
	errMessage := message
	var failed int64
	var failErrs []error
	for i := range staleChecks {
		if _, err := s.CompleteActivity(ctx, staleChecks[i].ID, models.ActivityStatusFailed, message, &errMessage, "Image update check failed"); err != nil {
			failErrs = append(failErrs, fmt.Errorf("fail stale image update check %s: %w", staleChecks[i].ID, err))
			continue
		}
		failed++
	}

	return failed, errors.Join(failErrs...)
}

// ResolveOrphanedQueuedActivities fails any activity still queued at startup.
// Queued state is owned by a live goroutine blocked on AwaitActivitySlot, so a
// queued row after a restart can never start running.
func (s *ActivityService) ResolveOrphanedQueuedActivities(ctx context.Context) (int64, error) {
	if s == nil || s.db == nil {
		return 0, nil
	}

	var queued []models.Activity
	if err := s.db.WithContext(ctx).
		Where("status = ?", models.ActivityStatusQueued).
		Find(&queued).Error; err != nil {
		return 0, fmt.Errorf("find orphaned queued activities: %w", err)
	}

	const message = "Queued activity was interrupted by an Arcane restart"
	errMessage := message
	var failed int64
	var failErrs []error
	for i := range queued {
		if _, err := s.CompleteActivity(ctx, queued[i].ID, models.ActivityStatusFailed, message, &errMessage); err != nil {
			failErrs = append(failErrs, fmt.Errorf("fail orphaned queued activity %s: %w", queued[i].ID, err))
			continue
		}
		failed++
	}

	return failed, errors.Join(failErrs...)
}

// PatchActivityMetadata merges patch into the activity's existing metadata,
// unlike UpdateActivity which replaces the metadata wholesale.
func (s *ActivityService) PatchActivityMetadata(ctx context.Context, activityID string, patch models.JSON) error {
	if err := s.checkInitInternal(); err != nil {
		return err
	}
	activityID = strings.TrimSpace(activityID)
	if activityID == "" {
		return errors.New("activity id is required")
	}
	if len(patch) == 0 {
		return nil
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var activity models.Activity
		if err := tx.First(&activity, "id = ?", activityID).Error; err != nil {
			return fmt.Errorf("failed to load activity: %w", err)
		}
		merged := cloneJSONInternal(activity.Metadata)
		if merged == nil {
			merged = models.JSON{}
		}
		maps.Copy(merged, patch)
		if err := tx.Model(&models.Activity{}).Where("id = ?", activityID).
			Updates(map[string]any{"metadata": merged, "updated_at": time.Now()}).Error; err != nil {
			return fmt.Errorf("failed to patch activity metadata: %w", err)
		}
		return nil
	})
}

// ResolveStaleAutoUpdateActivities finalizes auto-update activities left
// running by a prior process lifetime. A run whose metadata marks a triggered
// self-update completed by restarting Arcane, so it is recorded as success;
// anything else still running at startup was interrupted and is failed.
func (s *ActivityService) ResolveStaleAutoUpdateActivities(ctx context.Context) (int64, error) {
	if s == nil || s.db == nil {
		return 0, nil
	}

	var stale []models.Activity
	if err := s.db.WithContext(ctx).
		Where("type = ? AND status = ?", models.ActivityTypeAutoUpdate, models.ActivityStatusRunning).
		Find(&stale).Error; err != nil {
		return 0, fmt.Errorf("find stale auto-update activities: %w", err)
	}

	var resolved int64
	var resolveErrs []error
	for i := range stale {
		status := models.ActivityStatusFailed
		message := "Auto-update interrupted by Arcane restart"
		var errMessage *string
		if selfUpdate, _ := stale[i].Metadata["selfUpdateTriggered"].(bool); selfUpdate {
			status = models.ActivityStatusSuccess
			message = "Auto-update completed — Arcane restarted with the updated image"
		} else {
			errMessage = new(message)
		}
		if _, err := s.CompleteActivity(ctx, stale[i].ID, status, message, errMessage); err != nil {
			resolveErrs = append(resolveErrs, fmt.Errorf("resolve stale auto-update activity %s: %w", stale[i].ID, err))
			continue
		}
		resolved++
	}

	return resolved, errors.Join(resolveErrs...)
}

func completeActivityUpdatesInternal(startedAt time.Time, status models.ActivityStatus, finalMessage string, errMessage *string, finalStep []string, now time.Time) map[string]any {
	updates := map[string]any{
		"status":      status,
		"ended_at":    now,
		"duration_ms": now.Sub(startedAt).Milliseconds(),
		"updated_at":  now,
	}
	if trimmed := strings.TrimSpace(finalMessage); trimmed != "" {
		updates["latest_message"] = trimmed
	}
	if len(finalStep) > 0 {
		if step := strings.TrimSpace(finalStep[0]); step != "" {
			updates["step"] = step
		}
	}
	if errMessage != nil && strings.TrimSpace(*errMessage) != "" {
		updates["error"] = strings.TrimSpace(*errMessage)
	}
	if status == models.ActivityStatusSuccess {
		updates["progress"] = 100
	}
	return updates
}

func (s *ActivityService) ListActivitiesPaginated(ctx context.Context, environmentID string, params pagination.QueryParams) ([]activitytypes.Activity, pagination.Response, error) {
	if err := s.checkInitInternal(); err != nil {
		return nil, pagination.Response{}, err
	}

	environmentID = strings.TrimSpace(environmentID)
	if environmentID == "" {
		environmentID = "0"
	}

	var activities []models.Activity
	q := s.db.WithContext(ctx).Model(&models.Activity{}).Where("environment_id = ?", environmentID)

	if term := strings.TrimSpace(params.Search); term != "" {
		escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(term)
		searchPattern := "%" + escaped + "%"
		q = q.Where(
			"type LIKE ? ESCAPE '\\' OR COALESCE(resource_name, '') LIKE ? ESCAPE '\\' OR COALESCE(latest_message, '') LIKE ? ESCAPE '\\' OR COALESCE(step, '') LIKE ? ESCAPE '\\' OR COALESCE(error, '') LIKE ? ESCAPE '\\'",
			searchPattern, searchPattern, searchPattern, searchPattern, searchPattern,
		)
	}

	q = pagination.ApplyFilter(q, "status", params.Filters["status"])
	q = pagination.ApplyFilter(q, "type", params.Filters["type"])
	q = pagination.ApplyFilter(q, "resource_type", params.Filters["resourceType"])

	if params.Sort == "" {
		// Active rows sort by created_at (immutable) and terminal rows by ended_at
		// (set once), so a row's position only changes on the active->terminal
		// transition instead of on every progress update.
		q = q.Order("CASE WHEN status IN ('queued', 'running') THEN 0 ELSE 1 END ASC").
			Order("COALESCE(ended_at, created_at) DESC").
			Order("id DESC")
	}

	paginationResp, err := pagination.PaginateAndSortDB(params, q, &activities)
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to paginate activities: %w", err)
	}

	out := make([]activitytypes.Activity, 0, len(activities))
	for i := range activities {
		out = append(out, activityToDTOInternal(&activities[i]))
	}
	return out, paginationResp, nil
}

func (s *ActivityService) GetActivityDetail(ctx context.Context, environmentID, activityID string, limit int) (*activitytypes.Detail, error) {
	if err := s.checkInitInternal(); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > defaultActivityMessages {
		limit = defaultActivityMessages
	}

	var model models.Activity
	if err := s.db.WithContext(ctx).
		Where("id = ? AND environment_id = ?", activityID, environmentID).
		First(&model).Error; err != nil {
		return nil, fmt.Errorf("failed to load activity: %w", err)
	}

	var messages []models.ActivityMessage
	if err := s.db.WithContext(ctx).
		Where("activity_id = ?", activityID).
		Order("created_at DESC").
		Limit(limit).
		Find(&messages).Error; err != nil {
		return nil, fmt.Errorf("failed to load activity messages: %w", err)
	}

	outMessages := make([]activitytypes.Message, 0, len(messages))
	for _, v := range slices.Backward(messages) {
		outMessages = append(outMessages, activityMessageToDTOInternal(&v))
	}

	return &activitytypes.Detail{
		Activity: activityToDTOInternal(&model),
		Messages: outMessages,
	}, nil
}

func (s *ActivityService) PruneHistory(ctx context.Context, retentionDays, maxEntries int) (int64, error) {
	if s == nil || s.db == nil {
		return 0, nil
	}
	if retentionDays < 0 {
		retentionDays = defaultActivityRetentionDays
	}
	if maxEntries < 0 {
		maxEntries = defaultActivityHistoryLimit
	}

	var deleted int64
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if retentionDays > 0 {
			cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)
			ids, err := findTerminalActivityIDsInternal(tx.
				Where("COALESCE(ended_at, updated_at, created_at) < ?", cutoff))
			if err != nil {
				return fmt.Errorf("failed to find activities older than retention window: %w", err)
			}
			count, err := deleteActivitiesByIDInternal(tx, ids)
			if err != nil {
				return err
			}
			deleted += count
		}

		if maxEntries > 0 {
			ids, err := findActivityIDsBeyondHistoryLimitInternal(tx, maxEntries)
			if err != nil {
				return err
			}
			count, err := deleteActivitiesByIDInternal(tx, ids)
			if err != nil {
				return err
			}
			deleted += count
		}

		return nil
	}); err != nil {
		return 0, err
	}

	return deleted, nil
}

func (s *ActivityService) DeleteHistory(ctx context.Context, environmentID string) (int64, error) {
	if s == nil || s.db == nil {
		return 0, nil
	}

	environmentID = strings.TrimSpace(environmentID)
	if environmentID == "" {
		environmentID = "0"
	}

	var deleted int64
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		ids, err := findTerminalActivityIDsInternal(tx.Where("environment_id = ?", environmentID))
		if err != nil {
			return fmt.Errorf("failed to find activity history: %w", err)
		}
		count, err := deleteActivitiesByIDInternal(tx, ids)
		if err != nil {
			return err
		}
		deleted = count
		return nil
	}); err != nil {
		return 0, err
	}

	return deleted, nil
}

func (s *ActivityService) Subscribe(environmentID string) (<-chan activitytypes.StreamEvent, func() bool, func()) {
	ch := make(chan activitytypes.StreamEvent, 64)
	if s == nil {
		close(ch)
		return ch, func() bool { return false }, func() {}
	}

	environmentID = strings.TrimSpace(environmentID)
	if environmentID == "" {
		environmentID = "0"
	}

	sub := newActivitySubscriberInternal(environmentID, ch)
	s.subscribersMu.Lock()
	s.nextSubID++
	id := s.nextSubID
	s.subscribers[id] = sub
	s.subscribersMu.Unlock()
	go sub.pump()

	missedEvents := func() bool {
		s.subscribersMu.RLock()
		sub, ok := s.subscribers[id]
		s.subscribersMu.RUnlock()
		if !ok {
			return false
		}

		sub.mu.Lock()
		defer sub.mu.Unlock()
		if !sub.missed {
			return false
		}
		sub.missed = false
		return true
	}

	unsubscribe := func() {
		s.subscribersMu.Lock()
		sub, ok := s.subscribers[id]
		if ok {
			delete(s.subscribers, id)
		}
		s.subscribersMu.Unlock()
		if ok {
			// The pump goroutine owns ch and closes it on shutdown.
			close(sub.done)
		}
	}

	return ch, missedEvents, unsubscribe
}

func (s *ActivityService) publishActivityInternal(activity activitytypes.Activity) {
	s.publishInternal(activity.EnvironmentID, activitytypes.StreamEvent{
		Type:       "activity",
		ActivityID: activity.ID,
		Activity:   &activity,
		Timestamp:  time.Now(),
	})
}

func (s *ActivityService) publishMessageInternal(environmentID string, message activitytypes.Message) {
	s.publishInternal(environmentID, activitytypes.StreamEvent{
		Type:       "message",
		ActivityID: message.ActivityID,
		Message:    &message,
		Timestamp:  time.Now(),
	})
}

func (s *ActivityService) publishInternal(environmentID string, event activitytypes.StreamEvent) {
	if s == nil {
		return
	}
	s.subscribersMu.RLock()
	subs := make([]*activitySubscriber, 0, len(s.subscribers))
	for _, sub := range s.subscribers {
		if sub.environmentID == environmentID {
			subs = append(subs, sub)
		}
	}
	s.subscribersMu.RUnlock()

	for _, sub := range subs {
		sub.enqueue(event)
	}
}

func activityToDTOInternal(model *models.Activity) activitytypes.Activity {
	if model == nil {
		return activitytypes.Activity{}
	}
	return activitytypes.Activity{
		ID:                  model.ID,
		EnvironmentID:       model.EnvironmentID,
		SourceEnvironmentID: model.EnvironmentID,
		BatchID:             copyPtrInternal(model.BatchID),
		Type:                activitytypes.Type(model.Type),
		Status:              activitytypes.Status(model.Status),
		ResourceType:        copyPtrInternal(model.ResourceType),
		ResourceID:          copyPtrInternal(model.ResourceID),
		ResourceName:        copyPtrInternal(model.ResourceName),
		Progress:            clampProgressPtrInternal(model.Progress),
		Step:                model.Step,
		LatestMessage:       model.LatestMessage,
		StartedBy:           activityStartedByDTOInternal(model),
		StartedAt:           model.StartedAt,
		EndedAt:             copyPtrInternal(model.EndedAt),
		DurationMs:          copyPtrInternal(model.DurationMs),
		Error:               copyPtrInternal(model.Error),
		Metadata:            jsonToMapInternal(model.Metadata),
		CreatedAt:           model.CreatedAt,
		UpdatedAt:           copyPtrInternal(model.UpdatedAt),
	}
}

func activityMessageToDTOInternal(model *models.ActivityMessage) activitytypes.Message {
	if model == nil {
		return activitytypes.Message{}
	}
	return activitytypes.Message{
		ID:         model.ID,
		ActivityID: model.ActivityID,
		Level:      activitytypes.MessageLevel(model.Level),
		Message:    model.Message,
		Payload:    jsonToMapInternal(model.Payload),
		CreatedAt:  model.CreatedAt,
	}
}

func copyPtrInternal[T any](value *T) *T {
	if value == nil {
		return nil
	}
	return new(*value)
}

func clampProgressPtrInternal(value *int) *int {
	if value == nil {
		return nil
	}
	return new(min(max(*value, 0), 100))
}

func cloneJSONInternal(input models.JSON) models.JSON {
	if len(input) == 0 {
		return nil
	}
	out := make(models.JSON, len(input))
	maps.Copy(out, input)
	return out
}

func jsonToMapInternal(input models.JSON) map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]any, len(input))
	maps.Copy(out, input)
	return out
}

func terminalActivityStatusesInternal() []models.ActivityStatus {
	return []models.ActivityStatus{
		models.ActivityStatusSuccess,
		models.ActivityStatusFailed,
		models.ActivityStatusCancelled,
	}
}

func findTerminalActivityIDsInternal(q *gorm.DB) ([]string, error) {
	var activityIDs []string
	if err := q.Model(&models.Activity{}).
		Where("status IN ?", terminalActivityStatusesInternal()).
		Pluck("id", &activityIDs).Error; err != nil {
		return nil, err
	}
	return activityIDs, nil
}

func findActivityIDsBeyondHistoryLimitInternal(tx *gorm.DB, maxEntries int) ([]string, error) {
	var activityIDs []string
	if err := tx.Raw(`
		SELECT ranked.id
		FROM (
			SELECT id,
				ROW_NUMBER() OVER (
					PARTITION BY environment_id
					ORDER BY COALESCE(ended_at, updated_at, created_at) DESC, started_at DESC
				) AS activity_rank
			FROM activities
			WHERE status IN ?
		) ranked
		WHERE ranked.activity_rank > ?
	`, terminalActivityStatusesInternal(), maxEntries).Scan(&activityIDs).Error; err != nil {
		return nil, fmt.Errorf("failed to find excess activities: %w", err)
	}
	return activityIDs, nil
}

const deleteActivitiesBatchSize = 500

func deleteActivitiesByIDInternal(tx *gorm.DB, activityIDs []string) (int64, error) {
	if len(activityIDs) == 0 {
		return 0, nil
	}

	var totalDeleted int64
	for i := 0; i < len(activityIDs); i += deleteActivitiesBatchSize {
		end := min(i+deleteActivitiesBatchSize, len(activityIDs))
		batch := activityIDs[i:end]

		if err := tx.Where("activity_id IN ?", batch).Delete(&models.ActivityMessage{}).Error; err != nil {
			return totalDeleted, fmt.Errorf("failed to delete activity messages: %w", err)
		}
		result := tx.Where("id IN ?", batch).Delete(&models.Activity{})
		if result.Error != nil {
			return totalDeleted, fmt.Errorf("failed to delete activities: %w", result.Error)
		}
		totalDeleted += result.RowsAffected
	}

	return totalDeleted, nil
}

func activityStartedByDTOInternal(model *models.Activity) *activitytypes.StartedBy {
	if model.StartedByUsername == nil || strings.TrimSpace(*model.StartedByUsername) == "" {
		return &activitytypes.StartedBy{Username: "System"}
	}

	startedBy := &activitytypes.StartedBy{
		Username: strings.TrimSpace(*model.StartedByUsername),
	}
	if model.StartedByUserID != nil {
		startedBy.UserID = strings.TrimSpace(*model.StartedByUserID)
	}
	if model.StartedByDisplayName != nil {
		startedBy.DisplayName = strings.TrimSpace(*model.StartedByDisplayName)
	}
	return startedBy
}
