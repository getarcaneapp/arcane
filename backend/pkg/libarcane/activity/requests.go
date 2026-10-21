package activity

import (
	"context"

	"github.com/getarcaneapp/arcane/types/v3"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
)

type Service interface {
	StartActivity(ctx context.Context, req StartRequest) (*types.Activity, error)
	CompleteActivity(ctx context.Context, activityID string, status types.ActivityStatus, finalMessage string, errMessage *string, finalStep ...string) (*types.Activity, error)
}

type MessageAppender interface {
	AppendMessage(ctx context.Context, activityID string, req AppendMessageRequest) (*types.ActivityMessage, error)
	// AppendMessages persists a batch of messages in one transaction with a
	// single coalesced activity update; the Writer drains its queue through
	// this so bulk docker output does not fsync per line.
	AppendMessages(ctx context.Context, activityID string, reqs []AppendMessageRequest) ([]types.ActivityMessage, error)
}

// Tracker is an optional interface a Service may implement to make activities
// cancelable. Track derives a cancelable context bound to the activity ID and
// registers it so the activity can later be cancelled via the activity service.
// Implementers release the registration when the activity completes.
type Tracker interface {
	Track(ctx context.Context, activityID string) context.Context
}

// SlotWaiter is an optional interface a Service may implement to support
// per-environment activity concurrency limiting. AwaitActivitySlot blocks
// until the queued activity holds a slot (flipping its status to running), or
// returns the context cause when the wait is cancelled. The slot is released
// when the activity completes.
type SlotWaiter interface {
	AwaitActivitySlot(ctx context.Context, activityID, environmentID string) error
}

type StartRequest struct {
	// ID is an optional caller-chosen ID; starting an existing ID returns that row.
	ID            string
	EnvironmentID string
	// BatchID groups activities spawned by one logical user action. When nil,
	// the batch ID attached to the request context (if any) is used instead.
	BatchID *string
	// Queue routes the activity through the per-environment concurrency
	// limiter: with a free slot it starts running as usual; otherwise it is
	// created with status queued and the caller must block on
	// SlotWaiter.AwaitActivitySlot before doing the work.
	Queue bool
	// DeferSlot persists a queued activity without reserving a slot until its
	// serialized worker starts. It is used by actor-owned work submitted after
	// activity creation so queued tasks cannot hold slots ahead of the actor.
	DeferSlot     bool
	Type          types.ActivityType
	ResourceType  *string
	ResourceID    *string
	ResourceName  *string
	StartedBy     *types.Actor
	Step          string
	LatestMessage string
	Progress      *int
	Metadata      database.JSON
}

type UpdateRequest struct {
	Status        types.ActivityStatus
	ResourceName  *string
	Progress      *int
	Step          *string
	LatestMessage *string
	Error         *string
	Metadata      database.JSON
}

type AppendMessageRequest struct {
	Level    types.ActivityMessageLevel
	Message  string
	Payload  database.JSON
	Progress *int
	Step     string
}
