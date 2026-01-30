package settings

// PublicSetting represents a publicly accessible setting.
type PublicSetting struct {
	// Key is the identifier of the setting.
	//
	// Required: true
	Key string `json:"key"`

	// Type is the data type of the setting value.
	//
	// Required: true
	Type string `json:"type"`

	// Value is the setting value.
	//
	// Required: true
	Value string `json:"value"`
}

// SettingDto represents a setting with visibility information.
type SettingDto struct {
	// Embedded PublicSetting fields.
	PublicSetting

	// IsPublic indicates if the setting is publicly accessible.
	//
	// Required: true
	IsPublic bool `json:"isPublic"`
}

// Update is used to update application settings.
type Update struct {
	// ProjectsDirectory is the directory path where projects are stored.
	// Must be an absolute path.
	//
	// Required: false
	ProjectsDirectory *string `json:"projectsDirectory,omitempty"`

	// DiskUsagePath is the path to monitor for disk usage.
	//
	// Required: false
	DiskUsagePath *string `json:"diskUsagePath,omitempty"`

	// AutoUpdate indicates if automatic updates are enabled.
	//
	// Required: false
	AutoUpdate *string `json:"autoUpdate,omitempty"`

	// AutoUpdateInterval is the interval for checking automatic updates.
	//
	// Required: false
	AutoUpdateInterval *string `json:"autoUpdateInterval,omitempty"`

	// PollingEnabled indicates if polling is enabled.
	//
	// Required: false
	PollingEnabled *string `json:"pollingEnabled,omitempty"`

	// PollingInterval is the interval for polling operations.
	//
	// Required: false
	PollingInterval *string `json:"pollingInterval,omitempty"`

	// AutoInjectEnv indicates if project .env variables should be automatically injected into all containers.
	//
	// Required: false
	AutoInjectEnv *string `json:"autoInjectEnv,omitempty"`

	// EnvironmentHealthInterval is the interval for checking environment health.
	//
	// Required: false
	EnvironmentHealthInterval *string `json:"environmentHealthInterval,omitempty"`

	// PruneMode is the Docker prune mode ("all" or "dangling").
	//
	// Required: false
	PruneMode *string `json:"dockerPruneMode,omitempty" binding:"omitempty,oneof=all dangling"`

	// ScheduledPruneEnabled indicates if scheduled pruning is enabled.
	//
	// Required: false
	ScheduledPruneEnabled *string `json:"scheduledPruneEnabled,omitempty"`

	// ScheduledPruneInterval is the interval in minutes between prune operations.
	//
	// Required: false
	ScheduledPruneInterval *string `json:"scheduledPruneInterval,omitempty"`

	// ScheduledPruneContainers indicates if stopped containers should be pruned.
	//
	// Required: false
	ScheduledPruneContainers *string `json:"scheduledPruneContainers,omitempty"`

	// ScheduledPruneImages indicates if unused images should be pruned.
	//
	// Required: false
	ScheduledPruneImages *string `json:"scheduledPruneImages,omitempty"`

	// ScheduledPruneVolumes indicates if unused volumes should be pruned.
	//
	// Required: false
	ScheduledPruneVolumes *string `json:"scheduledPruneVolumes,omitempty"`

	// ScheduledPruneNetworks indicates if unused networks should be pruned.
	//
	// Required: false
	ScheduledPruneNetworks *string `json:"scheduledPruneNetworks,omitempty"`

	// ScheduledPruneBuildCache indicates if build cache should be pruned.
	//
	// Required: false
	ScheduledPruneBuildCache *string `json:"scheduledPruneBuildCache,omitempty"`

	// MaxImageUploadSize is the maximum size for image uploads.
	//
	// Required: false
	MaxImageUploadSize *string `json:"maxImageUploadSize,omitempty"`

	// BaseServerURL is the base URL of the server.
	//
	// Required: false
	BaseServerURL *string `json:"baseServerUrl,omitempty"`

	// EnableGravatar indicates if Gravatar is enabled for user avatars.
	//
	// Required: false
	EnableGravatar *string `json:"enableGravatar,omitempty"`

	// DefaultShell is the default shell used for container execution.
	//
	// Required: false
	DefaultShell *string `json:"defaultShell,omitempty"`

	// DockerHost is the Docker host connection string.
	//
	// Required: false
	DockerHost *string `json:"dockerHost,omitempty"`

	// AccentColor is the UI accent color.
	//
	// Required: false
	AccentColor *string `json:"accentColor,omitempty"`

	// AuthLocalEnabled indicates if local authentication is enabled.
	//
	// Required: false
	AuthLocalEnabled *string `json:"authLocalEnabled,omitempty"`

	// OidcEnabled indicates if OIDC authentication is enabled.
	//
	// Required: false
	OidcEnabled *string `json:"oidcEnabled,omitempty"`

	// OidcMergeAccounts indicates if OIDC accounts should be merged with local accounts.
	//
	// Required: false
	OidcMergeAccounts *string `json:"oidcMergeAccounts,omitempty"`

	// AuthSessionTimeout is the session timeout duration.
	//
	// Required: false
	AuthSessionTimeout *string `json:"authSessionTimeout,omitempty"`

	// AuthPasswordPolicy is the password policy rules.
	//
	// Required: false
	AuthPasswordPolicy *string `json:"authPasswordPolicy,omitempty"`

	// AuthOidcConfig is deprecated and will be removed in a future release.
	//
	// Required: false
	AuthOidcConfig *string `json:"authOidcConfig,omitempty"`

	// OidcClientId is the OIDC client identifier.
	//
	// Required: false
	OidcClientId *string `json:"oidcClientId,omitempty"`

	// OidcClientSecret is the OIDC client secret.
	//
	// Required: false
	OidcClientSecret *string `json:"oidcClientSecret,omitempty"`

	// OidcIssuerUrl is the OIDC issuer URL.
	//
	// Required: false
	OidcIssuerUrl *string `json:"oidcIssuerUrl,omitempty"`

	// OidcScopes is the list of OIDC scopes to request.
	//
	// Required: false
	OidcScopes *string `json:"oidcScopes,omitempty"`

	// OidcAdminClaim is the OIDC claim name used to identify administrators.
	//
	// Required: false
	OidcAdminClaim *string `json:"oidcAdminClaim,omitempty"`

	// OidcAdminValue is the OIDC claim value that identifies administrators.
	//
	// Required: false
	OidcAdminValue *string `json:"oidcAdminValue,omitempty"`

	// OidcSkipTlsVerify indicates if TLS verification should be skipped for OIDC.
	//
	// Required: false
	OidcSkipTlsVerify *string `json:"oidcSkipTlsVerify,omitempty"`

	// OidcAutoRedirectToProvider indicates if the login page should automatically redirect to OIDC provider.
	//
	// Required: false
	OidcAutoRedirectToProvider *string `json:"oidcAutoRedirectToProvider,omitempty"`

	// OidcProviderName is the custom display name for the OIDC provider.
	//
	// Required: false
	OidcProviderName *string `json:"oidcProviderName,omitempty"`

	// OidcProviderLogoUrl is the custom logo URL for the OIDC provider.
	//
	// Required: false
	OidcProviderLogoUrl *string `json:"oidcProviderLogoUrl,omitempty"`

	// MobileNavigationMode is the navigation mode for mobile devices.
	//
	// Required: false
	MobileNavigationMode *string `json:"mobileNavigationMode,omitempty"`

	// MobileNavigationShowLabels indicates if labels should be shown in mobile navigation.
	//
	// Required: false
	MobileNavigationShowLabels *string `json:"mobileNavigationShowLabels,omitempty"`

	// SidebarHoverExpansion indicates if the sidebar expands on hover.
	//
	// Required: false
	SidebarHoverExpansion *string `json:"sidebarHoverExpansion,omitempty"`
}
