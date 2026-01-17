package jobschedule

// Config represents the configured intervals (in minutes) for Arcane background jobs.
//
// All fields are in minutes.
// This makes conversion to time.Duration straightforward in the backend.
type Config struct {
	EnvironmentHealthInterval  string `json:"environmentHealthInterval"`
	EventCleanupInterval       string `json:"eventCleanupInterval"`
	AnalyticsHeartbeatInterval string `json:"analyticsHeartbeatInterval"`
}

// Update is used to update job schedule intervals (in minutes).
//
// Any nil field is ignored.
type Update struct {
	EnvironmentHealthInterval  *string `json:"environmentHealthInterval,omitempty"`
	EventCleanupInterval       *string `json:"eventCleanupInterval,omitempty"`
	AnalyticsHeartbeatInterval *string `json:"analyticsHeartbeatInterval,omitempty"`
}
