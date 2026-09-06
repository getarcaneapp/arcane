package event

import "time"

// StreamEvent invalidates event queries after persistence changes.
type StreamEvent struct {
	// Type is "changed"; clients reload their current event query.
	Type string `json:"type"`
	// Timestamp is when the invalidation was emitted.
	Timestamp time.Time `json:"timestamp"`
}
