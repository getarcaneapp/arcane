package jobschedule

// Config represents the configured intervals (in minutes) for Arcane background jobs.
//
// All fields are in minutes.
// This makes conversion to time.Duration straightforward in the backend.
type Config struct {
	EnvironmentHealthInterval  int `json:"environmentHealthInterval"`
	EventCleanupInterval       int `json:"eventCleanupInterval"`
	AnalyticsHeartbeatInterval int `json:"analyticsHeartbeatInterval"`
}

// Update is used to update job schedule intervals (in minutes).
//
// Any nil field is ignored.
type Update struct {
	EnvironmentHealthInterval  *int `json:"environmentHealthInterval,omitempty"`
	EventCleanupInterval       *int `json:"eventCleanupInterval,omitempty"`
	AnalyticsHeartbeatInterval *int `json:"analyticsHeartbeatInterval,omitempty"`
}
