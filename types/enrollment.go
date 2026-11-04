package types

import (
	"time"
)

type (
	EnrollmentToken struct {
		CreatedAt       time.Time  `json:"createdAt" doc:"Creation timestamp"`
		LastUsedAt      *time.Time `json:"lastUsedAt,omitempty" doc:"When an agent last enrolled with this token"`
		EnvironmentID   *string    `json:"environmentId,omitempty" doc:"Environment the token enrolls into, when bound"`
		EnvironmentName *string    `json:"environmentName,omitempty" doc:"Name of the bound environment"`
		MaxUses         *int       `json:"maxUses,omitempty" doc:"Maximum number of enrollments; absent means unlimited"`
		ID              string     `json:"id" doc:"Enrollment token ID"`
		Name            string     `json:"name" doc:"Enrollment token name"`
		UseCount        int        `json:"useCount" doc:"Number of enrollments made with this token"`
	}

	CreateEnrollmentTokenRequest struct {
		MaxUses *int   `json:"maxUses,omitempty" required:"false" minimum:"1" maximum:"10000" doc:"Maximum number of enrollments; absent or null means unlimited"`
		Name    string `json:"name" minLength:"1" maxLength:"100" doc:"Enrollment token name" unorm:"nfc" trim:"true"`
	}

	EnrollmentTokenCreated struct {
		CreatedAt time.Time `json:"createdAt" doc:"Creation timestamp"`
		MaxUses   *int      `json:"maxUses,omitempty" doc:"Maximum number of enrollments; absent means unlimited"`
		ID        string    `json:"id" doc:"Enrollment token ID"`
		Name      string    `json:"name" doc:"Enrollment token name"`
		Token     string    `json:"token" doc:"Enrollment token"`
	}

	EnrollmentTokenSecret struct {
		Token string `json:"token" doc:"Enrollment token"`
	}

	AgentEnrollRequest struct {
		Hostname       string `json:"hostname" maxLength:"253" doc:"Hostname of the enrolling agent" trim:"true"`
		SwarmNodeID    string `json:"swarmNodeId,omitempty" required:"false" maxLength:"64" doc:"Swarm node ID when the agent runs on an active swarm node"`
		SwarmClusterID string `json:"swarmClusterId,omitempty" required:"false" maxLength:"64" doc:"Swarm cluster ID when the agent runs on an active swarm node"`
	}

	AgentEnrollResponse struct {
		EnvironmentID string `json:"environmentId" doc:"Environment ID of the enrolled agent"`
		Token         string `json:"token" doc:"Agent token"`
	}
)

const (
	EnrollmentTokenPrefix = "arc_etkn_"
)
