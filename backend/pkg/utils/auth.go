package utils

// Auth header names and path prefixes shared between the Echo middleware
// (WebSocket/diagnostics) and the Huma auth bridge (REST). Keep these in one
// place so a change to a header name applies to every route type at once.
const (
	HeaderAgentBootstrap  = "X-Arcane-Agent-Bootstrap"
	HeaderAgentToken      = "X-Arcane-Agent-Token" // #nosec G101: header name, not a credential
	HeaderApiKey          = "X-Api-Key"            // #nosec G101: header name, not a credential
	HeaderActivityBatchID = "X-Arcane-Batch-Id"
	AgentPairingPrefix    = "/api/environments/0/agent/pair"

	// HeaderActorUserID and HeaderActorUsername carry the identity of the
	// human authenticated on the manager when it proxies a request to an
	// agent. The agent authenticates the request itself via the agent token
	// (Sudo permissions either way) but attributes audit events/logs to
	// this forwarded identity instead of its own service account when
	// present. Manager-side, these headers are stripped from the inbound
	// request before being set from the manager's own authenticated user,
	// so a client cannot forge them (see environment_middleware.go).
	HeaderActorUserID   = "X-Arcane-Actor-Id"
	HeaderActorUsername = "X-Arcane-Actor-Username"

	// HeaderIconCatalog carries the requesting user's icon catalog preference to
	// remote environments. Agents authenticate proxied calls as a synthetic user
	// with no preferences, so without it every remote environment would resolve
	// container/project icons against the default catalog.
	HeaderIconCatalog = "X-Arcane-Icon-Catalog"
)
