// Package stream defines the multiplexed client stream: one connection that
// carries every live channel the caller asked for.
//
// Browsers cap concurrent HTTP/1.1 requests per origin at six, and every
// long-lived stream permanently consumes one of them. Multiplexing keeps that
// budget for ordinary requests, and gives the client a single reconnect and
// heartbeat to reason about instead of one per feature.
package stream

import (
	"time"

	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	dashboardtypes "github.com/getarcaneapp/arcane/types/v2/dashboard"
	environmenttypes "github.com/getarcaneapp/arcane/types/v2/environment"
)

// Channel names a subscribable feed. Callers request channels explicitly
// because they differ enormously in cost: environments is one local query,
// while dashboard proxies a request to every remote agent on every tick.
const (
	ChannelEnvironments = "environments"
	ChannelDashboard    = "dashboard"
	ChannelActivities   = "activities"
)

// Event is one line of the multiplexed stream. Exactly one payload is set, and
// each payload keeps its own channel's event type rather than being flattened
// here, so a channel can add a field without this envelope changing.
type Event struct {
	// Channel identifies which feed produced this event. Empty on a heartbeat.
	//
	// Required: false
	Channel string `json:"channel,omitempty"`

	// Type is set to "heartbeat" for connection keepalives; otherwise the
	// per-channel payload carries its own type.
	//
	// Required: false
	Type string `json:"type,omitempty"`

	// Dashboard carries a ChannelDashboard payload.
	//
	// Required: false
	Dashboard *dashboardtypes.StreamEvent `json:"dashboard,omitempty"`

	// Activity carries a ChannelActivities payload.
	//
	// Required: false
	Activity *activitytypes.StreamEvent `json:"activity,omitempty"`

	// Environment carries a ChannelEnvironments payload.
	//
	// Required: false
	Environment *environmenttypes.StreamEvent `json:"environment,omitempty"`

	// Timestamp is when the envelope was produced.
	//
	// Required: true
	Timestamp time.Time `json:"timestamp"`
}
