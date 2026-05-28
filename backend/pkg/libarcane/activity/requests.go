package activity

import (
	"context"

	"github.com/getarcaneapp/arcane/backend/internal/models"
	activitytypes "github.com/getarcaneapp/arcane/types/activity"
)

type Service interface {
	StartActivity(ctx context.Context, req StartRequest) (*activitytypes.Activity, error)
	CompleteActivity(ctx context.Context, activityID string, status models.ActivityStatus, finalMessage string, errMessage *string, finalStep ...string) (*activitytypes.Activity, error)
}

type MessageAppender interface {
	AppendMessage(ctx context.Context, activityID string, req AppendMessageRequest) (*activitytypes.Message, error)
}

type StartRequest struct {
	EnvironmentID string
	Type          models.ActivityType
	ResourceType  *string
	ResourceID    *string
	ResourceName  *string
	StartedBy     *models.User
	Step          string
	LatestMessage string
	Progress      *int
	Metadata      models.JSON
}

type UpdateRequest struct {
	Status        models.ActivityStatus
	Progress      *int
	Step          *string
	LatestMessage *string
	Error         *string
	Metadata      models.JSON
}

type AppendMessageRequest struct {
	Level    models.ActivityMessageLevel
	Message  string
	Payload  models.JSON
	Progress *int
	Step     string
}
