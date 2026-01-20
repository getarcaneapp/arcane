package models

import "time"

type Environment struct {
	Name        string     `json:"name" sortable:"true"`
	ApiUrl      string     `json:"apiUrl" gorm:"column:api_url" sortable:"true"`
	Status      string     `json:"status" sortable:"true"`
	Enabled     bool       `json:"enabled" sortable:"true"`
	IsEdge      bool       `json:"isEdge" gorm:"column:is_edge;default:false"`
	LastSeen    *time.Time `json:"lastSeen" gorm:"column:last_seen"`
	AccessToken *string    `json:"-" gorm:"column:access_token"`
	ApiKeyID    *string    `json:"-" gorm:"column:api_key_id"`

	BaseModel
}

func (Environment) TableName() string { return "environments" }

type EnvironmentStatus string

const (
	EnvironmentStatusOnline  EnvironmentStatus = "online"
	EnvironmentStatusOffline EnvironmentStatus = "offline"
	EnvironmentStatusError   EnvironmentStatus = "error"
	EnvironmentStatusPending EnvironmentStatus = "pending"
)
