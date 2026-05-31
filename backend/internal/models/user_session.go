package models

import "time"

const (
	UserSessionSourceLocal     = "local"
	UserSessionSourceOidc      = "oidc"
	UserSessionSourceFederated = "federated"
)

type UserSession struct {
	BaseModel

	UserID                string               `json:"userId" gorm:"column:user_id;not null;index"`
	User                  *User                `json:"user,omitempty" gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE"`
	RefreshTokenHash      string               `json:"-" gorm:"column:refresh_token_hash;not null;uniqueIndex"`
	UserAgent             *string              `json:"userAgent,omitempty" gorm:"column:user_agent"`
	IPAddress             *string              `json:"ipAddress,omitempty" gorm:"column:ip_address"`
	Source                string               `json:"source,omitempty" gorm:"column:source"`
	FederatedCredentialID *string              `json:"federatedCredentialId,omitempty" gorm:"column:federated_credential_id;index"`
	FederatedCredential   *FederatedCredential `json:"federatedCredential,omitempty" gorm:"foreignKey:FederatedCredentialID;constraint:OnDelete:SET NULL"`
	LastUsedAt            time.Time            `json:"lastUsedAt" gorm:"column:last_used_at;not null"`
	ExpiresAt             time.Time            `json:"expiresAt" gorm:"column:expires_at;not null;index"`
	RevokedAt             *time.Time           `json:"revokedAt,omitempty" gorm:"column:revoked_at"`
}

func (UserSession) TableName() string {
	return "user_sessions"
}
