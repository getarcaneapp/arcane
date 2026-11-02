package activity

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/samber/mo"

	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/scheduler/jobcontext"
	"getarcane.dev/types/v3"
)

type (
	handlerActivityIDContextKey struct{}

	HandlerOptions struct {
		User           *types.Actor
		Metadata       database.JSON
		EnvironmentID  string
		Type           types.ActivityType
		ResourceType   string
		ResourceID     string
		ResourceName   string
		Step           string
		Message        string
		SuccessMessage string
		// Queue routes the activity through the per-environment concurrency
		// limiter so bulk long-running operations wait visibly instead of all
		// running at once. Quick actions (start/stop/delete) must not set this.
		Queue bool
	}
)

const (
	// ErrCanceled is the cancellation cause set on an activity's work context when a
	// user requests cancellation. Completion paths read context.Cause to record a
	// cancelled (rather than failed) terminal status.
	// cancelledMessage is the latest-message recorded when work is cancelled.
	cancelledMessage = "Cancelled by user"
)

var ErrCanceled = errors.New("activity cancelled by user")

func IDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	activityID, _ := ctx.Value(handlerActivityIDContextKey{}).(string)
	return strings.TrimSpace(activityID)
}

// AwaitHandlerActivitySlot blocks until the queued activity holds a
// concurrency slot (flipping it to running). A cancellation while waiting is
// not an error to the caller: the work context is already cancelled, so the
// subsequent action fails fast and CompleteHandlerActivity records the
// cancelled status.
func AwaitHandlerActivitySlot(ctx context.Context, activityService Service, activityID, environmentID string) {
	if activityService == nil || strings.TrimSpace(activityID) == "" {
		return
	}
	waiter, ok := activityService.(SlotWaiter)
	if !ok {
		return
	}
	if err := waiter.AwaitActivitySlot(ctx, activityID, environmentID); err != nil {
		slog.DebugContext(ctx, "queued activity wait ended before acquiring a slot", "activityId", activityID, "error", err)
	}
}

// StartHandlerActivity creates a background activity and returns its ID along
// with a work context the caller MUST use for the underlying operation. When
// the service supports cancellation (implements Tracker), the returned context
// is a cancelable child bound to the activity; cancelling the activity cancels
// this context. The activity registration is released when the activity is
// completed via the service. On failure it returns ("", ctx) unchanged.
//
// When queue is true the activity is routed through the per-environment
// concurrency limiter: when no slot is free the activity is created with
// status queued. The caller MUST then call AwaitHandlerActivitySlot on the
// returned work context before doing the actual work (streaming endpoints
// write their started line in between, so the client learns the activity ID
// before the queue wait begins). Quick actions (start/stop/delete) must pass
// false.
func StartHandlerActivity(ctx context.Context, activityService Service, opts HandlerOptions) (string, context.Context) {
	if activityService == nil {
		return "", ctx
	}

	activity, err := activityService.StartActivity(ctx, StartRequest{
		EnvironmentID: opts.EnvironmentID,
		Type:          opts.Type,
		Queue:         opts.Queue,
		ResourceType:  mo.EmptyableToOption(strings.TrimSpace(opts.ResourceType)).ToPointer(),
		ResourceID:    mo.EmptyableToOption(strings.TrimSpace(opts.ResourceID)).ToPointer(),
		ResourceName:  mo.EmptyableToOption(strings.TrimSpace(opts.ResourceName)).ToPointer(),
		StartedBy:     opts.User,
		Step:          opts.Step,
		LatestMessage: opts.Message,
		Metadata:      opts.Metadata,
	})
	if err != nil {
		slog.DebugContext(ctx, "failed to start background activity", "type", opts.Type, "error", err)
		return "", ctx
	}

	workCtx := ctx
	if tracker, ok := activityService.(Tracker); ok {
		workCtx = tracker.Track(ctx, activity.ID)
	}
	workCtx = context.WithValue(workCtx, handlerActivityIDContextKey{}, activity.ID)
	return activity.ID, workCtx
}

func CompleteHandlerActivity(ctx context.Context, activityService Service, activityID, successMessage string, err error) {
	if activityService == nil || strings.TrimSpace(activityID) == "" {
		return
	}

	status := types.ActivityStatusSuccess
	var errMessage *string
	finalMessage := successMessage
	if err != nil {
		// Read the cancellation cause from the (possibly-tracked) work context
		// before it is re-wrapped for the DB write below.
		if errors.Is(context.Cause(ctx), ErrCanceled) {
			status = types.ActivityStatusCancelled
			finalMessage = cancelledMessage
		} else {
			status = types.ActivityStatusFailed
			errText := err.Error()
			errMessage = &errText
			finalMessage = errText
		}
	}

	activityCtx := ActivityRuntimeContext(ctx, nil)
	if _, completeErr := activityService.CompleteActivity(activityCtx, activityID, status, finalMessage, errMessage); completeErr != nil {
		slog.WarnContext(activityCtx, "failed to complete background activity", "activityId", activityID, "error", completeErr)
	}
}

// RunHandlerActivity starts an activity, runs action with the activity's work
// context (cancelable when the service supports it), and completes the activity.
// The action MUST use the provided context for its operation so cancellation
// propagates.
func RunHandlerActivity(ctx context.Context, activityService Service, opts HandlerOptions, action func(ctx context.Context) error) (string, error) {
	activityID, workCtx := StartHandlerActivity(ctx, activityService, opts)
	if opts.Queue {
		AwaitHandlerActivitySlot(workCtx, activityService, activityID, opts.EnvironmentID)
	}

	err := jobcontext.Progress(workCtx, types.TargetOutcome{ID: opts.ResourceID, Status: types.RunStatusRunning, ActivityID: activityID})
	if err == nil {
		err = action(workCtx)
	}
	CompleteHandlerActivity(workCtx, activityService, activityID, opts.SuccessMessage, err)
	target := types.TargetOutcome{ID: opts.ResourceID, Status: types.RunStatusSucceeded, ActivityID: activityID}
	if err != nil {
		target.Status = types.RunStatusFailed
		target.Message = err.Error()
	}
	if progressErr := jobcontext.Progress(workCtx, target); progressErr != nil && err == nil {
		err = progressErr
	}
	return activityID, err
}

// WriteDoneLine writes the terminal success frame of an operation NDJSON
// stream. Clients resolve on this frame rather than relying on the network
// EOF, which proxies do not always propagate promptly.
func WriteDoneLine(writer io.Writer) {
	if writer == nil {
		return
	}
	_, _ = io.WriteString(writer, "{\"done\":true}\n")
	if flusher, ok := writer.(http.Flusher); ok {
		flusher.Flush()
	}
}

func WriteErrorLine(writer io.Writer, err error) {
	if writer == nil || err == nil {
		return
	}
	_ = json.MarshalWrite(writer, map[string]string{"error": err.Error()})
	_, _ = io.WriteString(writer, "\n")
	if flusher, ok := writer.(http.Flusher); ok {
		flusher.Flush()
	}
}

func WriteStartedLine(writer io.Writer, activityID string) {
	if writer == nil || strings.TrimSpace(activityID) == "" {
		return
	}

	payload := map[string]string{
		"type":       "activity",
		"activityId": activityID,
	}
	if err := json.MarshalWrite(writer, payload); err != nil {
		_, _ = fmt.Fprintf(writer, "{\"activityId\":%q}\n", activityID)
		return
	}
	_, _ = io.WriteString(writer, "\n")
}

func FlushWriter(writer io.Writer) {
	if flusher, ok := writer.(interface{ Flush() }); ok {
		flusher.Flush()
	}
}
