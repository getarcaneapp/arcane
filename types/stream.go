package types

import (
	"time"
)

type (
	// Exactly one payload is set, retaining its channel's event type.
	StreamEvent struct {
		Timestamp   time.Time               `json:"timestamp"`
		Activity    *ActivityStreamEvent    `json:"activity,omitempty"`
		Environment *EnvironmentStreamEvent `json:"environment,omitempty"`
		EventLog    *EventStreamEvent       `json:"eventLog,omitempty"`
		Version     *VersionStreamEvent     `json:"version,omitempty"`

		Upgrade *UpgradeJob `json:"upgrade,omitempty"`

		Channel string `json:"channel,omitempty"`

		Type string `json:"type,omitempty"`
	}
)

const (
	StreamChannelEnvironments = "environments"
	StreamChannelActivities   = "activities"
	StreamChannelEvents       = "events"
	StreamChannelVersion      = "version"
	StreamChannelUpgrade      = "arcane-upgrade"
)
