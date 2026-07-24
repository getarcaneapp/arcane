package models

import (
	"context"
	"time"

	"github.com/getarcaneapp/arcane/types/v2/user"
)

type User struct {
	BaseModel

	Username               string           `json:"username" sortable:"true"`
	PasswordHash           string           `json:"-" gorm:"column:password_hash"`
	DisplayName            *string          `json:"displayName,omitempty" gorm:"column:display_name" sortable:"true"`
	Email                  *string          `json:"email,omitempty" sortable:"true"`
	OidcSubjectId          *string          `json:"oidcSubjectId,omitempty" gorm:"column:oidc_subject_id"`
	LastLogin              *time.Time       `json:"lastLogin,omitempty" gorm:"column:last_login" sortable:"true"`
	Locale                 *string          `json:"locale,omitempty" gorm:"column:locale"`
	TimeFormat             user.TimeFormat  `json:"timeFormat" gorm:"column:time_format;not null;default:auto"`
	FontSize               *int             `json:"fontSize,omitempty" gorm:"column:font_size"`
	Preferences            user.Preferences `json:"preferences" gorm:"column:preferences;serializer:json"`
	RequiresPasswordChange bool             `json:"requiresPasswordChange" gorm:"column:requires_password_change"`
	IsServiceAccount       bool             `json:"isServiceAccount" gorm:"column:is_service_account;not null;default:false"`

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

// currentUserContextKeyInternal is the context key holding the authenticated
// user model. It lives here (rather than in api/middleware) so that services,
// which cannot import the middleware package, can read the requesting user for
// per-user preferences.
type currentUserContextKeyInternal struct{}

// WithCurrentUser returns a context carrying the authenticated user.
func WithCurrentUser(ctx context.Context, u *User) context.Context {
	return context.WithValue(ctx, currentUserContextKeyInternal{}, u)
}

// CurrentUserFromContext retrieves the authenticated user from the context.
// Returns nil, false on unauthenticated paths (background jobs, agent proxying).
func CurrentUserFromContext(ctx context.Context) (*User, bool) {
	u, ok := ctx.Value(currentUserContextKeyInternal{}).(*User)
	return u, ok
}
