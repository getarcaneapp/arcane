package federated

import (
	"time"

	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/environment"
	"getarcane.dev/arcane/v3/internal/role"
	"getarcane.dev/arcane/v3/internal/user"
)

type (
	FederatedCredential struct {
		database.BaseModel

		Description   *string                  `json:"description,omitempty" gorm:"column:description"`
		EnvironmentID *string                  `json:"environmentId,omitempty" gorm:"column:environment_id;index"`
		LastUsedAt    *time.Time               `json:"lastUsedAt,omitempty" gorm:"column:last_used_at" sortable:"true"`
		ExpiresAt     *time.Time               `json:"expiresAt,omitempty" gorm:"column:expires_at" sortable:"true"`
		IdentityUser  *user.User               `json:"identityUser,omitempty" gorm:"foreignKey:IdentityUserID;constraint:OnDelete:CASCADE"`
		Role          *role.Role               `json:"role,omitempty" gorm:"foreignKey:RoleID;constraint:OnDelete:RESTRICT"`
		Environment   *environment.Environment `json:"environment,omitempty" gorm:"foreignKey:EnvironmentID;constraint:OnDelete:SET NULL"`

		Name            string               `json:"name" gorm:"column:name;not null" sortable:"true"`
		IssuerURL       string               `json:"issuerUrl" gorm:"column:issuer_url;not null;index" sortable:"true"`
		SubjectClaim    string               `json:"subjectClaim" gorm:"column:subject_claim;not null;default:'sub'"`
		SubjectMatch    string               `json:"subjectMatch" gorm:"column:subject_match;not null"`
		MatchType       string               `json:"matchType" gorm:"column:match_type;not null;default:'exact'"`
		RoleID          string               `json:"roleId" gorm:"column:role_id;not null;index"`
		IdentityUserID  string               `json:"identityUserId" gorm:"column:identity_user_id;not null;index"`
		Audiences       database.StringSlice `json:"audiences" gorm:"column:audiences;type:text;not null"`
		TokenTTLSeconds int                  `json:"tokenTtlSeconds" gorm:"column:token_ttl_seconds;not null;default:900"`
		Enabled         bool                 `json:"enabled" gorm:"column:enabled;not null;default:false;index" sortable:"true"`
	}

	FederatedTokenReplay struct {
		database.BaseModel

		ExpiresAt time.Time `json:"expiresAt" gorm:"column:expires_at;not null;index"`

		TokenHash string `json:"-" gorm:"column:token_hash;not null;uniqueIndex"`
		IssuerURL string `json:"issuerUrl" gorm:"column:issuer_url;not null;index"`
	}
)

const (
	FederatedCredentialMatchExact = "exact"
	FederatedCredentialMatchGlob  = "glob"
)

func (FederatedCredential) TableName() string {
	return "federated_credentials"
}

func (FederatedTokenReplay) TableName() string {
	return "federated_token_replays"
}
