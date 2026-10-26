package types

import (
	"time"
)

type EnvironmentTest struct {
	// Status of the environment test.
	//
	// Required: true
	Status string `json:"status"`

	// Message providing additional details about the test result.
	//
	// Required: false
	Message *string `json:"message,omitempty"`
}

type AgentMTLSCertificate struct {
	// CommonName is the certificate subject common name.
	//
	// Required: false
	CommonName *string `json:"commonName,omitempty"`

	// ExpiresAt is when the generated mTLS client certificate expires.
	//
	// Required: false
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`

	// DaysRemaining is the rounded number of days until certificate expiry.
	//
	// Required: false
	DaysRemaining *int `json:"daysRemaining,omitempty"`

	// Expired indicates whether the certificate is already expired.
	//
	// Required: false
	Expired bool `json:"expired"`

	// ExpiringSoon indicates whether the certificate is within the warning window.
	//
	// Required: false
	ExpiringSoon bool `json:"expiringSoon"`
}

// Environment represents an environment in API responses.
type Environment struct {
	// ID of the environment.
	//
	// Required: true
	ID string `json:"id" readOnly:"true"`

	// Name of the environment.
	//
	// Required: false
	Name string `json:"name,omitempty" unorm:"nfc" trim:"true"`

	// ApiUrl is the URL of the environment API.
	//
	// Required: false
	ApiUrl string `json:"apiUrl" readOnly:"true"`

	// Status of the environment.
	//
	// Required: true
	Status string `json:"status" readOnly:"true"`

	// Enabled indicates if the environment is enabled.
	//
	// Required: false
	Enabled bool `json:"enabled" required:"false"`

	// LastSeen is the last successful manager-side health/contact timestamp.
	//
	// Required: false
	LastSeen *time.Time `json:"lastSeen,omitempty" readOnly:"true"`

	// AgentTransport indicates the active tunnel transport for an agent environment.
	// Values are "grpc" or "websocket" when connected.
	//
	// Required: false
	AgentTransport *string `json:"agentTransport,omitempty" readOnly:"true"`

	// LastAgentTransport is the most recently used tunnel transport, persisted
	// so the transport stays known while the tunnel is down. Values are "grpc"
	// or "websocket".
	//
	// Required: false
	LastAgentTransport *string `json:"lastAgentTransport,omitempty" readOnly:"true"`

	// AgentSecurityMode indicates how the current agent tunnel authenticated.
	// Values include "token" and "mtls".
	//
	// Required: false
	AgentSecurityMode *string `json:"agentSecurityMode,omitempty" readOnly:"true"`

	// AgentSessionID is the manager-issued ID for the current live agent session.
	//
	// Required: false
	AgentSessionID *string `json:"agentSessionId,omitempty" readOnly:"true"`

	// AgentInstance identifies the currently connected agent runtime instance.
	//
	// Required: false
	AgentInstance *string `json:"agentInstance,omitempty" readOnly:"true"`

	// AgentCapabilities lists the protocol features advertised by the connected agent.
	//
	// Required: false
	AgentCapabilities []string `json:"agentCapabilities,omitempty" readOnly:"true"`

	// AgentRoundTripMs is the last keepalive round trip of a multiplexed tunnel, in milliseconds.
	//
	// Required: false
	AgentRoundTripMs *float64 `json:"agentRoundTripMs,omitempty" readOnly:"true"`

	// AgentOpenStreams counts open tunnel streams, including idle pooled connections, or pending legacy requests.
	//
	// Required: false
	AgentOpenStreams *int `json:"agentOpenStreams,omitempty" readOnly:"true"`

	// Connected reports whether an agent environment currently has a live tunnel.
	//
	// Required: false
	Connected *bool `json:"connected,omitempty" readOnly:"true"`

	// ConnectedAt is when the current agent tunnel connection was established.
	//
	// Required: false
	ConnectedAt *time.Time `json:"connectedAt,omitempty" readOnly:"true"`

	// LastHeartbeat is the timestamp of the most recent heartbeat received
	// from the current agent tunnel connection.
	//
	// Required: false
	LastHeartbeat *time.Time `json:"lastHeartbeat,omitempty" readOnly:"true"`

	// AgentMTLSCertificate describes the generated agent client certificate when available.
	//
	// Required: false
	AgentMTLSCertificate *AgentMTLSCertificate `json:"agentMTLSCertificate,omitempty" readOnly:"true"`

	// ApiKey is returned only when creating or regenerating
	//
	// Required: false
	ApiKey *string `json:"apiKey,omitempty" readOnly:"true"`

	// RegenerateApiKey indicates whether to regenerate the API key on update.
	//
	// Required: false
	RegenerateApiKey bool `json:"regenerateApiKey,omitempty" writeOnly:"true"`
}

// EnvironmentStreamEvent is one line of the environment stream. A "snapshot" event carries
// the caller's full visible environment list with the manager's runtime overlay
// already applied, so clients replace their cached list wholesale rather than
// merging field by field — which also keeps renames and enable toggles live,
// not just connectivity.
type EnvironmentStreamEvent struct {
	// Type is the event kind: "snapshot" or "heartbeat".
	//
	// Required: true
	Type string `json:"type"`

	// Environments is the caller's visible environment list for a "snapshot".
	//
	// Required: false
	Environments []Environment `json:"environments,omitempty"`

	// Timestamp is when the event was produced.
	//
	// Required: true
	Timestamp time.Time `json:"timestamp"`
}

// SwarmBindingCandidate is the binding-relevant state of an environment being attached to a node.
type SwarmBindingCandidate struct {
	Hidden              bool
	Enabled             bool
	ParentEnvironmentID *string
	SwarmNodeID         *string
}

const LocalDockerEnvironmentID = "0"

type DeploymentSnippet struct {
	DockerRun     string                 `json:"dockerRun" doc:"Docker run command snippet"`
	DockerCompose string                 `json:"dockerCompose" doc:"Docker compose YAML snippet"`
	MTLS          *DeploymentSnippetMTLS `json:"mtls,omitempty" doc:"Optional Arcane-generated mTLS deployment assets for agents"`
}

type DeploymentSnippetMTLS struct {
	DockerRun     string                  `json:"dockerRun" doc:"Docker run snippet using Arcane-generated mTLS assets"`
	DockerCompose string                  `json:"dockerCompose" doc:"Docker compose snippet using Arcane-generated mTLS assets"`
	Files         []DeploymentSnippetFile `json:"files" doc:"Generated PEM files to place on the agent host"`
	HostDirHint   string                  `json:"hostDirHint" doc:"Suggested host directory containing the generated PEM files"`
}

type DeploymentSnippetFile struct {
	Name          string `json:"name" doc:"Suggested filename"`
	Content       string `json:"content,omitempty" doc:"PEM file contents. Omitted for sensitive files such as private keys; use downloadUrl instead."`
	DownloadURL   string `json:"downloadUrl,omitempty" doc:"Pairing-permission endpoint to download this file when content is withheld"`
	Sensitive     bool   `json:"sensitive,omitempty" doc:"True when this file is sensitive and must be fetched via downloadUrl"`
	ContainerPath string `json:"containerPath" doc:"Container mount path expected by the mTLS snippet"`
	Permissions   string `json:"permissions" doc:"Suggested file mode"`
}
