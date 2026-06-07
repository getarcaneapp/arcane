package models

import "time"

const (
	WebhookTargetTypeContainer = "container"
	WebhookTargetTypeProject   = "project"
	WebhookTargetTypeUpdater   = "updater"
	WebhookTargetTypeGitOps    = "gitops"

	WebhookActionTypeUpdate   = "update"
	WebhookActionTypeStart    = "start"
	WebhookActionTypeStop     = "stop"
	WebhookActionTypeRestart  = "restart"
	WebhookActionTypeRedeploy = "redeploy"
	WebhookActionTypeUp       = "up"
	WebhookActionTypeDown     = "down"
	WebhookActionTypeRun      = "run"
	WebhookActionTypeSync     = "sync"
)

type Webhook struct {
	Name            string     `json:"name" gorm:"column:name;not null"`
	TokenHash       string     `json:"-" gorm:"column:token_hash;not null;uniqueIndex"`
	TokenPrefix     string     `json:"tokenPrefix" gorm:"column:token_prefix;not null"`
	TargetType      string     `json:"targetType" gorm:"column:target_type;not null"`
	ActionType      string     `json:"actionType" gorm:"column:action_type;not null;default:''"`
	TargetID        string     `json:"targetId" gorm:"column:target_id;not null"`
	TargetRef       string     `json:"-" gorm:"column:target_ref;not null;default:''"`
	EnvironmentID   string     `json:"environmentId" gorm:"column:environment_id;not null;default:''"`
	Enabled         bool       `json:"enabled" gorm:"column:enabled;not null;default:true"`
	LastTriggeredAt *time.Time `json:"lastTriggeredAt,omitempty" gorm:"column:last_triggered_at"`
	BaseModel
}

func (Webhook) TableName() string {
	return "webhooks"
}
