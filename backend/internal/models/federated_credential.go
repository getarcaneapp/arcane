package models

import "time"

const (
	FederatedCredentialMatchExact = "exact"
	FederatedCredentialMatchGlob  = "glob"
)

type FederatedCredential struct {
	BaseModel

	Name            string       `json:"name" gorm:"column:name;not null" sortable:"true"`
	Description     *string      `json:"description,omitempty" gorm:"column:description"`
	Enabled         bool         `json:"enabled" gorm:"column:enabled;not null;default:false;index" sortable:"true"`
	IssuerURL       string       `json:"issuerUrl" gorm:"column:issuer_url;not null;index" sortable:"true"`
	Audiences       StringSlice  `json:"audiences" gorm:"column:audiences;type:text;not null"`
	SubjectClaim    string       `json:"subjectClaim" gorm:"column:subject_claim;not null;default:'sub'"`
	SubjectMatch    string       `json:"subjectMatch" gorm:"column:subject_match;not null"`
	MatchType       string       `json:"matchType" gorm:"column:match_type;not null;default:'exact'"`
	RoleID          string       `json:"roleId" gorm:"column:role_id;not null;index"`
	EnvironmentID   *string      `json:"environmentId,omitempty" gorm:"column:environment_id;index"`
	IdentityUserID  string       `json:"identityUserId" gorm:"column:identity_user_id;not null;index"`
	TokenTTLSeconds int          `json:"tokenTtlSeconds" gorm:"column:token_ttl_seconds;not null;default:900"`
	LastUsedAt      *time.Time   `json:"lastUsedAt,omitempty" gorm:"column:last_used_at" sortable:"true"`
	ExpiresAt       *time.Time   `json:"expiresAt,omitempty" gorm:"column:expires_at" sortable:"true"`
	IdentityUser    *User        `json:"identityUser,omitempty" gorm:"foreignKey:IdentityUserID;constraint:OnDelete:CASCADE"`
	Role            *Role        `json:"role,omitempty" gorm:"foreignKey:RoleID;constraint:OnDelete:RESTRICT"`
	Environment     *Environment `json:"environment,omitempty" gorm:"foreignKey:EnvironmentID;constraint:OnDelete:SET NULL"`
}

func (FederatedCredential) TableName() string {
	return "federated_credentials"
}
