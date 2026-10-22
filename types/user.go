package types

// TimeFormat controls how absolute times are displayed for a user.
type TimeFormat string

const (
	TimeFormatAuto   TimeFormat = "auto"
	TimeFormat12Hour TimeFormat = "12h"
	TimeFormat24Hour TimeFormat = "24h"
)

// UserPreferences holds the per-user display/UI preferences. Every field is a
// pointer so that "unset" (nil) is distinguishable from an explicit value and
// falls back to the frontend default. It doubles as the GORM serializer target
// on the user model and as the wire DTO.
type UserPreferences struct {
	ThemeMode                  *string `json:"themeMode,omitempty" enum:"light,dark,system" doc:"Light/dark mode preference"`
	ApplicationTheme           *string `json:"applicationTheme,omitempty" enum:"default,graphite,carbon,ocean,amber,github,nord,everforest,rosepine" doc:"Application theme preset"`
	AccentColor                *string `json:"accentColor,omitempty" doc:"Primary accent color, or 'default'"`
	IconCatalog                *string `json:"iconCatalog,omitempty" enum:"selfhst,dashboard-icons" doc:"Catalog used to resolve project and container icon slugs"`
	OledMode                   *bool   `json:"oledMode,omitempty" doc:"Use true-black backgrounds in dark mode"`
	GlassEffectsEnabled        *bool   `json:"glassEffectsEnabled,omitempty" doc:"Apply blur and glass effects to UI surfaces"`
	AnimationsEnabled          *bool   `json:"animationsEnabled,omitempty" doc:"Enable decorative interface animations"`
	SidebarHoverExpansion      *bool   `json:"sidebarHoverExpansion,omitempty" doc:"Expand the desktop sidebar on hover"`
	KeyboardShortcutsEnabled   *bool   `json:"keyboardShortcutsEnabled,omitempty" doc:"Enable keyboard shortcuts and shortcut hints"`
	MobileNavigationMode       *string `json:"mobileNavigationMode,omitempty" enum:"floating,docked" doc:"Mobile navigation style"`
	MobileNavigationShowLabels *bool   `json:"mobileNavigationShowLabels,omitempty" doc:"Show text labels in mobile navigation"`
	DefaultLandingPage         *string `json:"defaultLandingPage,omitempty" doc:"Route opened after signing in"`
	DefaultProjectEditorLayout *string `json:"defaultProjectEditorLayout,omitempty" enum:"auto,classic,tree" doc:"Initial layout of the project editor; auto keeps workspace detection"`
	AllEnvironmentsPreview     *bool   `json:"allEnvironmentsPreview,omitempty" doc:"Preview: resource lists show and act on every enabled environment"`
	// A pointer so the merge-only preferences update replaces the whole map.
	EnvironmentColors     *map[string]string `json:"environmentColors,omitempty" doc:"Environment label color by environment ID"`
	EnvironmentLabelStyle *string            `json:"environmentLabelStyle,omitempty" enum:"name,dot" doc:"How resource lists label environments"`
}

// User represents a user in API responses.
type User struct {
	FontSize               *int                 `json:"fontSize,omitempty" minimum:"12" maximum:"20" doc:"Preferred root UI font size in px" example:"14"`
	DisplayName            *string              `json:"displayName,omitempty" maxLength:"255" doc:"Display name of the user" example:"John Doe" unorm:"nfc" trim:"true"`
	Email                  *string              `json:"email,omitempty" doc:"Email address of the user" example:"john@example.com"`
	AvatarURL              *string              `json:"avatarUrl,omitempty" readOnly:"true" doc:"URL to the user's custom avatar image; omitted when using the default profile picture"`
	OidcSubjectId          *string              `json:"oidcSubjectId,omitempty" readOnly:"true" doc:"OIDC subject identifier for SSO users"`
	Locale                 *string              `json:"locale,omitempty" doc:"Locale preference of the user" example:"en-US"`
	LastLogin              *string              `json:"lastLogin,omitempty" readOnly:"true" doc:"Date and time of the user's most recent sign-in; omitted when the user has never signed in"`
	PermissionsByEnv       map[string][]string  `json:"permissionsByEnv" readOnly:"true" doc:"Permissions by environment ID; 'global' applies to all environments and organization endpoints"`
	ScopedPermissionsByEnv map[string][]string  `json:"scopedPermissionsByEnv" readOnly:"true" doc:"Subset of permissionsByEnv limited by resource selectors"`
	Preferences            UserPreferences      `json:"preferences" required:"false" doc:"Personal display and UI preferences"`
	ID                     string               `json:"id" readOnly:"true" doc:"Unique identifier of the user" example:"550e8400-e29b-41d4-a716-446655440000"`
	Username               string               `json:"username" required:"false" minLength:"1" maxLength:"255" pattern:"^[^@]+$" patternDescription:"No @ character" doc:"Username (no @)"`
	Password               string               `json:"password,omitempty" writeOnly:"true" minLength:"8" doc:"Password of the user; never returned"`
	TimeFormat             TimeFormat           `json:"timeFormat" required:"false" enum:"auto,12h,24h" doc:"Preferred time display format" example:"auto"`
	CreatedAt              string               `json:"createdAt,omitempty" readOnly:"true" doc:"Date and time when the user was created"`
	UpdatedAt              string               `json:"updatedAt,omitempty" readOnly:"true" doc:"Date and time when the user was last updated"`
	Bindings               []RoleBindingSummary `json:"bindings" readOnly:"true" doc:"Role bindings held by the user directly or through groups"`
	Groups                 []GroupRef           `json:"groups" readOnly:"true" doc:"Groups the user belongs to"`
	IsGlobalAdmin          bool                 `json:"isGlobalAdmin" readOnly:"true" doc:"Whether the user effectively holds global administrator access"`
	CanDelete              bool                 `json:"canDelete" readOnly:"true" doc:"Whether the user can currently be deleted"`
	RequiresPasswordChange bool                 `json:"requiresPasswordChange" readOnly:"true" doc:"Whether the user must change their password"`
}

// Actor identifies a user performing domain work without exposing persistence data.
type Actor struct {
	ID          string          `json:"id"`
	Username    string          `json:"username"`
	DisplayName *string         `json:"displayName,omitempty"`
	Preferences UserPreferences `json:"preferences"`
}

// ActorProvider supplies an actor from an authenticated user model.
type ActorProvider interface{ Actor() *Actor }

// CurrentUserContextKey holds the authenticated user or actor in a request context.
type CurrentUserContextKey struct{}

// SystemUser identifies work initiated by Arcane itself.
var SystemUser = Actor{Username: "System"}
