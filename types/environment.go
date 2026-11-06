package types

import (
	"time"
)

type (
	EnvironmentTest struct {
		Message *string `json:"message,omitempty"`
		Status  string  `json:"status"`
	}

	EnvironmentTunnelRestart struct {
		SessionID   string `json:"sessionId,omitzero"`
		Reconnected bool   `json:"reconnected"`
	}

	AgentMTLSCertificate struct {
		CommonName    *string    `json:"commonName,omitempty"`
		ExpiresAt     *time.Time `json:"expiresAt,omitempty"`
		DaysRemaining *int       `json:"daysRemaining,omitempty"`
		Expired       bool       `json:"expired"`
		ExpiringSoon  bool       `json:"expiringSoon"`
	}

	AgentHostInfo struct {
		Hostname      string `json:"hostname,omitempty"`
		OS            string `json:"os,omitempty"`
		KernelVersion string `json:"kernelVersion,omitempty"`
		Architecture  string `json:"architecture,omitempty"`
		DockerVersion string `json:"dockerVersion,omitempty"`
		AgentVersion  string `json:"agentVersion,omitempty"`
		PublicIP      string `json:"publicIp,omitempty"`
		MemoryBytes   int64  `json:"memoryBytes,omitempty"`
		CPUs          int32  `json:"cpus,omitempty"`
	}

	AgentTunnelStats struct {
		RoundTripMinMs       *float64   `json:"roundTripMinMs,omitempty"`
		RoundTripAvgMs       *float64   `json:"roundTripAvgMs,omitempty"`
		RoundTripMaxMs       *float64   `json:"roundTripMaxMs,omitempty"`
		LastDisconnectAt     *time.Time `json:"lastDisconnectAt,omitempty"`
		LastDisconnectReason string     `json:"lastDisconnectReason,omitempty"`
		BytesIn              int64      `json:"bytesIn"`
		BytesOut             int64      `json:"bytesOut"`
		StreamsOpened        int64      `json:"streamsOpened"`
		Sessions             int        `json:"sessions"`
	}

	Environment struct {
		LastSeen *time.Time `json:"lastSeen,omitempty" readOnly:"true"`

		AgentTransport *string `json:"agentTransport,omitempty" readOnly:"true"`

		LastAgentTransport *string `json:"lastAgentTransport,omitempty" readOnly:"true"`

		// Values include "token" and "mtls".
		AgentSecurityMode *string `json:"agentSecurityMode,omitempty" readOnly:"true"`
		AgentSessionID    *string `json:"agentSessionId,omitempty" readOnly:"true"`
		AgentInstance     *string `json:"agentInstance,omitempty" readOnly:"true"`

		AgentRoundTripMs     *float64              `json:"agentRoundTripMs,omitempty" readOnly:"true"`
		AgentOpenStreams     *int                  `json:"agentOpenStreams,omitempty" readOnly:"true"`
		Connected            *bool                 `json:"connected,omitempty" readOnly:"true"`
		ConnectedAt          *time.Time            `json:"connectedAt,omitempty" readOnly:"true"`
		LastHeartbeat        *time.Time            `json:"lastHeartbeat,omitempty" readOnly:"true"`
		AgentMTLSCertificate *AgentMTLSCertificate `json:"agentMTLSCertificate,omitempty" readOnly:"true"`

		AgentHost *AgentHostInfo `json:"agentHost,omitempty" readOnly:"true"`

		AgentTunnelStats  *AgentTunnelStats `json:"agentTunnelStats,omitempty" readOnly:"true"`
		ID                string            `json:"id" readOnly:"true"`
		Name              string            `json:"name,omitempty" unorm:"nfc" trim:"true"`
		ApiUrl            string            `json:"apiUrl" readOnly:"true"`
		Status            string            `json:"status" readOnly:"true"`
		AgentCapabilities []string          `json:"agentCapabilities,omitempty" readOnly:"true"`
		Enabled           bool              `json:"enabled" required:"false"`
	}

	EnvironmentStreamEvent struct {
		Timestamp    time.Time     `json:"timestamp"`
		Type         string        `json:"type"`
		Environments []Environment `json:"environments,omitempty"`
	}

	SwarmBindingCandidate struct {
		ParentEnvironmentID *string
		SwarmNodeID         *string
		Hidden              bool
		Enabled             bool
	}

	DeploymentSnippet struct {
		MTLS            *DeploymentSnippetMTLS `json:"mtls,omitempty" doc:"Optional Arcane-generated mTLS deployment assets for agents"`
		DockerRun       string                 `json:"dockerRun" doc:"Docker run command snippet"`
		DockerCompose   string                 `json:"dockerCompose" doc:"Docker compose YAML snippet"`
		EnrollmentToken string                 `json:"enrollmentToken" doc:"Single-use enrollment token the agent uses to join this environment, shown only once"`
	}

	DeploymentSnippetMTLS struct {
		DockerRun     string                  `json:"dockerRun" doc:"Docker run snippet using Arcane-generated mTLS assets"`
		DockerCompose string                  `json:"dockerCompose" doc:"Docker compose snippet using Arcane-generated mTLS assets"`
		HostDirHint   string                  `json:"hostDirHint" doc:"Suggested host directory containing the generated PEM files"`
		Files         []DeploymentSnippetFile `json:"files" doc:"Generated PEM files to place on the agent host"`
	}

	DeploymentSnippetFile struct {
		Name          string `json:"name" doc:"Suggested filename"`
		Content       string `json:"content,omitempty" doc:"PEM file contents. Omitted for sensitive files such as private keys; use downloadUrl instead."`
		DownloadURL   string `json:"downloadUrl,omitempty" doc:"Pairing-permission endpoint to download this file when content is withheld"`
		ContainerPath string `json:"containerPath" doc:"Container mount path expected by the mTLS snippet"`
		Permissions   string `json:"permissions" doc:"Suggested file mode"`
		Sensitive     bool   `json:"sensitive,omitempty" doc:"True when this file is sensitive and must be fetched via downloadUrl"`
	}
)

const (
	LocalDockerEnvironmentID = "0"
)
