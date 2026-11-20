package environment

import (
	"strings"
	"time"

	"getarcane.dev/arcane/v3/internal/database"
)

type (
	Environment struct {
		database.BaseModel

		LastSeen              *time.Time `json:"lastSeen" gorm:"column:last_seen"`
		LastAgentTransport    *string    `json:"lastAgentTransport" gorm:"column:last_agent_transport"`
		AccessToken           *string    `json:"-" gorm:"column:access_token"`
		ApiKeyID              *string    `json:"-" gorm:"column:api_key_id"`
		ParentEnvironmentID   *string    `json:"-" gorm:"column:parent_environment_id"`
		SwarmNodeID           *string    `json:"-" gorm:"column:swarm_node_id"`
		DockerHost            *string    `json:"dockerHost" gorm:"column:docker_host"`
		EnrollmentRequestHash *string    `json:"-" gorm:"column:enrollment_request_hash"`

		Name    string `json:"name" sortable:"true" unorm:"nfc" trim:"true"`
		ApiUrl  string `json:"apiUrl" gorm:"column:api_url" sortable:"true"`
		Status  string `json:"status" sortable:"true"`
		Enabled bool   `json:"enabled" sortable:"true"`
		Hidden  bool   `json:"hidden" gorm:"column:hidden;default:false"`
	}

	EnvironmentStatus string
)

const (
	EnvironmentStatusOnline  EnvironmentStatus = "online"
	EnvironmentStatusOffline EnvironmentStatus = "offline"
	EnvironmentStatusError   EnvironmentStatus = "error"
	EnvironmentStatusPending EnvironmentStatus = "pending"
)

func (Environment) TableName() string { return "environments" }

// DockerHostURL returns the configured Docker API endpoint for a docker-host environment, or an
// empty string for agent-backed environments.
func (e Environment) DockerHostURL() string {
	if e.DockerHost == nil {
		return ""
	}
	return strings.TrimSpace(*e.DockerHost)
}

// IsDockerHost reports whether the manager talks to this environment's Docker API directly
// instead of routing through an Arcane agent tunnel.
func (e Environment) IsDockerHost() bool {
	return e.DockerHostURL() != ""
}
