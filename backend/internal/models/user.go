package models

import (
	"time"

	"github.com/getarcaneapp/arcane/types/v2/user"
)

type User struct {
	BaseModel

	Username               string          `json:"username" sortable:"true"`
	PasswordHash           string          `json:"-" gorm:"column:password_hash"`
	DisplayName            *string         `json:"displayName,omitempty" gorm:"column:display_name" sortable:"true"`
	Email                  *string         `json:"email,omitempty" sortable:"true"`
	OidcSubjectId          *string         `json:"oidcSubjectId,omitempty" gorm:"column:oidc_subject_id"`
	LastLogin              *time.Time      `json:"lastLogin,omitempty" gorm:"column:last_login" sortable:"true"`
	Locale                 *string         `json:"locale,omitempty" gorm:"column:locale"`
	TimeFormat             user.TimeFormat `json:"timeFormat" gorm:"column:time_format;not null;default:auto"`
	FontSize               *int            `json:"fontSize,omitempty" gorm:"column:font_size"`
	NavigationLayout       *string         `json:"navigationLayout,omitempty" gorm:"column:navigation_layout"`
	RequiresPasswordChange bool            `json:"requiresPasswordChange" gorm:"column:requires_password_change"`
	IsServiceAccount       bool            `json:"isServiceAccount" gorm:"column:is_service_account;not null;default:false"`

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
