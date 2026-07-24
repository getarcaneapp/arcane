package user

// TimeFormat controls how absolute times are displayed for a user.
type TimeFormat string

const (
	TimeFormatAuto   TimeFormat = "auto"
	TimeFormat12Hour TimeFormat = "12h"
	TimeFormat24Hour TimeFormat = "24h"
)

// Preferences holds the per-user display/UI preferences. Every field is a
// pointer so that "unset" (nil) is distinguishable from an explicit value and
// falls back to the frontend default. It doubles as the GORM serializer target
// on the user model and as the wire DTO.
type Preferences struct {
	ThemeMode                  *string `json:"themeMode,omitempty" enum:"light,dark,system" doc:"Light/dark mode preference"`
	ApplicationTheme           *string `json:"applicationTheme,omitempty" enum:"default,graphite,ocean,amber,github,nord,everforest,rosepine" doc:"Application theme preset"`
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
}

// CreateUser represents the request body for creating a new user.
// Role assignments are managed separately via PUT /users/{userId}/role-assignments.
type CreateUser struct {
	Username    string      `json:"username" minLength:"1" maxLength:"255" doc:"Username of the user" example:"johndoe"`
	Password    string      `json:"password" minLength:"8" doc:"Password of the user"`
	DisplayName *string     `json:"displayName,omitempty" maxLength:"255" doc:"Display name of the user" example:"John Doe"`
	Email       *string     `json:"email,omitempty" doc:"Email address of the user" example:"john@example.com"`
	Locale      *string     `json:"locale,omitempty" doc:"Locale preference of the user" example:"en-US"`
	TimeFormat  *TimeFormat `json:"timeFormat,omitempty" enum:"auto,12h,24h" doc:"Preferred time display format" example:"auto"`
}

// UpdateUser represents the request body for updating a user.
// Role assignments are managed separately via PUT /users/{userId}/role-assignments.
type UpdateUser struct {
	Username    *string     `json:"username,omitempty" minLength:"1" maxLength:"255" doc:"Username of the user"`
	DisplayName *string     `json:"displayName,omitempty" maxLength:"255" doc:"Display name of the user"`
	Email       *string     `json:"email,omitempty" doc:"Email address of the user"`
	Locale      *string     `json:"locale,omitempty" doc:"Locale preference of the user"`
	TimeFormat  *TimeFormat `json:"timeFormat,omitempty" enum:"auto,12h,24h" doc:"Preferred time display format"`
	Password    *string     `json:"password,omitempty" minLength:"8" doc:"New password for the user"`
}

// RoleAssignmentSummary is a compact form of a user's role assignment used
// inside the User payload (so the frontend can render the assignments table
// without a separate request).
type RoleAssignmentSummary struct {
	RoleID        string  `json:"roleId" doc:"Role ID granted by this assignment"`
	EnvironmentID *string `json:"environmentId,omitempty" doc:"Environment ID this assignment is scoped to; omit for a global assignment"`
	Source        string  `json:"source" doc:"How the assignment was created" enum:"manual,oidc"`
}

// User represents a user in API responses.
type User struct {
	FontSize               *int                    `json:"fontSize,omitempty" minimum:"12" maximum:"20" doc:"Preferred root UI font size in px" example:"14"`
	DisplayName            *string                 `json:"displayName,omitempty" doc:"Display name of the user" example:"John Doe"`
	Email                  *string                 `json:"email,omitempty" doc:"Email address of the user" example:"john@example.com"`
	AvatarURL              *string                 `json:"avatarUrl,omitempty" doc:"URL to the user's custom avatar image; omitted when using the default profile picture"`
	OidcSubjectId          *string                 `json:"oidcSubjectId,omitempty" doc:"OIDC subject identifier for SSO users"`
	Locale                 *string                 `json:"locale,omitempty" doc:"Locale preference of the user" example:"en-US"`
	PermissionsByEnv       map[string][]string     `json:"permissionsByEnv" doc:"Permissions the user effectively holds, keyed by environment ID. The 'global' key holds permissions that apply across every environment (and to org-level endpoints)."`
	Preferences            Preferences             `json:"preferences" doc:"Personal display and UI preferences"`
	ID                     string                  `json:"id" doc:"Unique identifier of the user" example:"550e8400-e29b-41d4-a716-446655440000"`
	Username               string                  `json:"username" doc:"Username of the user" example:"johndoe"`
	TimeFormat             TimeFormat              `json:"timeFormat" enum:"auto,12h,24h" doc:"Preferred time display format" example:"auto"`
	CreatedAt              string                  `json:"createdAt,omitempty" doc:"Date and time when the user was created"`
	UpdatedAt              string                  `json:"updatedAt,omitempty" doc:"Date and time when the user was last updated"`
	RoleAssignments        []RoleAssignmentSummary `json:"roleAssignments" doc:"Role assignments held by the user"`
	IsGlobalAdmin          bool                    `json:"isGlobalAdmin" doc:"Whether the user effectively holds global administrator access"`
	CanDelete              bool                    `json:"canDelete" doc:"Whether the user can currently be deleted"`
	RequiresPasswordChange bool                    `json:"requiresPasswordChange" doc:"Whether the user must change their password"`
}
