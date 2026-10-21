package user

import (
	"time"

	"github.com/getarcaneapp/arcane/types/v3"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
)

type User struct {
	database.BaseModel

	Username               string                `json:"username" sortable:"true"`
	PasswordHash           string                `json:"-" gorm:"column:password_hash"`
	DisplayName            *string               `json:"displayName,omitempty" gorm:"column:display_name" sortable:"true" unorm:"nfc" trim:"true"`
	Email                  *string               `json:"email,omitempty" sortable:"true"`
	OidcSubjectId          *string               `json:"oidcSubjectId,omitempty" gorm:"column:oidc_subject_id"`
	LastLogin              *time.Time            `json:"lastLogin,omitempty" gorm:"column:last_login" sortable:"true"`
	Locale                 *string               `json:"locale,omitempty" gorm:"column:locale"`
	TimeFormat             types.TimeFormat      `json:"timeFormat" gorm:"column:time_format;not null;default:auto"`
	FontSize               *int                  `json:"fontSize,omitempty" gorm:"column:font_size"`
	Preferences            types.UserPreferences `json:"preferences" gorm:"column:preferences;serializer:json"`
	RequiresPasswordChange bool                  `json:"requiresPasswordChange" gorm:"column:requires_password_change"`
	IsServiceAccount       bool                  `json:"isServiceAccount" gorm:"column:is_service_account;not null;default:false"`
	PasskeyMFAEnabled      bool                  `json:"passkeyMfaEnabled" gorm:"column:passkey_mfa_enabled;not null;default:false"`

	// Avatar metadata
	HasAvatar bool `json:"hasAvatar" gorm:"column:has_avatar;not null;default:false"`

	// OIDC provider tokens
	OidcAccessToken          *string    `json:"-" gorm:"type:text"`
	OidcRefreshToken         *string    `json:"-" gorm:"type:text"`
	OidcAccessTokenExpiresAt *time.Time `json:"-"`
}

func (User) TableName() string {
	return "users"
}

// UserAvatar represents the raw profile picture data for a user.
// Stored separately from the main User struct to prevent
// loading up to 2MB of binary data on every user query.
type UserAvatar struct {
	UserID   string `json:"userId" gorm:"column:user_id;primaryKey"`
	Data     []byte `json:"-" gorm:"column:data;type:blob;not null"`
	MimeType string `json:"mimeType" gorm:"column:mime_type;not null"`
}

func (UserAvatar) TableName() string {
	return "user_avatars"
}

// Actor returns the identity and preferences used by requests and background work.
func (u *User) Actor() *types.Actor {
	if u == nil {
		return nil
	}
	return &types.Actor{ID: u.ID, Username: u.Username, DisplayName: u.DisplayName, Preferences: u.Preferences}
}
