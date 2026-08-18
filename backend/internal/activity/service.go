package activity

import (
	"context"
	stderrors "errors"
	"hash/fnv"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/timeouts"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/dbutil"
	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	"github.com/samber/mo"
	"gorm.io/gorm"
)

const (
	defaultActivityRetentionDays = 30
	defaultActivityHistoryLimit  = 1000
	defaultActivityMessages      = 500
	staleImageUpdateCheckAge     = 6 * time.Hour
	// abandonedActivityGrace is how old an untracked queued/running activity
	// must be before the periodic sweep fails it. It covers the window between
	// StartActivity's row creation and the worker's Track call.
	abandonedActivityGrace = 2 * time.Minute
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

	// terminalPublished records when each activity's terminal snapshot was
	// published, so later non-terminal snapshots from slower goroutines can
	// be dropped (see admitActivityPublishInternal). Entries are pruned on
	// terminal publishes once older than terminalPublishRetention.
	terminalPublishedMu sync.Mutex
	terminalPublished   map[string]time.Time

	// publishLocks serializes the commit→publish window of the non-terminal
	// snapshot writers (AppendMessages, UpdateActivity) per activity, so their
	// events reach subscribers in commit order — without it, a batch that
	// commits first but publishes second would revert progress/step/status
	// fields a concurrent update already streamed. Terminal snapshots are
	// ordered separately by admitActivityPublishInternal. Striped by activity
	// ID hash so no per-activity lifecycle management is needed.
	publishLocks [64]sync.Mutex
}

// terminalPublishRetention bounds how long a terminal publish suppresses
// stale non-terminal snapshots for the same activity. Stale publishers are
// goroutines already past their commit, so they publish within moments; the
// retention only has to outlive that gap while keeping the latch map small.
const terminalPublishRetention = 10 * time.Minute

// ErrActivityNotCancelable indicates the activity has already reached a terminal
// state and can no longer be cancelled.
const ErrActivityNotCancelable = errors.Sentinel("activity is not cancelable")

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

func (sub *activitySubscriber) nextInternal() mo.Option[activitytypes.StreamEvent] {
	sub.mu.Lock()
	defer sub.mu.Unlock()

	if len(sub.queue) == 0 {
		return mo.None[activitytypes.StreamEvent]()
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
	return mo.Some(event)
}

func (sub *activitySubscriber) pump() {
	defer close(sub.ch)
	for {
		event, ok := sub.nextInternal().Get()
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

func NewActivityService(db *database.DB, settingsService *settings.SettingsService) *ActivityService {
	return &ActivityService{
		db:                db,
		subscribers:       map[int]*activitySubscriber{},
		running:           map[string]context.CancelCauseFunc{},
		limiter:           newActivitySlotLimiterInternal(settingsService),
		slotReleases:      map[string]func(){},
		terminalPublished: map[string]time.Time{},
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
		startedByUserID = mo.EmptyableToOption(strings.TrimSpace(req.StartedBy.ID)).ToPointer()
		startedByUsername = mo.EmptyableToOption(strings.TrimSpace(req.StartedBy.Username)).ToPointer()
		if req.StartedBy.DisplayName != nil {
			startedByDisplayName = mo.EmptyableToOption(strings.TrimSpace(*req.StartedBy.DisplayName)).ToPointer()
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
	status := activitytypes.StatusRunning
	var slotRelease func()
	if req.Queue {
		if release, ok := s.limiter.tryAcquireInternal(ctx, environmentID).Get(); ok {
			slotRelease = release
		} else {
			status = activitytypes.StatusQueued
		}
	}

	model := &Activity{
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
		BaseModel: database.BaseModel{
			CreatedAt: now,
		},
	}
	if model.Type == "" {
		model.Type = activitytypes.TypeAutoUpdate
	}

	if err := s.db.WithContext(ctx).Create(model).Error; err != nil {
		if slotRelease != nil {
			slotRelease()
		}
		return nil, errors.WrapIf(err, "failed to create activity")
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

	if _, updateErr := s.UpdateActivity(ctx, activityID, UpdateActivityRequest{Status: activitytypes.StatusRunning}); updateErr != nil {
		slog.Warn("failed to mark queued activity running", "activityId", activityID, "error", updateErr)
	}
	return nil
}

// AwaitActivitySlotBounded waits for a concurrency slot like AwaitActivitySlot
// but gives up after timeouts.DefaultActivitySlotWait, returning
// ActivitySlotWaitTimeoutError so the caller fails the queued activity loudly
// instead of parking forever behind long-running slot holders.
func (s *ActivityService) AwaitActivitySlotBounded(ctx context.Context, activityID, environmentID string) error {
	slotCtx, cancel := context.WithTimeout(ctx, timeouts.DefaultActivitySlotWait)
	defer cancel()
	if err := s.AwaitActivitySlot(slotCtx, activityID, environmentID); err != nil {
		if ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded) {
			return common.Classify(common.ErrTimeout, errors.New("timed out waiting for a free activity slot; other long-running activities are holding all slots"))
		}
		return err
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

	// Hold the per-activity publish lock across commit and publish so this
	// snapshot cannot be overtaken on the stream by a concurrent append batch
	// that committed earlier but would publish later.
	lock := s.publishLockInternal(activityID)
	lock.Lock()
	defer lock.Unlock()

	var model Activity
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&Activity{}).Where("id = ?", activityID).Updates(updates)
		if result.Error != nil {
			return errors.WrapIf(result.Error, "failed to update activity")
		}
		if result.RowsAffected == 0 {
			return errors.New("activity not found")
		}
		if err := tx.First(&model, "id = ?", activityID).Error; err != nil {
			return errors.WrapIf(err, "failed to load updated activity")
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
	messages, err := s.AppendMessages(ctx, activityID, []AppendActivityMessageRequest{req})
	if err != nil || len(messages) == 0 {
		return nil, err
	}
	return &messages[0], nil
}

// AppendMessages persists a batch of output lines in one transaction — a
// single multi-row message INSERT plus one coalesced Activity update —
// instead of an INSERT+UPDATE+re-SELECT transaction per line, which turned
// an image pull into hundreds of fsync'd SQLite transactions. The activity
// publish DTO is re-SELECTed inside the transaction after the update (once
// per batch, not per line) so it reflects lifecycle fields a concurrent
// CompleteActivity/UpdateActivity committed first; a terminal write that
// commits after this transaction but publishes before this snapshot is
// handled by admitActivityPublishInternal dropping the stale event.
func (s *ActivityService) AppendMessages(ctx context.Context, activityID string, reqs []AppendActivityMessageRequest) ([]activitytypes.Message, error) {
	if err := s.checkInitInternal(); err != nil {
		return nil, err
	}
	activityID = strings.TrimSpace(activityID)
	if activityID == "" {
		return nil, errors.New("activity id is required")
	}

	messages, updates := buildAppendBatchInternal(activityID, reqs)
	if len(messages) == 0 {
		return nil, nil
	}

	// Hold the per-activity publish lock across commit and publish so this
	// batch's snapshot cannot be overtaken on the stream by a concurrent
	// UpdateActivity that committed later — subscribers must see the two
	// snapshots in commit order.
	lock := s.publishLockInternal(activityID)
	lock.Lock()
	defer lock.Unlock()

	current, err := s.appendBatchWithRetryInternal(ctx, activityID, messages, updates)
	if err != nil {
		return nil, err
	}

	out := make([]activitytypes.Message, 0, len(messages))
	for _, message := range messages {
		dto := activityMessageToDTOInternal(message)
		s.publishMessageInternal(current.EnvironmentID, dto)
		out = append(out, dto)
	}
	s.publishActivityInternal(activityToDTOInternal(current))
	return out, nil
}

// buildAppendBatchInternal turns the request batch into insertable message rows
// plus the coalesced Activity column updates (latest message/progress/step from
// the last request carrying each).
func buildAppendBatchInternal(activityID string, reqs []AppendActivityMessageRequest) ([]*ActivityMessage, map[string]any) {
	now := time.Now()
	messages := make([]*ActivityMessage, 0, len(reqs))
	latestMessage := ""
	var latestProgress *int
	latestStep := ""
	for _, req := range reqs {
		messageText := strings.TrimSpace(req.Message)
		if messageText == "" {
			continue
		}
		if len(messageText) > 8192 {
			messageText = messageText[:8192]
		}

		level := req.Level
		if level == "" {
			level = activitytypes.MessageLevelInfo
		}

		messages = append(messages, &ActivityMessage{
			ActivityID: activityID,
			Level:      level,
			Message:    messageText,
			Payload:    cloneJSONInternal(req.Payload),
			BaseModel: database.BaseModel{
				// Spread inside the shared timestamp so the created_at sort
				// used for retrieval keeps the original line order.
				CreatedAt: now.Add(time.Duration(len(messages)) * time.Microsecond),
			},
		})

		latestMessage = messageText
		if req.Progress != nil {
			latestProgress = req.Progress
		}
		if step := strings.TrimSpace(req.Step); step != "" {
			latestStep = step
		}
	}
	if len(messages) == 0 {
		return nil, nil
	}

	updates := map[string]any{
		"latest_message": latestMessage,
		"updated_at":     now,
	}
	if latestProgress != nil {
		updates["progress"] = *clampProgressPtrInternal(latestProgress)
	}
	if latestStep != "" {
		updates["step"] = latestStep
	}
	return messages, updates
}

// appendBatchWithRetryInternal commits the batch INSERT plus Activity update in
// one transaction. Bulk appends contend with terminal-status writes for the
// SQLite write lock, and a batch lost to transient SQLITE_BUSY silently drops
// up to 32 output lines — the Writer drain has no error path to replay them —
// so retry like CompleteActivity does.
func (s *ActivityService) appendBatchWithRetryInternal(ctx context.Context, activityID string, messages []*ActivityMessage, updates map[string]any) (*Activity, error) {
	var current Activity
	const appendWriteAttempts = 3
	var writeErr error
	for attempt := 1; attempt <= appendWriteAttempts; attempt++ {
		writeErr = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.First(&current, "id = ?", activityID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return errors.New("activity not found")
				}
				return errors.WrapIf(err, "failed to load activity")
			}

			if err := tx.Create(&messages).Error; err != nil {
				return errors.WrapIf(err, "failed to append activity message")
			}

			result := tx.Model(&Activity{}).Where("id = ?", activityID).Updates(updates)
			if result.Error != nil {
				return errors.WrapIf(result.Error, "failed to update activity latest message")
			}
			if result.RowsAffected == 0 {
				return errors.New("activity not found")
			}
			if err := tx.First(&current, "id = ?", activityID).Error; err != nil {
				return errors.WrapIf(err, "failed to load updated activity")
			}
			return nil
		})
		if writeErr == nil {
			return &current, nil
		}
		if attempt == appendWriteAttempts || !dbutil.IsRetryableWriteError(writeErr) {
			return nil, writeErr
		}
		slog.WarnContext(ctx, "retrying activity message batch write", "activityId", activityID, "attempt", attempt, "error", writeErr)
		time.Sleep(250 * time.Millisecond * time.Duration(attempt))
	}
	return nil, writeErr
}

func (s *ActivityService) CompleteActivity(ctx context.Context, activityID string, status activitytypes.Status, finalMessage string, errMessage *string, finalStep ...string) (*activitytypes.Activity, error) {
	if err := s.checkInitInternal(); err != nil {
		return nil, err
	}
	if status == "" {
		status = activitytypes.StatusSuccess
	}
	if status != activitytypes.StatusSuccess && status != activitytypes.StatusFailed && status != activitytypes.StatusCancelled {
		status = activitytypes.StatusSuccess
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
	var model Activity
	// A lost terminal write leaves the activity stuck in running forever, so
	// retry transient DB contention (SQLITE_BUSY under bulk activity writes)
	// instead of surfacing it once and giving up.
	const completeWriteAttempts = 3
	var writeErr error
	for attempt := 1; attempt <= completeWriteAttempts; attempt++ {
		writeErr = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.First(&model, "id = ?", activityID).Error; err != nil {
				return errors.WrapIf(err, "failed to load activity")
			}

			updates := completeActivityUpdatesInternal(model.StartedAt, status, finalMessage, errMessage, finalStep, now)
			if err := tx.Model(&Activity{}).Where("id = ?", activityID).Updates(updates).Error; err != nil {
				return errors.WrapIf(err, "failed to complete activity")
			}
			if err := tx.First(&model, "id = ?", activityID).Error; err != nil {
				return errors.WrapIf(err, "failed to load completed activity")
			}
			return nil
		})
		if writeErr == nil {
			break
		}
		if attempt == completeWriteAttempts || !dbutil.IsRetryableWriteError(writeErr) {
			return nil, writeErr
		}
		slog.WarnContext(ctx, "retrying activity terminal status write", "activityId", activityID, "attempt", attempt, "error", writeErr)
		time.Sleep(250 * time.Millisecond * time.Duration(attempt))
	}
	if writeErr != nil {
		return nil, writeErr
	}

	if strings.TrimSpace(finalMessage) != "" {
		level := activitytypes.MessageLevelSuccess
		switch status {
		case activitytypes.StatusFailed:
			level = activitytypes.MessageLevelError
		case activitytypes.StatusCancelled:
			level = activitytypes.MessageLevelWarning
		case activitytypes.StatusQueued, activitytypes.StatusRunning, activitytypes.StatusSuccess:
		}
		activityCtx := utils.ActivityRuntimeContext(ctx, nil)
		if _, err := s.AppendMessage(activityCtx, activityID, AppendActivityMessageRequest{
			Level:   level,
			Message: finalMessage,
		}); err != nil {
			slog.DebugContext(ctx, "failed to append final activity message", "activityId", activityID, "error", err)
		}
	}

	dto := s.publishTerminalSnapshotInternal(ctx, &model)
	return &dto, nil
}

// publishTerminalSnapshotInternal publishes an activity's terminal event from
// a snapshot re-read under the per-activity publish lock. Terminal writers
// cannot hold that lock across their own transactions (CompleteActivity
// appends its final message through the locked AppendMessages path), so a
// snapshot captured in-transaction can be overtaken by an append or update
// that commits later but publishes first; re-reading under the lock
// guarantees the terminal event carries every field already streamed. The
// passed model is published as-is if the re-read fails, and is updated in
// place otherwise so callers return the published state.
func (s *ActivityService) publishTerminalSnapshotInternal(ctx context.Context, model *Activity) activitytypes.Activity {
	lock := s.publishLockInternal(model.ID)
	lock.Lock()
	defer lock.Unlock()

	var fresh Activity
	if err := s.db.WithContext(ctx).First(&fresh, "id = ?", model.ID).Error; err != nil {
		slog.DebugContext(ctx, "failed to reload activity for terminal publish", "activityId", model.ID, "error", err)
	} else {
		*model = fresh
	}
	dto := activityToDTOInternal(model)
	s.publishActivityInternal(dto)
	return dto
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

	var model Activity
	if err := s.db.WithContext(ctx).Where("id = ? AND environment_id = ?", activityID, environmentID).First(&model).Error; err != nil {
		return nil, err
	}
	switch model.Status {
	case activitytypes.StatusSuccess, activitytypes.StatusFailed, activitytypes.StatusCancelled:
		return nil, ErrActivityNotCancelable
	case activitytypes.StatusQueued, activitytypes.StatusRunning:
		// Active states — cancellation can proceed.
	}

	requestedBy = strings.TrimSpace(requestedBy)
	if requestedBy == "" {
		requestedBy = "a user"
	}
	writeCtx := utils.ActivityRuntimeContext(ctx, nil)
	if _, err := s.AppendMessage(writeCtx, activityID, AppendActivityMessageRequest{
		Level:   activitytypes.MessageLevelWarning,
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
	var finalized Activity
	if err := s.db.WithContext(writeCtx).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&finalized, "id = ? AND environment_id = ?", activityID, environmentID).Error; err != nil {
			return err
		}
		updates := completeActivityUpdatesInternal(finalized.StartedAt, activitytypes.StatusCancelled, cancelledMessageInternal, nil, nil, now)
		result := tx.Model(&Activity{}).
			Where("id = ? AND status IN ?", activityID, []activitytypes.Status{activitytypes.StatusQueued, activitytypes.StatusRunning}).
			Updates(updates)
		if result.Error != nil {
			return errors.WrapIf(result.Error, "failed to cancel activity")
		}
		if result.RowsAffected == 0 {
			return ErrActivityNotCancelable
		}
		if err := tx.First(&finalized, "id = ? AND environment_id = ?", activityID, environmentID).Error; err != nil {
			return errors.WrapIf(err, "failed to load cancelled activity")
		}
		return nil
	}); err != nil {
		return nil, err
	}

	dto := s.publishTerminalSnapshotInternal(writeCtx, &finalized)
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
	var staleChecks []Activity
	if err := s.db.WithContext(ctx).
		Where("type = ? AND status = ? AND started_at < ?", activitytypes.TypeImageUpdateCheck, activitytypes.StatusRunning, cutoff).
		Find(&staleChecks).Error; err != nil {
		return 0, errors.WrapIf(err, "find stale image update checks")
	}

	const message = "Image update check failed because it was stale after Arcane restarted"
	errMessage := message
	var failed int64
	var failErrs []error
	for i := range staleChecks {
		if _, err := s.CompleteActivity(ctx, staleChecks[i].ID, activitytypes.StatusFailed, message, &errMessage, "Image update check failed"); err != nil {
			failErrs = append(failErrs, errors.WrapIff(err, "fail stale image update check %s", staleChecks[i].ID))
			continue
		}
		failed++
	}

	return failed, stderrors.Join(failErrs...)
}

// isTrackedInternal reports whether activityID has a live work registration in
// this process (i.e. a worker goroutine called Track and has not completed yet).
func (s *ActivityService) isTrackedInternal(activityID string) bool {
	s.runningMu.Lock()
	defer s.runningMu.Unlock()
	_, ok := s.running[activityID]
	return ok
}

// FailAbandonedActivities marks queued/running activities whose worker is no
// longer alive in this process as failed, releasing any concurrency slot they
// still hold. Liveness comes from the running map, not age: every creation
// path Tracks its activity right after StartActivity, and the short grace
// period covers that create→Track window. This assumes exactly one Arcane
// process owns the database (managers and agents each own theirs); running
// multiple replicas against one database would make each replica sweep the
// other's live work.
//
// The terminal write is status-guarded like CancelActivity's untracked
// fallback: a worker completing concurrently wins the race and the row is
// skipped here.
func (s *ActivityService) FailAbandonedActivities(ctx context.Context) (int64, error) {
	if s == nil || s.db == nil {
		return 0, nil
	}

	cutoff := time.Now().Add(-abandonedActivityGrace)
	activeStatuses := []activitytypes.Status{activitytypes.StatusQueued, activitytypes.StatusRunning}
	var candidates []Activity
	if err := s.db.WithContext(ctx).
		Where("status IN ? AND started_at < ?", activeStatuses, cutoff).
		Find(&candidates).Error; err != nil {
		return 0, errors.WrapIf(err, "find abandoned activities")
	}

	const message = "Activity was marked failed because its worker is no longer running"
	errMessage := message
	var swept int64
	var sweepErrs []error
	for i := range candidates {
		activityID := candidates[i].ID
		if s.isTrackedInternal(activityID) {
			continue
		}

		now := time.Now()
		var finalized Activity
		lostRace := false
		if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			updates := completeActivityUpdatesInternal(candidates[i].StartedAt, activitytypes.StatusFailed, message, &errMessage, nil, now)
			result := tx.Model(&Activity{}).
				Where("id = ? AND status IN ?", activityID, activeStatuses).
				Updates(updates)
			if result.Error != nil {
				return errors.WrapIf(result.Error, "fail abandoned activity")
			}
			if result.RowsAffected == 0 {
				lostRace = true
				return nil
			}
			if err := tx.First(&finalized, "id = ?", activityID).Error; err != nil {
				return errors.WrapIf(err, "load failed abandoned activity")
			}
			return nil
		}); err != nil {
			sweepErrs = append(sweepErrs, errors.WrapIff(err, "sweep activity %s", activityID))
			continue
		}
		if lostRace {
			continue
		}

		s.releaseSlotInternal(activityID)
		s.publishTerminalSnapshotInternal(ctx, &finalized)
		swept++
	}

	return swept, stderrors.Join(sweepErrs...)
}

// ResolveOrphanedQueuedActivities fails any activity still queued at startup.
// Queued state is owned by a live goroutine blocked on AwaitActivitySlot, so a
// queued row after a restart can never start running.
func (s *ActivityService) ResolveOrphanedQueuedActivities(ctx context.Context) (int64, error) {
	if s == nil || s.db == nil {
		return 0, nil
	}

	var queued []Activity
	if err := s.db.WithContext(ctx).
		Where("status = ?", activitytypes.StatusQueued).
		Find(&queued).Error; err != nil {
		return 0, errors.WrapIf(err, "find orphaned queued activities")
	}

	const message = "Queued activity was interrupted by an Arcane restart"
	errMessage := message
	var failed int64
	var failErrs []error
	for i := range queued {
		if _, err := s.CompleteActivity(ctx, queued[i].ID, activitytypes.StatusFailed, message, &errMessage); err != nil {
			failErrs = append(failErrs, errors.WrapIff(err, "fail orphaned queued activity %s", queued[i].ID))
			continue
		}
		failed++
	}

	return failed, stderrors.Join(failErrs...)
}

// PatchActivityMetadata merges patch into the activity's existing metadata,
// unlike UpdateActivity which replaces the metadata wholesale.
func (s *ActivityService) PatchActivityMetadata(ctx context.Context, activityID string, patch database.JSON) error {
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
		var activity Activity
		if err := tx.First(&activity, "id = ?", activityID).Error; err != nil {
			return errors.WrapIf(err, "failed to load activity")
		}
		merged := cloneJSONInternal(activity.Metadata)
		if merged == nil {
			merged = database.JSON{}
		}
		maps.Copy(merged, patch)
		if err := tx.Model(&Activity{}).Where("id = ?", activityID).
			Updates(map[string]any{"metadata": merged, "updated_at": time.Now()}).Error; err != nil {
			return errors.WrapIf(err, "failed to patch activity metadata")
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

	var stale []Activity
	if err := s.db.WithContext(ctx).
		Where("type = ? AND status = ?", activitytypes.TypeAutoUpdate, activitytypes.StatusRunning).
		Find(&stale).Error; err != nil {
		return 0, errors.WrapIf(err, "find stale auto-update activities")
	}

	var resolved int64
	var resolveErrs []error
	for i := range stale {
		status := activitytypes.StatusFailed
		message := "Auto-update interrupted by Arcane restart"
		var errMessage *string
		if selfUpdate, _ := stale[i].Metadata["selfUpdateTriggered"].(bool); selfUpdate {
			status = activitytypes.StatusSuccess
			message = "Auto-update completed — Arcane restarted with the updated image"
		} else {
			errMessage = new(message)
		}
		if _, err := s.CompleteActivity(ctx, stale[i].ID, status, message, errMessage); err != nil {
			resolveErrs = append(resolveErrs, errors.WrapIff(err, "resolve stale auto-update activity %s", stale[i].ID))
			continue
		}
		resolved++
	}

	return resolved, stderrors.Join(resolveErrs...)
}

func completeActivityUpdatesInternal(startedAt time.Time, status activitytypes.Status, finalMessage string, errMessage *string, finalStep []string, now time.Time) map[string]any {
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
	if status == activitytypes.StatusSuccess {
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

	var activities []Activity
	q := s.db.WithContext(ctx).Model(&Activity{}).Where("environment_id = ?", environmentID)

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
		return nil, pagination.Response{}, errors.WrapIf(err, "failed to paginate activities")
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

	var model Activity
	if err := s.db.WithContext(ctx).
		Where("id = ? AND environment_id = ?", activityID, environmentID).
		First(&model).Error; err != nil {
		return nil, errors.WrapIf(err, "failed to load activity")
	}

	var messages []ActivityMessage
	if err := s.db.WithContext(ctx).
		Where("activity_id = ?", activityID).
		Order("created_at DESC").
		Limit(limit).
		Find(&messages).Error; err != nil {
		return nil, errors.WrapIf(err, "failed to load activity messages")
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
				return errors.WrapIf(err, "failed to find activities older than retention window")
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
			return errors.WrapIf(err, "failed to find activity history")
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
	if !s.admitActivityPublishInternal(activity) {
		return
	}
	s.publishInternal(activity.EnvironmentID, activitytypes.StreamEvent{
		Type:       "activity",
		ActivityID: activity.ID,
		Activity:   &activity,
		Timestamp:  time.Now(),
	})
}

func (s *ActivityService) publishLockInternal(activityID string) *sync.Mutex {
	h := fnv.New32a()
	_, _ = h.Write([]byte(activityID))
	return &s.publishLocks[h.Sum32()%uint32(len(s.publishLocks))]
}

func isTerminalActivityStatusInternal(status activitytypes.Status) bool {
	return status == activitytypes.StatusSuccess ||
		status == activitytypes.StatusFailed ||
		status == activitytypes.StatusCancelled
}

// admitActivityPublishInternal orders activity events across the commit →
// publish gap. Writes publish their snapshot after committing, so a goroutine
// that committed before a terminal write can publish after it; since a
// terminal activity emits no further events to correct the stream (and
// subscriber coalescing would even replace an undelivered terminal event in
// place), such a stale non-terminal snapshot must be dropped, not delivered.
// Activities never leave a terminal status, so once a terminal snapshot is
// published, any later non-terminal snapshot for that ID is stale by
// construction.
func (s *ActivityService) admitActivityPublishInternal(activity activitytypes.Activity) bool {
	if s == nil {
		return true
	}
	s.terminalPublishedMu.Lock()
	defer s.terminalPublishedMu.Unlock()
	if !isTerminalActivityStatusInternal(activity.Status) {
		_, sealed := s.terminalPublished[activity.ID]
		return !sealed
	}
	now := time.Now()
	for id, publishedAt := range s.terminalPublished {
		if now.Sub(publishedAt) > terminalPublishRetention {
			delete(s.terminalPublished, id)
		}
	}
	if s.terminalPublished == nil {
		s.terminalPublished = map[string]time.Time{}
	}
	s.terminalPublished[activity.ID] = now
	return true
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

func activityToDTOInternal(model *Activity) activitytypes.Activity {
	if model == nil {
		return activitytypes.Activity{}
	}
	return activitytypes.Activity{
		ID:                  model.ID,
		EnvironmentID:       model.EnvironmentID,
		SourceEnvironmentID: model.EnvironmentID,
		BatchID:             copyPtrInternal(model.BatchID),
		Type:                model.Type,
		Status:              model.Status,
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

func activityMessageToDTOInternal(model *ActivityMessage) activitytypes.Message {
	if model == nil {
		return activitytypes.Message{}
	}
	return activitytypes.Message{
		ID:         model.ID,
		ActivityID: model.ActivityID,
		Level:      model.Level,
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

func cloneJSONInternal(input database.JSON) database.JSON {
	if len(input) == 0 {
		return nil
	}
	out := make(database.JSON, len(input))
	maps.Copy(out, input)
	return out
}

func jsonToMapInternal(input database.JSON) map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]any, len(input))
	maps.Copy(out, input)
	return out
}

func terminalActivityStatusesInternal() []activitytypes.Status {
	return []activitytypes.Status{
		activitytypes.StatusSuccess,
		activitytypes.StatusFailed,
		activitytypes.StatusCancelled,
	}
}

func findTerminalActivityIDsInternal(q *gorm.DB) ([]string, error) {
	var activityIDs []string
	if err := q.Model(&Activity{}).
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
		return nil, errors.WrapIf(err, "failed to find excess activities")
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

		if err := tx.Where("activity_id IN ?", batch).Delete(&ActivityMessage{}).Error; err != nil {
			return totalDeleted, errors.WrapIf(err, "failed to delete activity messages")
		}
		result := tx.Where("id IN ?", batch).Delete(&Activity{})
		if result.Error != nil {
			return totalDeleted, errors.WrapIf(result.Error, "failed to delete activities")
		}
		totalDeleted += result.RowsAffected
	}

	return totalDeleted, nil
}

func activityStartedByDTOInternal(model *Activity) *activitytypes.StartedBy {
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
