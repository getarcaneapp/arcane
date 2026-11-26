package group

import "getarcane.dev/arcane/v3/internal/database"

type (
	Group struct {
		database.BaseModel

		Description *string `json:"description,omitempty" gorm:"column:description"`
		Name        string  `json:"name" gorm:"column:name;not null;uniqueIndex" sortable:"true"`
	}

	GroupMember struct {
		database.BaseModel

		GroupID string `json:"groupId" gorm:"column:group_id;not null;index"`
		UserID  string `json:"userId" gorm:"column:user_id;not null;index"`
		Source  string `json:"source" gorm:"column:source;not null;default:'manual'"`
	}

	GroupRoleAssignment struct {
		database.BaseModel

		EnvironmentID *string `json:"environmentId,omitempty" gorm:"column:environment_id;index"`
		ResourceType  *string `json:"resourceType,omitempty" gorm:"column:resource_type"`
		ResourceID    *string `json:"resourceId,omitempty" gorm:"column:resource_id"`
		GroupID       string  `json:"groupId" gorm:"column:group_id;not null;index"`
		RoleID        string  `json:"roleId" gorm:"column:role_id;not null;index"`
		Source        string  `json:"source" gorm:"column:source;not null;default:'manual'"`
	}
)

const (
	SourceManual = "manual"
	SourceOidc   = "oidc"
	SourceLdap   = "ldap"
)

func (Group) TableName() string { return "groups" }

func (GroupMember) TableName() string { return "group_members" }

func (GroupRoleAssignment) TableName() string { return "group_role_assignments" }
