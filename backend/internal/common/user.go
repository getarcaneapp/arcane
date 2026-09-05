package common

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"

	"context"
	"time"

	"github.com/getarcaneapp/arcane/types/v2/user"
)

type User struct {
	database.BaseModel

	Username               string           `json:"username" sortable:"true" unorm:"nfc" trim:"true" minLength:"1"`
	PasswordHash           string           `json:"-" gorm:"column:password_hash"`
	DisplayName            *string          `json:"displayName,omitempty" gorm:"column:display_name" sortable:"true" unorm:"nfc" trim:"true"`
	Email                  *string          `json:"email,omitempty" sortable:"true" unorm:"nfc" trim:"true"`
	OidcSubjectId          *string          `json:"oidcSubjectId,omitempty" gorm:"column:oidc_subject_id"`
	LastLogin              *time.Time       `json:"lastLogin,omitempty" gorm:"column:last_login" sortable:"true"`
	Locale                 *string          `json:"locale,omitempty" gorm:"column:locale"`
	TimeFormat             user.TimeFormat  `json:"timeFormat" gorm:"column:time_format;not null;default:auto"`
	FontSize               *int             `json:"fontSize,omitempty" gorm:"column:font_size"`
	Preferences            user.Preferences `json:"preferences" gorm:"column:preferences;serializer:json"`
	RequiresPasswordChange bool             `json:"requiresPasswordChange" gorm:"column:requires_password_change"`
	IsServiceAccount       bool             `json:"isServiceAccount" gorm:"column:is_service_account;not null;default:false"`
	PasskeyMFAEnabled      bool             `json:"passkeyMfaEnabled" gorm:"column:passkey_mfa_enabled;not null;default:false"`

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

// CurrentUserContextKey is the context key holding the authenticated user
// model, set via context.WithValue(ctx, common.CurrentUserContextKey{}, user).
// It lives here (rather than in transport middleware) so that services, which cannot
// import the middleware package, can read the requesting user for per-user
// preferences.
type CurrentUserContextKey struct{}

// CurrentUserFromContext retrieves the authenticated user from the context.
// Returns nil, false on unauthenticated paths (background jobs, agent proxying).
func CurrentUserFromContext(ctx context.Context) (*User, bool) {
	u, ok := ctx.Value(CurrentUserContextKey{}).(*User)
	return u, ok
}

// SystemUser is the actor recorded for work Arcane performs on its own behalf —
// scheduled jobs, startup reconciliation, GitOps syncs — rather than in response
// to a signed-in user.
var SystemUser = User{
	Username: "System",
}
