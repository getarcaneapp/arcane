package session

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"

	"time"
)

const (
	UserSessionSourceLocal     = "local"
	UserSessionSourceOidc      = "oidc"
	UserSessionSourceFederated = "federated"
	UserSessionSourcePasskey   = "passkey"
)

type UserSession struct {
	database.BaseModel

	UserID           string       `json:"userId" gorm:"column:user_id;not null;index"`
	User             *common.User `json:"user,omitempty" gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE"`
	RefreshTokenHash string       `json:"-" gorm:"column:refresh_token_hash;not null;uniqueIndex"`
	UserAgent        *string      `json:"userAgent,omitempty" gorm:"column:user_agent"`
	IPAddress        *string      `json:"ipAddress,omitempty" gorm:"column:ip_address"`
	Source           string       `json:"source,omitempty" gorm:"column:source"`
	// FederatedCredentialID links federated sessions to their credential row;
	// the FK (ON DELETE SET NULL) lives in the SQL migrations.
	FederatedCredentialID *string    `json:"federatedCredentialId,omitempty" gorm:"column:federated_credential_id;index"`
	LastUsedAt            time.Time  `json:"lastUsedAt" gorm:"column:last_used_at;not null"`
	ExpiresAt             time.Time  `json:"expiresAt" gorm:"column:expires_at;not null;index"`
	RevokedAt             *time.Time `json:"revokedAt,omitempty" gorm:"column:revoked_at"`
	MFAMethod             *string    `json:"mfaMethod,omitempty" gorm:"column:mfa_method"`
	MFAVerifiedAt         *time.Time `json:"mfaVerifiedAt,omitempty" gorm:"column:mfa_verified_at"`
}

func (UserSession) TableName() string {
	return "user_sessions"
}

const (
	// PasskeyMFAMethod and RecoveryCodeMFAMethod name the second factor a login
	// completed with, recorded on the session for auditing.
	PasskeyMFAMethod      = "passkey"
	RecoveryCodeMFAMethod = "recovery_code"
)
