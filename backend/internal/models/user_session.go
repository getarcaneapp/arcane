package models

import "time"

type UserSession struct {
	BaseModel
	UserID           string     `json:"userId" gorm:"column:user_id;not null;index"`
	User             *User      `json:"user,omitempty" gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE"`
	RefreshTokenHash string     `json:"-" gorm:"column:refresh_token_hash;not null;uniqueIndex"`
	UserAgent        *string    `json:"userAgent,omitempty" gorm:"column:user_agent"`
	IPAddress        *string    `json:"ipAddress,omitempty" gorm:"column:ip_address"`
	LastUsedAt       time.Time  `json:"lastUsedAt" gorm:"column:last_used_at;not null"`
	ExpiresAt        time.Time  `json:"expiresAt" gorm:"column:expires_at;not null;index"`
	RevokedAt        *time.Time `json:"revokedAt,omitempty" gorm:"column:revoked_at"`
}

func (UserSession) TableName() string {
	return "user_sessions"
}
