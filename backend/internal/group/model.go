package group

import "github.com/getarcaneapp/arcane/backend/v3/internal/database"

type Group struct {
	database.BaseModel

	Name        string  `json:"name" gorm:"column:name;not null;uniqueIndex" sortable:"true"`
	Description *string `json:"description,omitempty" gorm:"column:description"`
}

func (Group) TableName() string { return "groups" }

type GroupMember struct {
	database.BaseModel

	GroupID string `json:"groupId" gorm:"column:group_id;not null;index"`
	UserID  string `json:"userId" gorm:"column:user_id;not null;index"`
	Source  string `json:"source" gorm:"column:source;not null;default:'manual'"`
}

func (GroupMember) TableName() string { return "group_members" }

type OidcGroupMapping struct {
	database.BaseModel

	ClaimValue string `json:"claimValue" gorm:"column:claim_value;not null;index"`
	GroupID    string `json:"groupId" gorm:"column:group_id;not null;index"`
	Source     string `json:"source" gorm:"column:source;not null;default:'manual'"`
}

func (OidcGroupMapping) TableName() string { return "oidc_group_mappings" }

const (
	MemberSourceManual = "manual"
	MemberSourceOidc   = "oidc"

	MappingSourceManual = "manual"
	MappingSourceEnv    = "env"
)
