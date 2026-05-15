package utils

import "slices"

// Auth header names and path prefixes shared between the Echo middleware
// (WebSocket/diagnostics) and the Huma auth bridge (REST). Keep these in one
// place so a change to a header name applies to every route type at once.
const (
	HeaderAgentBootstrap = "X-Arcane-Agent-Bootstrap"
	HeaderAgentToken     = "X-Arcane-Agent-Token" // #nosec G101: header name, not a credential
	HeaderApiKey         = "X-API-Key"            // #nosec G101: header name, not a credential
	AgentPairingPrefix   = "/api/environments/0/agent/pair"
)

// UserHasRole reports whether the user's roles contains the given role.
func UserHasRole(roles []string, role string) bool {
	return slices.Contains(roles, role)
}
