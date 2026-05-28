package activity

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/pkg/utils"
)

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
) string {
	if activityService == nil {
		return ""
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
		return ""
	}
	return activity.ID
}

func CompleteHandlerActivity(ctx context.Context, activityService Service, activityID string, successMessage string, err error) {
	if activityService == nil || strings.TrimSpace(activityID) == "" {
		return
	}

	status := models.ActivityStatusSuccess
	var errMessage *string
	finalMessage := successMessage
	if err != nil {
		status = models.ActivityStatusFailed
		errText := err.Error()
		errMessage = &errText
		finalMessage = errText
	}

	activityCtx := utils.ActivityRuntimeContext(ctx, nil)
	if _, completeErr := activityService.CompleteActivity(activityCtx, activityID, status, finalMessage, errMessage); completeErr != nil {
		slog.DebugContext(activityCtx, "failed to complete background activity", "activityId", activityID, "error", completeErr)
	}
}

func RunHandlerActivity(ctx context.Context, activityService Service, opts HandlerOptions, action func() error) (string, error) {
	activityID := StartHandlerActivityForUser(
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

	err := action()
	CompleteHandlerActivity(ctx, activityService, activityID, opts.SuccessMessage, err)
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
