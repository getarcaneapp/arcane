package activity

import (
	"context"

	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/types/v3"
)

type (
	Service interface {
		StartActivity(ctx context.Context, req StartRequest) (*types.Activity, error)
		CompleteActivity(ctx context.Context, activityID string, status types.ActivityStatus, finalMessage string, errMessage *string, finalStep ...string) (*types.Activity, error)
	}

	MessageAppender interface {
		AppendMessage(ctx context.Context, activityID string, req AppendMessageRequest) (*types.ActivityMessage, error)
		AppendMessages(ctx context.Context, activityID string, reqs []AppendMessageRequest) ([]types.ActivityMessage, error)
	}

	Tracker interface {
		Track(ctx context.Context, activityID string) context.Context
	}

	SlotWaiter interface {
		AwaitActivitySlot(ctx context.Context, activityID, environmentID string) error
	}

	StartRequest struct {
		BatchID       *string
		ResourceType  *string
		ResourceID    *string
		ResourceName  *string
		StartedBy     *types.Actor
		Progress      *int
		Metadata      database.JSON
		ID            string
		EnvironmentID string
		Type          types.ActivityType
		Step          string
		LatestMessage string
		Queue         bool
		DeferSlot     bool
	}

	UpdateRequest struct {
		ResourceName  *string
		Progress      *int
		Step          *string
		LatestMessage *string
		Error         *string
		Metadata      database.JSON
		Status        types.ActivityStatus
	}

	AppendMessageRequest struct {
		Level    types.ActivityMessageLevel
		Message  string
		Payload  database.JSON
		Progress *int
		Step     string
	}
)
