// Package operations defines the environment operations contracts shared by Arcane clients.
package operations

import "time"

const (
	CompatibilityCurrent = "current"
	// CompatibilityLegacy marks state derived from an older agent's Dashboard API.
	CompatibilityLegacy = "legacy"

	StreamErrorCodeAgentIncompatible = "agent_incompatible"
	StreamErrorCodeUnreachable       = "unreachable"
)

// WorkloadCount reports an attention total with optional workload-type breakdowns.
// Nil breakdown fields mean the source could not provide that detail.
type WorkloadCount struct {
	Total                int  `json:"total"`
	Projects             *int `json:"projects,omitempty"`
	StandaloneContainers *int `json:"standaloneContainers,omitempty"`
}

// State contains the current attention categories visible to the caller. Nil category
// fields mean the caller cannot access that category, rather than a zero count.
type State struct {
	Updates         *WorkloadCount `json:"updates,omitempty"`
	Stopped         *WorkloadCount `json:"stopped,omitempty"`
	Vulnerabilities *int           `json:"vulnerabilities,omitempty"`
	ExpiringAPIKeys *int           `json:"expiringApiKeys,omitempty"`
	Compatibility   string         `json:"compatibility"`
}

// StreamEvent is one environment-scoped operations stream update.
type StreamEvent struct {
	Type          string    `json:"type"`
	EnvironmentID string    `json:"environmentId,omitempty"`
	State         *State    `json:"state,omitempty"`
	Error         string    `json:"error,omitempty"`
	ErrorCode     string    `json:"errorCode,omitempty"`
	Timestamp     time.Time `json:"timestamp"`
}
