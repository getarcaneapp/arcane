package activity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/pkg/utils"
)

// ErrCanceled is the cancellation cause set on an activity's work context when a
// user requests cancellation. Completion paths read context.Cause to record a
// cancelled (rather than failed) terminal status.
var ErrCanceled = errors.New("activity cancelled by user")

// cancelledMessage is the latest-message recorded when work is cancelled.
const cancelledMessage = "Cancelled by user"

// CancelledByContext reports whether ctx was cancelled by a user cancellation
// request (as opposed to app shutdown or a deadline). Callers that finalize an
// activity from a possibly-cancelled work context use this to choose between a
// cancelled and a failed terminal status.
func CancelledByContext(ctx context.Context) bool {
	return ctx != nil && errors.Is(context.Cause(ctx), ErrCanceled)
}

type HandlerOptions struct {
	EnvironmentID  string
	Type           models.ActivityType
	ResourceType   string
	ResourceID     string
	ResourceName   string
	User           *models.User
	Step           string
	Message        string
	SuccessMessage string
	Metadata       models.JSON
}

// StartHandlerActivityForUser creates a background activity and returns its ID
// along with a work context the caller MUST use for the underlying operation.
// When the service supports cancellation (implements Tracker), the returned
// context is a cancelable child bound to the activity; cancelling the activity
// cancels this context. The activity registration is released when the activity
// is completed via the service. On failure it returns ("", ctx) unchanged.
func StartHandlerActivityForUser(
	ctx context.Context,
	activityService Service,
	environmentID string,
	activityType models.ActivityType,
	resourceType string,
	resourceID string,
	resourceName string,
	user *models.User,
	step string,
	message string,
	metadata models.JSON,
) (string, context.Context) {
	if activityService == nil {
		return "", ctx
	}

	activity, err := activityService.StartActivity(ctx, StartRequest{
		EnvironmentID: environmentID,
		Type:          activityType,
		ResourceType:  utils.StringPtrFromTrimmed(resourceType),
		ResourceID:    utils.StringPtrFromTrimmed(resourceID),
		ResourceName:  utils.StringPtrFromTrimmed(resourceName),
		StartedBy:     user,
		Step:          step,
		LatestMessage: message,
		Metadata:      metadata,
	})
	if err != nil {
		slog.DebugContext(ctx, "failed to start background activity", "type", activityType, "error", err)
		return "", ctx
	}

	workCtx := ctx
	if tracker, ok := activityService.(Tracker); ok {
		workCtx = tracker.Track(ctx, activity.ID)
	}
	return activity.ID, workCtx
}

func CompleteHandlerActivity(ctx context.Context, activityService Service, activityID string, successMessage string, err error) {
	if activityService == nil || strings.TrimSpace(activityID) == "" {
		return
	}

	status := models.ActivityStatusSuccess
	var errMessage *string
	finalMessage := successMessage
	if err != nil {
		// Read the cancellation cause from the (possibly-tracked) work context
		// before it is re-wrapped for the DB write below.
		if CancelledByContext(ctx) {
			status = models.ActivityStatusCancelled
			finalMessage = cancelledMessage
		} else {
			status = models.ActivityStatusFailed
			errText := err.Error()
			errMessage = &errText
			finalMessage = errText
		}
	}

	activityCtx := utils.ActivityRuntimeContext(ctx, nil)
	if _, completeErr := activityService.CompleteActivity(activityCtx, activityID, status, finalMessage, errMessage); completeErr != nil {
		slog.DebugContext(activityCtx, "failed to complete background activity", "activityId", activityID, "error", completeErr)
	}
}

// RunHandlerActivity starts an activity, runs action with the activity's work
// context (cancelable when the service supports it), and completes the activity.
// The action MUST use the provided context for its operation so cancellation
// propagates.
func RunHandlerActivity(ctx context.Context, activityService Service, opts HandlerOptions, action func(ctx context.Context) error) (string, error) {
	activityID, workCtx := StartHandlerActivityForUser(
		ctx,
		activityService,
		opts.EnvironmentID,
		opts.Type,
		opts.ResourceType,
		opts.ResourceID,
		opts.ResourceName,
		opts.User,
		opts.Step,
		opts.Message,
		opts.Metadata,
	)

	err := action(workCtx)
	CompleteHandlerActivity(workCtx, activityService, activityID, opts.SuccessMessage, err)
	return activityID, err
}

func WriteStartedLine(writer io.Writer, activityID string) {
	if writer == nil || strings.TrimSpace(activityID) == "" {
		return
	}

	payload := map[string]string{
		"type":       "activity",
		"activityId": activityID,
	}
	if err := json.NewEncoder(writer).Encode(payload); err != nil {
		_, _ = fmt.Fprintf(writer, `{"activityId":%q}`+"\n", activityID)
	}
}

func FlushWriter(writer io.Writer) {
	if flusher, ok := writer.(interface{ Flush() }); ok {
		flusher.Flush()
	}
}
