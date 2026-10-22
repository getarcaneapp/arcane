package types

import (
	"time"
)

// Channel names a subscribable feed. Callers request channels explicitly
// because they differ enormously in cost: environments is one local query,
// while dashboard proxies a request to every remote agent on every tick.
const (
	StreamChannelEnvironments = "environments"
	StreamChannelDashboard    = "dashboard"
	StreamChannelActivities   = "activities"
	StreamChannelEvents       = "events"
	StreamChannelVersion      = "version"
	StreamChannelUpgrade      = "arcane-upgrade"
)

// StreamEvent is the JSON data of one message in the multiplexed SSE stream.
// Exactly one payload is set, retaining its channel's event type.
type StreamEvent struct {
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
	Dashboard *DashboardStreamEvent `json:"dashboard,omitempty"`

	// Activity carries a ChannelActivities payload.
	//
	// Required: false
	Activity *ActivityStreamEvent `json:"activity,omitempty"`

	// Environment carries a ChannelEnvironments payload.
	//
	// Required: false
	Environment *EnvironmentStreamEvent `json:"environment,omitempty"`

	// EventLog carries a ChannelEvents invalidation.
	//
	// Required: false
	EventLog *EventStreamEvent `json:"eventLog,omitempty"`

	// Version carries a ChannelVersion payload.
	//
	// Required: false
	Version *VersionStreamEvent `json:"version,omitempty"`

	Upgrade *UpgradeJob `json:"upgrade,omitempty"`

	// Timestamp is when the envelope was produced.
	//
	// Required: true
	Timestamp time.Time `json:"timestamp"`
}
