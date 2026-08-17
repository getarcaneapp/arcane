package federated

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/role"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"

	"time"
)

const (
	FederatedCredentialMatchExact = "exact"
	FederatedCredentialMatchGlob  = "glob"
)

type FederatedCredential struct {
	database.BaseModel

	Name            string                   `json:"name" gorm:"column:name;not null" sortable:"true"`
	Description     *string                  `json:"description,omitempty" gorm:"column:description"`
	Enabled         bool                     `json:"enabled" gorm:"column:enabled;not null;default:false;index" sortable:"true"`
	IssuerURL       string                   `json:"issuerUrl" gorm:"column:issuer_url;not null;index" sortable:"true"`
	Audiences       database.StringSlice     `json:"audiences" gorm:"column:audiences;type:text;not null"`
	SubjectClaim    string                   `json:"subjectClaim" gorm:"column:subject_claim;not null;default:'sub'"`
	SubjectMatch    string                   `json:"subjectMatch" gorm:"column:subject_match;not null"`
	MatchType       string                   `json:"matchType" gorm:"column:match_type;not null;default:'exact'"`
	RoleID          string                   `json:"roleId" gorm:"column:role_id;not null;index"`
	EnvironmentID   *string                  `json:"environmentId,omitempty" gorm:"column:environment_id;index"`
	IdentityUserID  string                   `json:"identityUserId" gorm:"column:identity_user_id;not null;index"`
	TokenTTLSeconds int                      `json:"tokenTtlSeconds" gorm:"column:token_ttl_seconds;not null;default:900"`
	LastUsedAt      *time.Time               `json:"lastUsedAt,omitempty" gorm:"column:last_used_at" sortable:"true"`
	ExpiresAt       *time.Time               `json:"expiresAt,omitempty" gorm:"column:expires_at" sortable:"true"`
	IdentityUser    *common.User             `json:"identityUser,omitempty" gorm:"foreignKey:IdentityUserID;constraint:OnDelete:CASCADE"`
	Role            *role.Role               `json:"role,omitempty" gorm:"foreignKey:RoleID;constraint:OnDelete:RESTRICT"`
	Environment     *environment.Environment `json:"environment,omitempty" gorm:"foreignKey:EnvironmentID;constraint:OnDelete:SET NULL"`
}

func (FederatedCredential) TableName() string {
	return "federated_credentials"
}

type FederatedTokenReplay struct {
	database.BaseModel

	TokenHash string    `json:"-" gorm:"column:token_hash;not null;uniqueIndex"`
	IssuerURL string    `json:"issuerUrl" gorm:"column:issuer_url;not null;index"`
	ExpiresAt time.Time `json:"expiresAt" gorm:"column:expires_at;not null;index"`
}

func (FederatedTokenReplay) TableName() string {
	return "federated_token_replays"
}
