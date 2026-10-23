package activity

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"maps"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/samber/mo"
	"go.getarcane.app/kit/pkg"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/getarcaneapp/arcane/backend/v3/internal/activity/children/pubsub"
	"github.com/getarcaneapp/arcane/backend/v3/internal/activity/children/slots"
	"github.com/getarcaneapp/arcane/backend/v3/internal/activity/children/sweeps"
	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	activitylib "github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/timeouts"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/concurrency"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/dbutil"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/httpx"
)

const (
	defaultActivityRetentionDays = 30
	defaultActivityHistoryLimit  = 1000
	defaultActivityMessages      = 500

	cancelledMessage = "Cancelled by user"

	deleteActivitiesBatchSize = 500
)

var ErrActivityNotCancelable = errors.New("activity is not cancelable")

type ActivityService struct {
	Changes *concurrency.Signal[string]
	db      *database.DB
	pubsub  *pubsub.Service
	slots   *slots.Service
	sweeps  *sweeps.Service

	// running maps an active activity ID to the cancel function of its work
	// context, so cancellation requests can interrupt in-flight work. Entries
	// are added by Track and removed when the activity is completed.
	runningMu sync.Mutex
	running   map[string]context.CancelCauseFunc

	// publishLocks serializes the commit→publish window of the non-terminal
	// snapshot writers (AppendMessages, UpdateActivity) per activity, so their
	// events reach subscribers in commit order — without it, a batch that
	// commits first but publishes second would revert progress/step/status
	// fields a concurrent update already streamed. Terminal snapshots are
	// ordered separately by the pubsub terminal latch. Striped by activity
	// ID hash so no per-activity lifecycle management is needed.
	publishLocks [64]sync.Mutex
}

type (
	StartActivityRequest         = activitylib.StartRequest
	UpdateActivityRequest        = activitylib.UpdateRequest
	AppendActivityMessageRequest = activitylib.AppendMessageRequest
)

func NewActivityService(db *database.DB, settingsService *settings.SettingsService) *ActivityService {
	changes := concurrency.NewSignal[string]()
	return &ActivityService{
		Changes: changes,
		db:      db,
		pubsub:  pubsub.NewService(changes),
		slots:   slots.NewService(settingsService),
		sweeps:  sweeps.NewService(db),
		running: map[string]context.CancelCauseFunc{},
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

func (s *ActivityService) checkInit() error {
	if s == nil || s.db == nil {
		return errors.New("activity service not initialized")
	}
	return nil
}

func (s *ActivityService) StartActivity(ctx context.Context, req StartActivityRequest) (*types.Activity, error) {
	if err := s.checkInit(); err != nil {
		return nil, err
	}

	if req.ID != "" {
		var existing Activity
		err := s.db.WithContext(ctx).Where("id = ?", req.ID).Limit(1).Find(&existing).Error
		if err != nil {
			return nil, fmt.Errorf("failed to load activity: %w", err)
		}
		if existing.ID != "" {
			dto := activityToDTO(&existing)
			return &dto, nil
		}
	}

	now := time.Now()
	environmentID := cmp.Or(strings.TrimSpace(req.EnvironmentID), "0")

	// Queue-opted activities take a concurrency slot up front when one is
	// free; otherwise they are created as queued and AwaitActivitySlot blocks
	// until a slot opens.
	status := types.ActivityStatusRunning
	var slotRelease func()
	if req.Queue {
		if req.DeferSlot {
			status = types.ActivityStatusQueued
		} else if release, ok := s.slots.TryAcquire(ctx, environmentID).Get(); ok {
			slotRelease = release
		} else {
			status = types.ActivityStatusQueued
		}
	}

	startedBy := kit.FromPtr(req.StartedBy)
	model := &Activity{
		ID:                   req.ID,
		EnvironmentID:        environmentID,
		BatchID:              copyPtr(kit.Ternary(req.BatchID != nil, req.BatchID, mo.EmptyableToOption(utils.ActivityBatchIDFromContext(ctx)).ToPointer())),
		Type:                 cmp.Or(req.Type, types.ActivityTypeAutoUpdate),
		Status:               status,
		ResourceType:         copyPtr(req.ResourceType),
		ResourceID:           copyPtr(req.ResourceID),
		ResourceName:         copyPtr(req.ResourceName),
		StartedByUserID:      mo.EmptyableToOption(strings.TrimSpace(startedBy.ID)).ToPointer(),
		StartedByUsername:    mo.EmptyableToOption(strings.TrimSpace(startedBy.Username)).ToPointer(),
		StartedByDisplayName: mo.EmptyableToOption(strings.TrimSpace(kit.FromPtr(startedBy.DisplayName))).ToPointer(),
		Progress:             clampProgressPtr(req.Progress),
		Step:                 strings.TrimSpace(req.Step),
		LatestMessage:        strings.TrimSpace(req.LatestMessage),
		StartedAt:            now,
		Metadata:             cloneJSON(req.Metadata),
		CreatedAt:            now,
	}

	create := s.db.WithContext(ctx)
	if req.ID != "" {
		create = create.Clauses(clause.OnConflict{DoNothing: true})
	}
	created := create.Create(model)
	if created.Error != nil || created.RowsAffected == 0 {
		if slotRelease != nil {
			slotRelease()
		}
		if created.Error != nil {
			return nil, fmt.Errorf("failed to create activity: %w", created.Error)
		}
		// A concurrent start with the same ID created it first.
		var existing Activity
		if err := s.db.WithContext(ctx).First(&existing, "id = ?", req.ID).Error; err != nil {
			return nil, fmt.Errorf("failed to load activity: %w", err)
		}
		dto := activityToDTO(&existing)
		return &dto, nil
	}
	if slotRelease != nil {
		s.slots.Register(model.ID, slotRelease)
	}

	dto := activityToDTO(model)
	s.pubsub.PublishActivity(dto)
	return &dto, nil
}

// StartTrackedActivity holds the cancellation lock across persistence and
// registration, so cancellation cannot treat the new row as untracked.
func (s *ActivityService) StartTrackedActivity(ctx context.Context, req StartActivityRequest) (*types.Activity, context.Context, error) {
	if err := s.checkInit(); err != nil {
		return nil, nil, err
	}
	s.runningMu.Lock()
	defer s.runningMu.Unlock()
	started, err := s.StartActivity(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	workCtx, cancel := context.WithCancelCause(ctx)
	if s.running == nil {
		s.running = map[string]context.CancelCauseFunc{}
	}
	s.running[started.ID] = cancel
	return started, workCtx, nil
}

// AwaitActivitySlot blocks until the queued activity holds a concurrency slot,
// then flips its status to running. It returns immediately when the activity
// already took a slot at creation. On cancellation the context cause is
// returned and the activity stays queued for its caller to finalize.
// Implements activitylib.SlotWaiter.
func (s *ActivityService) AwaitActivitySlot(ctx context.Context, activityID, environmentID string) error {
	if err := s.checkInit(); err != nil {
		return err
	}
	activityID = strings.TrimSpace(activityID)
	if activityID == "" {
		return errors.New("activity id is required")
	}
	environmentID = cmp.Or(strings.TrimSpace(environmentID), "0")

	acquired, err := s.slots.Await(ctx, activityID, environmentID)
	if err != nil || !acquired {
		return err
	}

	if _, updateErr := s.UpdateActivity(ctx, activityID, UpdateActivityRequest{Status: types.ActivityStatusRunning}); updateErr != nil {
		slog.WarnContext(ctx, "failed to mark queued activity running", "activityId", activityID, "error", updateErr)
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

func (s *ActivityService) UpdateActivity(ctx context.Context, activityID string, req UpdateActivityRequest) (*types.Activity, error) {
	if err := s.checkInit(); err != nil {
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
	if req.ResourceName != nil {
		updates["resource_name"] = strings.TrimSpace(*req.ResourceName)
	}
	if req.Progress != nil {
		updates["progress"] = *clampProgressPtr(req.Progress)
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
		updates["metadata"] = cloneJSON(req.Metadata)
	}

	// Hold the per-activity publish lock across commit and publish so this
	// snapshot cannot be overtaken on the stream by a concurrent append batch
	// that committed earlier but would publish later.
	lock := s.publishLock(activityID)
	lock.Lock()
	defer lock.Unlock()

	var model Activity
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&Activity{}).Where("id = ?", activityID).Updates(updates)
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

	dto := activityToDTO(&model)
	s.pubsub.PublishActivity(dto)
	return &dto, nil
}

func (s *ActivityService) AppendMessage(ctx context.Context, activityID string, req AppendActivityMessageRequest) (*types.ActivityMessage, error) {
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
// handled by the pubsub terminal latch dropping the stale event.
func (s *ActivityService) AppendMessages(ctx context.Context, activityID string, reqs []AppendActivityMessageRequest) ([]types.ActivityMessage, error) {
	if err := s.checkInit(); err != nil {
		return nil, err
	}
	activityID = strings.TrimSpace(activityID)
	if activityID == "" {
		return nil, errors.New("activity id is required")
	}

	messages, updates := buildAppendBatch(activityID, reqs)
	if len(messages) == 0 {
		return nil, nil
	}

	// Hold the per-activity publish lock across commit and publish so this
	// batch's snapshot cannot be overtaken on the stream by a concurrent
	// UpdateActivity that committed later — subscribers must see the two
	// snapshots in commit order.
	lock := s.publishLock(activityID)
	lock.Lock()
	defer lock.Unlock()

	// The Writer drain cannot replay a lost batch, so retry like CompleteActivity.
	var current Activity
	if err := s.retryWrite(ctx, activityID, "message batch", func(tx *gorm.DB) error {
		if err := tx.First(&current, "id = ?", activityID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("activity not found")
			}
			return fmt.Errorf("failed to load activity: %w", err)
		}
		if err := tx.Create(&messages).Error; err != nil {
			return fmt.Errorf("failed to append activity message: %w", err)
		}
		result := tx.Model(&Activity{}).Where("id = ?", activityID).Updates(updates)
		if result.Error != nil {
			return fmt.Errorf("failed to update activity latest message: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return errors.New("activity not found")
		}
		if err := tx.First(&current, "id = ?", activityID).Error; err != nil {
			return fmt.Errorf("failed to load updated activity: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	out := make([]types.ActivityMessage, 0, len(messages))
	for _, message := range messages {
		dto := types.ActivityMessage{
			ID:         message.ID,
			ActivityID: message.ActivityID,
			Level:      message.Level,
			Message:    message.Message,
			Payload:    cloneJSON(message.Payload),
			CreatedAt:  message.CreatedAt,
		}
		s.pubsub.PublishMessage(current.EnvironmentID, current.Type, dto)
		out = append(out, dto)
	}
	s.pubsub.PublishActivity(activityToDTO(&current))
	return out, nil
}

// buildAppendBatch turns the request batch into insertable message rows
// plus the coalesced Activity column updates (latest message/progress/step from
// the last request carrying each).
func buildAppendBatch(activityID string, reqs []AppendActivityMessageRequest) ([]*ActivityMessage, map[string]any) {
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

		level := cmp.Or(req.Level, types.ActivityMessageLevelInfo)

		messages = append(messages, &ActivityMessage{
			ActivityID: activityID,
			Level:      level,
			Message:    messageText,
			Payload:    cloneJSON(req.Payload),
			// Spread inside the shared timestamp so the created_at sort
			// used for retrieval keeps the original line order.
			CreatedAt: now.Add(time.Duration(len(messages)) * time.Microsecond),
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
		updates["progress"] = *clampProgressPtr(latestProgress)
	}
	if latestStep != "" {
		updates["step"] = latestStep
	}
	return messages, updates
}

// retryWrite runs a write transaction, retrying transient SQLite contention
// (SQLITE_BUSY under bulk activity writes) up to three times.
func (s *ActivityService) retryWrite(ctx context.Context, activityID, what string, write func(tx *gorm.DB) error) error {
	const attempts = 3
	for attempt := 1; ; attempt++ {
		err := s.db.WithContext(ctx).Transaction(write)
		if err == nil || attempt == attempts || !dbutil.IsRetryableWriteError(err) {
			return err
		}
		slog.WarnContext(ctx, "retrying activity write", "write", what, "activityId", activityID, "attempt", attempt, "error", err)
		select {
		case <-ctx.Done():
			return errors.Join(err, context.Cause(ctx))
		case <-time.After(250 * time.Millisecond * time.Duration(attempt)):
		}
	}
}

func (s *ActivityService) CompleteActivity(ctx context.Context, activityID string, status types.ActivityStatus, finalMessage string, errMessage *string, finalStep ...string) (*types.Activity, error) {
	if err := s.checkInit(); err != nil {
		return nil, err
	}
	if !slices.Contains(terminalActivityStatuses, status) {
		status = types.ActivityStatusSuccess
	}

	activityID = strings.TrimSpace(activityID)
	if activityID == "" {
		return nil, errors.New("activity id is required")
	}

	// The activity is reaching a terminal state; release any cancel
	// registration (a nil cause keeps an earlier ErrCanceled) and free its slot.
	s.runningMu.Lock()
	cancel, tracked := s.running[activityID]
	delete(s.running, activityID)
	s.runningMu.Unlock()
	if tracked {
		cancel(nil)
	}
	s.slots.Release(activityID)

	// Detach from cancellation so the terminal write always lands — completion is
	// often triggered precisely because the work context was cancelled.
	ctx = context.WithoutCancel(ctx)

	now := time.Now()
	var model Activity
	// A lost terminal write leaves the activity stuck in running forever.
	if err := s.retryWrite(ctx, activityID, "terminal status", func(tx *gorm.DB) error {
		if err := tx.First(&model, "id = ?", activityID).Error; err != nil {
			return fmt.Errorf("failed to load activity: %w", err)
		}

		updates := completeActivityUpdates(model.StartedAt, status, finalMessage, errMessage, finalStep, now)
		if err := tx.Model(&Activity{}).Where("id = ?", activityID).Updates(updates).Error; err != nil {
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
		level := types.ActivityMessageLevelSuccess
		switch status {
		case types.ActivityStatusFailed:
			level = types.ActivityMessageLevelError
		case types.ActivityStatusCancelled:
			level = types.ActivityMessageLevelWarning
		case types.ActivityStatusQueued, types.ActivityStatusRunning, types.ActivityStatusSuccess:
		}
		activityCtx := utils.ActivityRuntimeContext(ctx, nil)
		if _, err := s.AppendMessage(activityCtx, activityID, AppendActivityMessageRequest{
			Level:   level,
			Message: finalMessage,
		}); err != nil {
			slog.DebugContext(ctx, "failed to append final activity message", "activityId", activityID, "error", err)
		}
	}

	dto := s.publishTerminalSnapshot(ctx, &model)
	return &dto, nil
}

// publishTerminalSnapshot publishes an activity's terminal event from
// a snapshot re-read under the per-activity publish lock. Terminal writers
// cannot hold that lock across their own transactions (CompleteActivity
// appends its final message through the locked AppendMessages path), so a
// snapshot captured in-transaction can be overtaken by an append or update
// that commits later but publishes first; re-reading under the lock
// guarantees the terminal event carries every field already streamed. The
// passed model is published as-is if the re-read fails, and is updated in
// place otherwise so callers return the published state.
func (s *ActivityService) publishTerminalSnapshot(ctx context.Context, model *Activity) types.Activity {
	lock := s.publishLock(model.ID)
	lock.Lock()
	defer lock.Unlock()

	var fresh Activity
	if err := s.db.WithContext(ctx).First(&fresh, "id = ?", model.ID).Error; err != nil {
		slog.DebugContext(ctx, "failed to reload activity for terminal publish", "activityId", model.ID, "error", err)
	} else {
		*model = fresh
	}
	dto := activityToDTO(model)
	s.pubsub.PublishActivity(dto)
	return dto
}

// CancelActivity requests cancellation of a running or queued activity. When the
// activity's work is running in this process it interrupts it (the work finalizes
// its own terminal status); otherwise it marks the activity cancelled directly,
// but only if it is still active. Returns ErrActivityNotCancelable if the activity
// has already reached a terminal state, or gorm.ErrRecordNotFound if it is unknown.
func (s *ActivityService) CancelActivity(ctx context.Context, environmentID, activityID, requestedBy string) (*types.Activity, error) {
	if err := s.checkInit(); err != nil {
		return nil, err
	}
	activityID = strings.TrimSpace(activityID)
	if activityID == "" {
		return nil, errors.New("activity id is required")
	}
	environmentID = cmp.Or(strings.TrimSpace(environmentID), "0")

	var model Activity
	if err := s.db.WithContext(ctx).Where("id = ? AND environment_id = ?", activityID, environmentID).First(&model).Error; err != nil {
		return nil, err
	}
	if model.Type == types.ActivityTypeJobRun {
		return nil, ErrActivityNotCancelable
	}
	switch model.Status {
	case types.ActivityStatusSuccess, types.ActivityStatusFailed, types.ActivityStatusCancelled:
		return nil, ErrActivityNotCancelable
	case types.ActivityStatusQueued, types.ActivityStatusRunning:
		// Active states — cancellation can proceed.
	}

	requestedBy = cmp.Or(strings.TrimSpace(requestedBy), "a user")
	writeCtx := utils.ActivityRuntimeContext(ctx, nil)
	if _, err := s.AppendMessage(writeCtx, activityID, AppendActivityMessageRequest{
		Level:   types.ActivityMessageLevelWarning,
		Message: "Cancellation requested by " + requestedBy,
	}); err != nil {
		slog.DebugContext(ctx, "failed to append cancellation message", "activityId", activityID, "error", err)
	}

	if s.RequestCancel(activityID) {
		// The running work observes the cancelled context and writes its own
		// terminal status, which reaches clients via the activity stream. Return
		// the pre-cancel snapshot rather than reloading here: the worker has not
		// finished unwinding yet, so a reload would still report "running".
		return new(activityToDTO(&model)), nil
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
		updates := completeActivityUpdates(finalized.StartedAt, types.ActivityStatusCancelled, cancelledMessage, nil, nil, now)
		result := tx.Model(&Activity{}).
			Where("id = ? AND status IN ?", activityID, []types.ActivityStatus{types.ActivityStatusQueued, types.ActivityStatusRunning}).
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

	dto := s.publishTerminalSnapshot(writeCtx, &finalized)
	return &dto, nil
}

// FailStaleImageUpdateChecks marks image update checks that were left running
// across a prior process lifetime as failed. It intentionally scopes cleanup to
// old image-update-check activities so startup repair cannot affect other work.
func (s *ActivityService) FailStaleImageUpdateChecks(ctx context.Context) (int64, error) {
	return s.finalizeSwept(ctx, "stale image update check",
		func() ([]sweeps.Candidate, error) { return s.sweeps.StaleImageUpdateChecks(ctx) },
		func(sweeps.Candidate) (types.ActivityStatus, string) {
			return types.ActivityStatusFailed, "Image update check failed because it was stale after Arcane restarted"
		},
		"Image update check failed",
	)
}

// finalizeSwept completes each swept candidate with the status and message
// resolve picks; failed ones also record the message as their error.
func (s *ActivityService) finalizeSwept(
	ctx context.Context,
	label string,
	find func() ([]sweeps.Candidate, error),
	resolve func(sweeps.Candidate) (types.ActivityStatus, string),
	finalStep ...string,
) (int64, error) {
	candidates, err := find()
	if err != nil {
		return 0, fmt.Errorf("find %s activities: %w", label, err)
	}
	var finalized int64
	var errs []error
	for _, candidate := range candidates {
		status, message := resolve(candidate)
		errMessage := kit.Ternary(status == types.ActivityStatusFailed, new(message), nil)
		if _, completeErr := s.CompleteActivity(ctx, candidate.ID, status, message, errMessage, finalStep...); completeErr != nil {
			errs = append(errs, fmt.Errorf("finalize %s activity %s: %w", label, candidate.ID, completeErr))
			continue
		}
		finalized++
	}
	return finalized, errors.Join(errs...)
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
	activeStatuses := []types.ActivityStatus{types.ActivityStatusQueued, types.ActivityStatusRunning}
	s.runningMu.Lock()
	trackedIDs := slices.Collect(maps.Keys(s.running))
	s.runningMu.Unlock()
	candidates, findErr := s.sweeps.Abandoned(ctx, trackedIDs)
	if findErr != nil {
		return 0, fmt.Errorf("find abandoned activities: %w", findErr)
	}
	// A worker may have Tracked its activity since the snapshot above.
	s.runningMu.Lock()
	candidates = slices.DeleteFunc(candidates, func(candidate sweeps.Candidate) bool {
		_, tracked := s.running[candidate.ID]
		return tracked
	})
	s.runningMu.Unlock()
	if len(candidates) == 0 {
		return 0, nil
	}

	const message = "Activity was marked failed because its worker is no longer running"
	now := time.Now()
	ids := make([]string, 0, len(candidates))
	var durationExpr strings.Builder
	durationArgs := make([]any, 0, 2*len(candidates))
	durationExpr.WriteString("CASE id")
	for _, candidate := range candidates {
		ids = append(ids, candidate.ID)
		durationExpr.WriteString(" WHEN ? THEN ?")
		durationArgs = append(durationArgs, candidate.ID, now.Sub(candidate.StartedAt).Milliseconds())
	}
	durationExpr.WriteString(" ELSE duration_ms END")
	updates := completeActivityUpdates(now, types.ActivityStatusFailed, message, new(message), nil, now)
	updates["duration_ms"] = gorm.Expr(durationExpr.String(), durationArgs...)

	// Status-guarded so a worker completing concurrently wins; only rows this
	// sweep failed carry its message, so the reload skips the race losers.
	var finalized []Activity
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&Activity{}).
			Where("id IN ? AND status IN ?", ids, activeStatuses).
			Updates(updates).Error; err != nil {
			return fmt.Errorf("fail abandoned activities: %w", err)
		}
		if err := tx.Where("id IN ? AND status = ? AND error = ?", ids, types.ActivityStatusFailed, message).Find(&finalized).Error; err != nil {
			return fmt.Errorf("load failed abandoned activities: %w", err)
		}
		return nil
	}); err != nil {
		return 0, fmt.Errorf("sweep abandoned activities: %w", err)
	}

	for i := range finalized {
		s.slots.Release(finalized[i].ID)
		s.publishTerminalSnapshot(ctx, &finalized[i])
	}

	return int64(len(finalized)), nil
}

// ResolveOrphanedQueuedActivities fails any activity still queued at startup.
// Queued state is owned by a live goroutine blocked on AwaitActivitySlot, so a
// queued row after a restart can never start running.
func (s *ActivityService) ResolveOrphanedQueuedActivities(ctx context.Context, protectedIDs ...string) (int64, error) {
	return s.finalizeSwept(ctx, "orphaned queued",
		func() ([]sweeps.Candidate, error) { return s.sweeps.OrphanedQueued(ctx, protectedIDs) },
		func(sweeps.Candidate) (types.ActivityStatus, string) {
			return types.ActivityStatusFailed, "Queued activity was interrupted by an Arcane restart"
		},
	)
}

// PatchActivityMetadata merges patch into the activity's existing metadata,
// unlike UpdateActivity which replaces the metadata wholesale.
func (s *ActivityService) PatchActivityMetadata(ctx context.Context, activityID string, patch database.JSON) error {
	if err := s.checkInit(); err != nil {
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
		var record Activity
		if err := tx.First(&record, "id = ?", activityID).Error; err != nil {
			return fmt.Errorf("failed to load activity: %w", err)
		}
		merged := cloneJSON(record.Metadata)
		if merged == nil {
			merged = database.JSON{}
		}
		maps.Copy(merged, patch)
		if err := tx.Model(&Activity{}).Where("id = ?", activityID).
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
func (s *ActivityService) ResolveStaleAutoUpdateActivities(ctx context.Context, protectedIDs ...string) (int64, error) {
	return s.finalizeSwept(ctx, "stale auto-update",
		func() ([]sweeps.Candidate, error) { return s.sweeps.StaleAutoUpdates(ctx, protectedIDs) },
		func(candidate sweeps.Candidate) (types.ActivityStatus, string) {
			if selfUpdate, _ := candidate.Metadata["selfUpdateTriggered"].(bool); selfUpdate {
				return types.ActivityStatusSuccess, "Auto-update completed — Arcane restarted with the updated image"
			}
			return types.ActivityStatusFailed, "Auto-update interrupted by Arcane restart"
		},
	)
}

func completeActivityUpdates(startedAt time.Time, status types.ActivityStatus, finalMessage string, errMessage *string, finalStep []string, now time.Time) map[string]any {
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
	if status == types.ActivityStatusSuccess {
		updates["progress"] = 100
	}
	return updates
}

func (s *ActivityService) ListActivitiesPaginated(ctx context.Context, environmentID string, params pagination.QueryParams) ([]types.Activity, pagination.Response, error) {
	if err := s.checkInit(); err != nil {
		return nil, pagination.Response{}, err
	}

	environmentID = cmp.Or(strings.TrimSpace(environmentID), "0")

	var activities []Activity
	q := s.db.WithContext(ctx).Model(&Activity{}).Where("environment_id = ?", environmentID)
	q = scopeJobActivityVisibility(ctx, q, environmentID, authz.PermActivitiesRead)

	if term := strings.TrimSpace(params.Search); term != "" {
		escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(term)
		searchPattern := "%" + escaped + "%"
		// Matches the Activity Center's own search fields.
		q = q.Where(
			"type LIKE ? ESCAPE '\\' OR COALESCE(resource_name, '') LIKE ? ESCAPE '\\' OR "+
				"COALESCE(latest_message, '') LIKE ? ESCAPE '\\' OR COALESCE(error, '') LIKE ? ESCAPE '\\'",
			searchPattern, searchPattern, searchPattern, searchPattern,
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

	out := make([]types.Activity, 0, len(activities))
	for i := range activities {
		out = append(out, activityToDTO(&activities[i]))
	}
	return out, paginationResp, nil
}

func (s *ActivityService) GetActivityDetail(ctx context.Context, environmentID, activityID string, limit int) (*types.ActivityDetail, error) {
	if err := s.checkInit(); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > defaultActivityMessages {
		limit = defaultActivityMessages
	}

	var model Activity
	if err := s.db.WithContext(ctx).
		Where("id = ? AND environment_id = ?", activityID, environmentID).
		First(&model).Error; err != nil {
		return nil, fmt.Errorf("failed to load activity: %w", err)
	}

	var messages []ActivityMessage
	if err := s.db.WithContext(ctx).
		Where("activity_id = ?", activityID).
		Order("created_at DESC").
		Limit(limit).
		Find(&messages).Error; err != nil {
		return nil, fmt.Errorf("failed to load activity messages: %w", err)
	}

	outMessages := make([]types.ActivityMessage, 0, len(messages))
	for _, message := range slices.Backward(messages) {
		outMessages = append(outMessages, types.ActivityMessage{
			ID:         message.ID,
			ActivityID: message.ActivityID,
			Level:      message.Level,
			Message:    message.Message,
			Payload:    cloneJSON(message.Payload),
			CreatedAt:  message.CreatedAt,
		})
	}

	return &types.ActivityDetail{
		Activity: activityToDTO(&model),
		Messages: outMessages,
	}, nil
}

func (s *ActivityService) PruneHistory(ctx context.Context, retentionDays, maxEntries int) (int64, error) {
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
			var ids []string
			if err := tx.Model(&Activity{}).
				Where("status IN ? AND COALESCE(ended_at, updated_at, created_at) < ?", terminalActivityStatuses, cutoff).
				Pluck("id", &ids).Error; err != nil {
				return fmt.Errorf("failed to find activities older than retention window: %w", err)
			}
			count, err := deleteActivitiesByID(tx, ids)
			if err != nil {
				return err
			}
			deleted += count
		}

		if maxEntries > 0 {
			var ids []string
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
			`, terminalActivityStatuses, maxEntries).Scan(&ids).Error; err != nil {
				return fmt.Errorf("failed to find excess activities: %w", err)
			}
			count, err := deleteActivitiesByID(tx, ids)
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
	environmentID = cmp.Or(strings.TrimSpace(environmentID), "0")

	var deleted int64
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var ids []string
		query := tx.Model(&Activity{}).Where("environment_id = ? AND status IN ?", environmentID, terminalActivityStatuses)
		if err := scopeJobActivityVisibility(ctx, query, environmentID, authz.PermActivitiesDelete).Pluck("id", &ids).Error; err != nil {
			return fmt.Errorf("failed to find activity history: %w", err)
		}
		count, err := deleteActivitiesByID(tx, ids)
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

func (s *ActivityService) publishLock(activityID string) *sync.Mutex {
	h := fnv.New32a()
	_, _ = h.Write([]byte(activityID))
	return &s.publishLocks[h.Sum32()%uint32(len(s.publishLocks))]
}

func activityToDTO(model *Activity) types.Activity {
	if model == nil {
		return types.Activity{}
	}
	return types.Activity{
		ID:                  model.ID,
		EnvironmentID:       model.EnvironmentID,
		SourceEnvironmentID: model.EnvironmentID,
		BatchID:             copyPtr(model.BatchID),
		Type:                model.Type,
		Status:              model.Status,
		ResourceType:        copyPtr(model.ResourceType),
		ResourceID:          copyPtr(model.ResourceID),
		ResourceName:        copyPtr(model.ResourceName),
		Progress:            clampProgressPtr(model.Progress),
		Step:                model.Step,
		LatestMessage:       model.LatestMessage,
		StartedBy: func() *types.ActivityStartedBy {
			username := strings.TrimSpace(kit.FromPtr(model.StartedByUsername))
			if username == "" {
				return &types.ActivityStartedBy{Username: "System"}
			}
			return &types.ActivityStartedBy{
				Username:    username,
				UserID:      strings.TrimSpace(kit.FromPtr(model.StartedByUserID)),
				DisplayName: strings.TrimSpace(kit.FromPtr(model.StartedByDisplayName)),
			}
		}(),
		StartedAt:  model.StartedAt,
		EndedAt:    copyPtr(model.EndedAt),
		DurationMs: copyPtr(model.DurationMs),
		Error:      copyPtr(model.Error),
		Metadata:   cloneJSON(model.Metadata),
		CreatedAt:  model.CreatedAt,
		UpdatedAt:  copyPtr(model.UpdatedAt),
	}
}

func copyPtr[T any](value *T) *T {
	if value == nil {
		return nil
	}
	return new(*value)
}

func clampProgressPtr(value *int) *int {
	if value == nil {
		return nil
	}
	return new(min(max(*value, 0), 100))
}

func cloneJSON(input database.JSON) database.JSON {
	if len(input) == 0 {
		return nil
	}
	out := make(database.JSON, len(input))
	maps.Copy(out, input)
	return out
}

var terminalActivityStatuses = []types.ActivityStatus{
	types.ActivityStatusSuccess,
	types.ActivityStatusFailed,
	types.ActivityStatusCancelled,
}

func deleteActivitiesByID(tx *gorm.DB, activityIDs []string) (int64, error) {
	if len(activityIDs) == 0 {
		return 0, nil
	}

	var totalDeleted int64
	for i := 0; i < len(activityIDs); i += deleteActivitiesBatchSize {
		end := min(i+deleteActivitiesBatchSize, len(activityIDs))
		batch := activityIDs[i:end]

		if err := tx.Where("activity_id IN ?", batch).Delete(&ActivityMessage{}).Error; err != nil {
			return totalDeleted, fmt.Errorf("failed to delete activity messages: %w", err)
		}
		result := tx.Where("id IN ?", batch).Delete(&Activity{})
		if result.Error != nil {
			return totalDeleted, fmt.Errorf("failed to delete activities: %w", result.Error)
		}
		totalDeleted += result.RowsAffected
	}

	return totalDeleted, nil
}

// FailInterruptedBackups finalizes backup activities before startup admits work.
func (s *ActivityService) FailInterruptedBackups(ctx context.Context, protectedIDs ...string) error {
	_, err := s.finalizeSwept(ctx, "interrupted backup",
		func() ([]sweeps.Candidate, error) {
			pending, err := s.sweeps.InterruptedBackups(ctx, protectedIDs)
			s.runningMu.Lock()
			defer s.runningMu.Unlock()
			return slices.DeleteFunc(pending, func(candidate sweeps.Candidate) bool {
				_, tracked := s.running[candidate.ID]
				return tracked
			}), err
		},
		func(sweeps.Candidate) (types.ActivityStatus, string) {
			return types.ActivityStatusFailed, "Backup interrupted by Arcane restart"
		},
	)
	return err
}

// SyncJobRun updates a durable job summary without taking an execution slot.
// Only the queue owns its lifecycle, including reopening it for an explicit retry.
func (s *ActivityService) SyncJobRun(ctx context.Context, run types.SchedulerRun, name string) error {
	if err := s.checkInit(); err != nil {
		return err
	}
	if run.ActivityID == "" {
		return errors.New("job activity ID is required")
	}
	lock := s.publishLock(run.ActivityID)
	lock.Lock()
	defer lock.Unlock()
	model := jobActivity(run, name)
	changed := false
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing Activity
		err := tx.First(&existing, "id = ?", run.ActivityID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			changed = true
			return tx.Create(&model).Error
		}
		if err != nil {
			return err
		}
		if existing.Type != types.ActivityTypeJobRun {
			return errors.New("job activity identity belongs to another operation")
		}
		if stamp, ok := existing.Metadata["runUpdatedAt"].(string); ok {
			previous, parseErr := time.Parse(time.RFC3339Nano, stamp)
			if parseErr == nil && !previous.Before(run.UpdatedAt) {
				if existing.EnvironmentID == model.EnvironmentID {
					return nil
				}
				existing.EnvironmentID = model.EnvironmentID
				model = existing
			}
		}
		changed = true
		return tx.Table("activities").Where("id = ?", run.ActivityID).Updates(map[string]any{
			"environment_id": model.EnvironmentID,
			"status":         model.Status, "step": model.Step, "latest_message": model.LatestMessage,
			"ended_at": model.EndedAt, "duration_ms": model.DurationMs, "error": model.Error,
			"metadata": model.Metadata, "updated_at": model.UpdatedAt,
		}).Error
	}); err != nil {
		return fmt.Errorf("synchronize job activity: %w", err)
	}
	if changed {
		// Ordinary activities cannot reopen. Job summaries can, after an explicit
		// retry; the publish lock and durable timestamp reject stale snapshots.
		s.pubsub.Reopen(model.ID)
		s.pubsub.PublishActivity(activityToDTO(&model))
	}
	return nil
}

func (s *ActivityService) ResolveOrphanedJobRuns(ctx context.Context, finished map[string]types.SchedulerRun, protectedIDs ...string) (int64, error) {
	if err := s.checkInit(); err != nil {
		return 0, err
	}
	candidates, err := s.sweeps.OrphanedJobRuns(ctx, protectedIDs)
	if err != nil {
		return 0, fmt.Errorf("find orphaned job run activities: %w", err)
	}
	var resolved int64
	var errs []error
	for _, candidate := range candidates {
		finalized, resolveErr := s.resolveOrphanedJobRun(ctx, candidate.ID, finished)
		if resolveErr != nil {
			errs = append(errs, fmt.Errorf("finalize orphaned job run activity %s: %w", candidate.ID, resolveErr))
			continue
		}
		if finalized {
			resolved++
		}
	}
	return resolved, errors.Join(errs...)
}

func (s *ActivityService) resolveOrphanedJobRun(ctx context.Context, activityID string, finished map[string]types.SchedulerRun) (bool, error) {
	lock := s.publishLock(activityID)
	lock.Lock()
	defer lock.Unlock()
	var model Activity
	var affected int64
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing Activity
		if err := tx.First(&existing, "id = ?", activityID).Error; err != nil {
			return err
		}
		run, found := finished[activityID]
		if !found {
			trigger, _ := existing.Metadata["trigger"].(string)
			domainActivityID, _ := existing.Metadata["domainActivityId"].(string)
			run = types.SchedulerRun{
				ActivityID:    existing.ID,
				ID:            kit.FromPtr(existing.BatchID),
				JobID:         kit.FromPtr(existing.ResourceID),
				EnvironmentID: existing.EnvironmentID,
				Trigger:       trigger,
				RequestedBy:   kit.FromPtr(existing.StartedByUserID),
				Status:        types.RunStatusFailed,
				CreatedAt:     existing.StartedAt,
				UpdatedAt:     time.Now().UTC(),
				Outcome: types.SchedulerOutcome{
					Status:     types.RunStatusFailed,
					Message:    "Job run no longer exists; its result was not recorded",
					ActivityID: domainActivityID,
				},
			}
		}
		model = jobActivity(run, kit.FromPtr(existing.ResourceName))
		result := tx.Table("activities").
			Where("id = ? AND status IN ?", activityID, []types.ActivityStatus{types.ActivityStatusQueued, types.ActivityStatusRunning}).
			Updates(map[string]any{
				"environment_id": model.EnvironmentID,
				"status":         model.Status,
				"step":           model.Step,
				"latest_message": model.LatestMessage,
				"ended_at":       model.EndedAt,
				"duration_ms":    model.DurationMs,
				"error":          model.Error,
				"metadata":       model.Metadata,
				"updated_at":     model.UpdatedAt,
			})
		affected = result.RowsAffected
		return result.Error
	}); err != nil {
		return false, err
	}
	if affected == 0 {
		return false, nil
	}
	s.pubsub.PublishActivity(activityToDTO(&model))
	return true, nil
}

func jobActivity(run types.SchedulerRun, name string) Activity {
	status := types.ActivityStatusQueued
	switch run.Status {
	case types.RunStatusRunning:
		status = types.ActivityStatusRunning
	case types.RunStatusSucceeded, types.RunStatusSkipped:
		status = types.ActivityStatusSuccess
	case types.RunStatusFailed, types.RunStatusPartial, types.RunStatusNeedsAttention:
		status = types.ActivityStatusFailed
	case types.RunStatusCanceled:
		status = types.ActivityStatusCancelled
	case types.RunStatusQueued, types.RunStatusWaiting, types.RunStatusRetrying:
	}
	message := cmp.Or(run.Outcome.Message, name+": "+string(run.Status))
	terminal := slices.Contains(terminalActivityStatuses, status)
	return Activity{
		ID:                run.ActivityID,
		CreatedAt:         run.CreatedAt,
		UpdatedAt:         new(run.UpdatedAt),
		EnvironmentID:     run.EnvironmentID,
		BatchID:           new(run.ID),
		Type:              types.ActivityTypeJobRun,
		Status:            status,
		ResourceType:      new("job"),
		ResourceID:        new(run.JobID),
		ResourceName:      new(name),
		StartedByUserID:   mo.EmptyableToOption(run.RequestedBy).ToPointer(),
		StartedByUsername: mo.EmptyableToOption(run.RequestedBy).ToPointer(),
		StartedAt:         run.CreatedAt,
		Step:              string(run.Status),
		LatestMessage:     message,
		EndedAt:           kit.Ternary(terminal, new(run.UpdatedAt), nil),
		DurationMs:        kit.Ternary(terminal, new(max(int64(0), run.UpdatedAt.Sub(run.CreatedAt).Milliseconds())), nil),
		Error:             kit.Ternary(status == types.ActivityStatusFailed, new(message), nil),
		Metadata: database.JSON{
			"jobId":            run.JobID,
			"runId":            run.ID,
			"environmentId":    run.EnvironmentID,
			"jobStatus":        string(run.Status),
			"trigger":          run.Trigger,
			"attemptCount":     run.AttemptCount,
			"runUpdatedAt":     run.UpdatedAt.Format(time.RFC3339Nano),
			"domainActivityId": run.Outcome.ActivityID,
		},
	}
}

// remoteActivityPageCeiling mirrors the agent's page clamp in PaginateAndSortDB.
const remoteActivityPageCeiling = 100

// normalizeRemoteActivityParams applies the agent's paging bounds so both
// sources are cut the same way; -1 stays the explicit "show all" sentinel.
func normalizeRemoteActivityParams(params pagination.QueryParams) pagination.QueryParams {
	params.Start = max(params.Start, 0)
	switch {
	case params.Limit == -1:
	case params.Limit <= 0:
		params.Limit = 20
	case params.Limit > remoteActivityPageCeiling:
		params.Limit = remoteActivityPageCeiling
	}
	return params
}

// collectActivityWindow reads window rows from a source in pages the
// agent accepts, stopping early when the source runs out. -1 reads everything.
func collectActivityWindow(window int, fetchPage func(start, limit int) ([]types.Activity, int64, error)) ([]types.Activity, int64, error) {
	if window == -1 {
		return fetchPage(0, -1)
	}
	var rows []types.Activity
	var total int64
	for len(rows) < window {
		limit := min(remoteActivityPageCeiling, window-len(rows))
		page, pageTotal, err := fetchPage(len(rows), limit)
		if err != nil {
			return nil, 0, err
		}
		rows = append(rows, page...)
		total = pageTotal
		if len(page) < limit {
			break
		}
	}
	return rows, total, nil
}

// ListRemoteActivities combines manager-owned summaries with agent-owned work
// before slicing the page. The remote records must already match the filters
// and be the agent's newest window; remoteTotal is the agent's filtered count.
func (s *ActivityService) ListRemoteActivities(
	ctx context.Context,
	environmentID string,
	remote []types.Activity,
	remoteTotal int64,
	params pagination.QueryParams,
) ([]types.Activity, pagination.Response, error) {
	params = normalizeRemoteActivityParams(params)
	window := kit.Ternary(params.Limit == -1, -1, params.Start+params.Limit+kit.Ternary(params.SkipCount, 1, 0))
	activities, localTotal, err := collectActivityWindow(window, func(start, limit int) ([]types.Activity, int64, error) {
		local := params
		local.Start, local.Limit = start, limit
		rows, page, err := s.ListActivitiesPaginated(ctx, environmentID, local)
		if err != nil {
			return nil, 0, err
		}
		return rows, page.TotalItems, nil
	})
	if err != nil {
		return nil, pagination.Response{}, err
	}
	for _, item := range remote {
		item.EnvironmentID = environmentID
		activities = append(activities, item)
	}
	total := remoteTotal + localTotal
	config := pagination.Config[types.Activity]{SortBindings: []pagination.SortBinding[types.Activity]{{
		Fn: func(a, b types.Activity) int { return compareActivities(a, b, params) },
	}}}
	hasNextPage := params.Limit != -1 && len(activities) > params.Start+params.Limit
	pageParams := params
	pageParams.Order = pagination.SortAsc
	activities = config.OrderAndPaginate(activities, pageParams)
	if params.SkipCount && params.Limit != -1 {
		return activities, pagination.Response{
			TotalPages:   pagination.UnknownTotal,
			TotalItems:   pagination.UnknownTotal,
			CurrentPage:  params.Start/params.Limit + 1,
			ItemsPerPage: params.Limit,
			HasNextPage:  hasNextPage,
		}, nil
	}
	return activities, pagination.BuildResponse(total, 0, params), nil
}

func (s *ActivityService) AggregateActivities(ctx context.Context, params pagination.QueryParams) (types.Paginated[types.Activity], error) {
	collection, err := httpx.CollectEnvironments[types.Paginated[types.Activity]](ctx, url.Values{"start": {"0"}, "limit": {"-1"}, "includeTotal": {"true"}})
	if err != nil {
		return types.Paginated[types.Activity]{}, err
	}

	pages := make([]pagination.EnvironmentPage[types.Activity], 0, len(collection.Results))
	for _, result := range collection.Results {
		pages = append(pages, pagination.EnvironmentPage[types.Activity]{
			Environment:    result.Environment,
			Items:          result.Value.Data,
			TotalAvailable: cmp.Or(result.Value.Pagination.GrandTotalItems, result.Value.Pagination.TotalItems),
		})
	}
	data, resp := pagination.MergeEnvironmentPages(pagination.Config[types.Activity]{
		SortBindings: []pagination.SortBinding[types.Activity]{{
			Key: kit.Ternary(params.Sort == "environment", "", params.Sort),
			Fn: func(a, b types.Activity) int {
				return compareActivities(a, b, pagination.QueryParams{SortParams: pagination.SortParams{Sort: kit.Ternary(params.Sort == "environment", "", params.Sort)}})
			},
		}},
	}, pages, pagination.QueryParams{
		SearchQuery: params.SearchQuery,
		SortParams: pagination.SortParams{
			Sort:  params.Sort,
			Order: kit.Ternary(params.Sort == "", pagination.SortAsc, params.Order),
		},
		Params:  params.Params,
		Filters: params.Filters,
	})

	return types.Paginated[types.Activity]{
		Success:            true,
		Data:               data,
		Pagination:         handlerutil.PaginationResponse(resp),
		FailedEnvironments: collection.Failures,
	}, nil
}

func compareActivities(a, b types.Activity, params pagination.QueryParams) int {
	if params.Sort == "" {
		aActive := a.Status == types.ActivityStatusQueued || a.Status == types.ActivityStatusRunning
		bActive := b.Status == types.ActivityStatusQueued || b.Status == types.ActivityStatusRunning
		if aActive != bActive {
			return kit.Ternary(aActive, -1, 1)
		}
		aTime, bTime := a.CreatedAt, b.CreatedAt
		if a.EndedAt != nil {
			aTime = *a.EndedAt
		}
		if b.EndedAt != nil {
			bTime = *b.EndedAt
		}
		return cmp.Or(bTime.Compare(aTime), strings.Compare(b.ID, a.ID))
	}

	var result int
	switch params.Sort {
	case "environmentId":
		result = strings.Compare(a.EnvironmentID, b.EnvironmentID)
	case "type":
		result = cmp.Compare(a.Type, b.Type)
	case "status":
		result = cmp.Compare(a.Status, b.Status)
	case "resourceType":
		result = strings.Compare(kit.FromPtr(a.ResourceType), kit.FromPtr(b.ResourceType))
	case "resourceName":
		result = strings.Compare(kit.FromPtr(a.ResourceName), kit.FromPtr(b.ResourceName))
	case "startedAt":
		result = a.StartedAt.Compare(b.StartedAt)
	case "createdAt":
		result = a.CreatedAt.Compare(b.CreatedAt)
	case "updatedAt":
		result = kit.FromPtr(a.UpdatedAt).Compare(kit.FromPtr(b.UpdatedAt))
	case "endedAt":
		result = kit.FromPtr(a.EndedAt).Compare(kit.FromPtr(b.EndedAt))
	case "durationMs":
		result = cmp.Compare(kit.FromPtr(a.DurationMs), kit.FromPtr(b.DurationMs))
	}
	if params.Order == pagination.SortDesc {
		result = -result
	}
	return cmp.Or(result, strings.Compare(a.ID, b.ID))
}
