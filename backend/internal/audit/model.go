package audit

import "time"

type AuditLog struct {
	ID            string         `gorm:"column:id;primaryKey;type:text"`
	CreatedAt     time.Time      `gorm:"column:created_at;not null;index" sortable:"true"`
	ActorType     string         `gorm:"column:actor_type;not null"`
	ActorUserID   *string        `gorm:"column:actor_user_id"`
	ActorUsername string         `gorm:"column:actor_username;not null;default:''" sortable:"true"`
	ApiKeyID      *string        `gorm:"column:api_key_id"`
	Action        string         `gorm:"column:action;not null" sortable:"true"`
	Outcome       string         `gorm:"column:outcome;not null;default:'success'" sortable:"true"`
	TargetType    string         `gorm:"column:target_type;not null;default:''" sortable:"true"`
	TargetID      string         `gorm:"column:target_id;not null;default:''"`
	TargetName    string         `gorm:"column:target_name;not null;default:''"`
	EnvironmentID *string        `gorm:"column:environment_id"`
	Permission    string         `gorm:"column:permission;not null;default:''"`
	RequestMethod string         `gorm:"column:request_method;not null;default:''"`
	RequestPath   string         `gorm:"column:request_path;not null;default:''"`
	RemoteAddr    string         `gorm:"column:remote_addr;not null;default:''"`
	UserAgent     string         `gorm:"column:user_agent;not null;default:''"`
	Before        any            `gorm:"column:before;serializer:json"`
	After         any            `gorm:"column:after;serializer:json"`
	Metadata      map[string]any `gorm:"column:metadata;serializer:json"`
}

func (AuditLog) TableName() string { return "audit_logs" }
