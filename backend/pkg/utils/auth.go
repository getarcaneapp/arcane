package utils

// Auth header names and path prefixes shared between the Echo middleware
// (WebSocket/diagnostics) and the Huma auth bridge (REST). Keep these in one
// place so a change to a header name applies to every route type at once.
const (
	HeaderAgentBootstrap  = "X-Arcane-Agent-Bootstrap"
	HeaderAgentToken      = "X-Arcane-Agent-Token" // #nosec G101: header name, not a credential
	HeaderApiKey          = "X-Api-Key"            // #nosec G101: header name, not a credential
	HeaderActivityBatchID = "X-Arcane-Batch-Id"
	// HeaderIconCatalog carries the requesting user's icon catalog preference to
	// remote environments. Agents authenticate proxied calls as a synthetic user
	// with no preferences, so without it every remote environment would resolve
	// container/project icons against the default catalog.
	HeaderIconCatalog  = "X-Arcane-Icon-Catalog"
	AgentPairingPrefix = "/api/environments/0/agent/pair"
)
