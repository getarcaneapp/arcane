package models

import "time"

type Environment struct {
	BaseModel

	Name                string     `json:"name" sortable:"true"`
	ApiUrl              string     `json:"apiUrl" gorm:"column:api_url" sortable:"true"`
	Status              string     `json:"status" sortable:"true"`
	Enabled             bool       `json:"enabled" sortable:"true"`
	IsEdge              bool       `json:"isEdge" gorm:"column:is_edge;default:false"`
	Hidden              bool       `json:"hidden" gorm:"column:hidden;default:false"`
	LastSeen            *time.Time `json:"lastSeen" gorm:"column:last_seen"`
	LastEdgeTransport   *string    `json:"lastEdgeTransport" gorm:"column:last_edge_transport"`
	AccessToken         *string    `json:"-" gorm:"column:access_token"`
	ApiKeyID            *string    `json:"-" gorm:"column:api_key_id"`
	ParentEnvironmentID *string    `json:"-" gorm:"column:parent_environment_id"`
	SwarmNodeID         *string    `json:"-" gorm:"column:swarm_node_id"`
}

func (Environment) TableName() string { return "environments" }

type EnvironmentStatus string

const (
	EnvironmentStatusOnline  EnvironmentStatus = "online"
	EnvironmentStatusStandby EnvironmentStatus = "standby"
	EnvironmentStatusOffline EnvironmentStatus = "offline"
	EnvironmentStatusError   EnvironmentStatus = "error"
	EnvironmentStatusPending EnvironmentStatus = "pending"
)
