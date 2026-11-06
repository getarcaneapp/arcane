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
		Queue          bool
	}
)

const (
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

func AwaitHandlerActivitySlot(ctx context.Context, activityService Service, activityID, environmentID string) {
	if strings.TrimSpace(activityID) == "" {
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

func StartHandlerActivity(ctx context.Context, activityService Service, opts HandlerOptions) (string, context.Context) {
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
	if strings.TrimSpace(activityID) == "" {
		return
	}

	status := types.ActivityStatusSuccess
	var errMessage *string
	finalMessage := successMessage
	if err != nil {
		// Read the cause before the context is re-wrapped for the DB write.
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
