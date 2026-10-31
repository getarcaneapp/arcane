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
	ApplicationTheme           *string `json:"applicationTheme,omitempty" pattern:"^[a-z0-9-]{1,48}$" doc:"Theme preset or custom theme ID"`
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
	EditorTabWidth             *int    `json:"editorTabWidth,omitempty" enum:"2,4,8" doc:"Spaces per indentation level in code editors and when formatting documents"`
	ToastPosition              *string `json:"toastPosition,omitempty" enum:"auto,top-left,top-right,top-center,bottom-left,bottom-right,bottom-center" doc:"Toast notification position"`
	// A pointer so the merge-only preferences update replaces the whole map.
	EnvironmentColors     *map[string]string       `json:"environmentColors,omitempty" doc:"Environment label color by environment ID"`
	EnvironmentLabelStyle *string                  `json:"environmentLabelStyle,omitempty" enum:"name,dot" doc:"How resource lists label environments"`
	DashboardLayout       *[]DashboardWidgetLayout `json:"dashboardLayout,omitempty" doc:"Dashboard widgets with their grid position, size, and visibility"`
	CustomThemes          *[]CustomTheme           `json:"customThemes,omitempty" maxItems:"20" doc:"Imported custom color themes"`
}

type CustomTheme struct {
	ID          string       `json:"id" doc:"Custom theme identifier, prefixed with custom-" example:"custom-solarized"`
	Name        string       `json:"name" doc:"Display name of the custom theme" example:"Solarized"`
	Description string       `json:"description,omitempty" doc:"Optional description of the custom theme"`
	Light       ThemePalette `json:"light" doc:"Color tokens used in light mode"`
	Dark        ThemePalette `json:"dark" doc:"Color tokens used in dark mode"`
}

type DashboardWidgetLayout struct {
	ID     string `json:"id" doc:"Dashboard widget identifier"`
	X      int    `json:"x" doc:"Column where the widget starts in the 12-column dashboard grid"`
	Y      int    `json:"y" doc:"Row where the widget starts in the dashboard grid"`
	W      int    `json:"w" doc:"Widget width in columns"`
	H      int    `json:"h" doc:"Widget height in rows"`
	Hidden bool   `json:"hidden,omitempty" doc:"Whether the widget is hidden"`
}

type ThemePalette struct {
	Background               string `json:"background" required:"true"`
	Foreground               string `json:"foreground" required:"true"`
	Card                     string `json:"card" required:"true"`
	CardForeground           string `json:"card-foreground" required:"true"`
	Popover                  string `json:"popover" required:"true"`
	PopoverForeground        string `json:"popover-foreground" required:"true"`
	Primary                  string `json:"primary" required:"true"`
	PrimaryForeground        string `json:"primary-foreground" required:"true"`
	Secondary                string `json:"secondary" required:"true"`
	SecondaryForeground      string `json:"secondary-foreground" required:"true"`
	Muted                    string `json:"muted" required:"true"`
	MutedForeground          string `json:"muted-foreground" required:"true"`
	Accent                   string `json:"accent" required:"true"`
	AccentForeground         string `json:"accent-foreground" required:"true"`
	Border                   string `json:"border" required:"true"`
	Input                    string `json:"input" required:"true"`
	Ring                     string `json:"ring" required:"true"`
	Sidebar                  string `json:"sidebar" required:"true"`
	SidebarForeground        string `json:"sidebar-foreground" required:"true"`
	SidebarPrimary           string `json:"sidebar-primary" required:"true"`
	SidebarPrimaryForeground string `json:"sidebar-primary-foreground" required:"true"`
	SidebarAccent            string `json:"sidebar-accent" required:"true"`
	SidebarAccentForeground  string `json:"sidebar-accent-foreground" required:"true"`
	SidebarBorder            string `json:"sidebar-border" required:"true"`
	SidebarRing              string `json:"sidebar-ring" required:"true"`
	Surface                  string `json:"surface" required:"true"`
	GlassShadowColor         string `json:"glass-shadow-color" required:"true"`
	SyntaxComment            string `json:"syntax-comment" required:"true"`
	SyntaxKeyword            string `json:"syntax-keyword" required:"true"`
	SyntaxProperty           string `json:"syntax-property" required:"true"`
	SyntaxString             string `json:"syntax-string" required:"true"`
	SyntaxNumber             string `json:"syntax-number" required:"true"`
	SyntaxOperator           string `json:"syntax-operator" required:"true"`
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
	DisplayName            *string                 `json:"displayName,omitempty" maxLength:"255" doc:"Display name of the user" example:"John Doe" unorm:"nfc" trim:"true"`
	Email                  *string                 `json:"email,omitempty" doc:"Email address of the user" example:"john@example.com"`
	AvatarURL              *string                 `json:"avatarUrl,omitempty" readOnly:"true" doc:"URL to the user's custom avatar image; omitted when using the default profile picture"`
	OidcSubjectId          *string                 `json:"oidcSubjectId,omitempty" readOnly:"true" doc:"OIDC subject identifier for SSO users"`
	Locale                 *string                 `json:"locale,omitempty" doc:"Locale preference of the user" example:"en-US"`
	LastLogin              *string                 `json:"lastLogin,omitempty" readOnly:"true" doc:"Date and time of the user's most recent sign-in; omitted when the user has never signed in"`
	PermissionsByEnv       map[string][]string     `json:"permissionsByEnv" readOnly:"true" doc:"Permissions by environment ID; 'global' applies to all environments and organization endpoints"`
	Preferences            UserPreferences         `json:"preferences" required:"false" doc:"Personal display and UI preferences"`
	ID                     string                  `json:"id" readOnly:"true" doc:"Unique identifier of the user" example:"550e8400-e29b-41d4-a716-446655440000"`
	Username               string                  `json:"username" required:"false" minLength:"1" maxLength:"255" pattern:"^[^@]+$" patternDescription:"No @ character" doc:"Username (no @)"`
	Password               string                  `json:"password,omitempty" writeOnly:"true" minLength:"8" doc:"Password of the user; never returned"`
	TimeFormat             TimeFormat              `json:"timeFormat" required:"false" enum:"auto,12h,24h" doc:"Preferred time display format" example:"auto"`
	CreatedAt              string                  `json:"createdAt,omitempty" readOnly:"true" doc:"Date and time when the user was created"`
	UpdatedAt              string                  `json:"updatedAt,omitempty" readOnly:"true" doc:"Date and time when the user was last updated"`
	RoleAssignments        []RoleAssignmentSummary `json:"roleAssignments" readOnly:"true" doc:"Role assignments held by the user"`
	IsGlobalAdmin          bool                    `json:"isGlobalAdmin" readOnly:"true" doc:"Whether the user effectively holds global administrator access"`
	CanDelete              bool                    `json:"canDelete" readOnly:"true" doc:"Whether the user can currently be deleted"`
	RequiresPasswordChange bool                    `json:"requiresPasswordChange" readOnly:"true" doc:"Whether the user must change their password"`
}

// Actor identifies a user performing domain work without exposing persistence data.
type Actor struct {
	Preferences UserPreferences `json:"preferences"`
	DisplayName *string         `json:"displayName,omitempty"`
	ID          string          `json:"id"`
	Username    string          `json:"username"`
}

// ActorProvider supplies an actor from an authenticated user model.
type ActorProvider interface{ Actor() *Actor }

// CurrentUserContextKey holds the authenticated user or actor in a request context.
type CurrentUserContextKey struct{}

// SystemUser identifies work initiated by Arcane itself.
var SystemUser = Actor{Username: "System"}
