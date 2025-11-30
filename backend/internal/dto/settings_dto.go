package dto

type PublicSettingDto struct {
	Key   string `json:"key"`
	Type  string `json:"type"`
	Value string `json:"value"`
}

type SettingDto struct {
	PublicSettingDto
	IsPublic bool `json:"isPublic"`
}

type UpdateSettingsDto struct {
	ProjectsDirectory         *string `json:"projectsDirectory,omitempty"`
	DiskUsagePath             *string `json:"diskUsagePath,omitempty"`
	AutoUpdate                *string `json:"autoUpdate,omitempty"`
	AutoUpdateInterval        *string `json:"autoUpdateInterval,omitempty"`
	PollingEnabled            *string `json:"pollingEnabled,omitempty"`
	PollingInterval           *string `json:"pollingInterval,omitempty"`
	EnvironmentHealthInterval *string `json:"environmentHealthInterval,omitempty"`
	PruneMode                 *string `json:"dockerPruneMode,omitempty" binding:"omitempty,oneof=all dangling"`
	MaxImageUploadSize        *string `json:"maxImageUploadSize,omitempty"`
	BaseServerURL             *string `json:"baseServerUrl,omitempty"`
	EnableGravatar            *string `json:"enableGravatar,omitempty"`
	DefaultShell              *string `json:"defaultShell,omitempty"`
	DockerHost                *string `json:"dockerHost,omitempty"`
	AccentColor               *string `json:"accentColor,omitempty"`
	AuthLocalEnabled          *string `json:"authLocalEnabled,omitempty"`
	OidcEnabled               *string `json:"oidcEnabled,omitempty"`
	OidcMergeAccounts         *string `json:"oidcMergeAccounts,omitempty"`
	AuthSessionTimeout        *string `json:"authSessionTimeout,omitempty"`
	AuthPasswordPolicy        *string `json:"authPasswordPolicy,omitempty"`
	// AuthOidcConfig DEPRECATED will be removed in a future release
	AuthOidcConfig             *string `json:"authOidcConfig,omitempty"`
	OidcClientId               *string `json:"oidcClientId,omitempty"`
	OidcClientSecret           *string `json:"oidcClientSecret,omitempty"`
	OidcIssuerUrl              *string `json:"oidcIssuerUrl,omitempty"`
	OidcScopes                 *string `json:"oidcScopes,omitempty"`
	OidcAdminClaim             *string `json:"oidcAdminClaim,omitempty"`
	OidcAdminValue             *string `json:"oidcAdminValue,omitempty"`
	OnboardingCompleted        *string `json:"onboardingCompleted,omitempty"`
	OnboardingSteps            *string `json:"onboardingSteps,omitempty"`
	MobileNavigationMode       *string `json:"mobileNavigationMode,omitempty"`
	MobileNavigationShowLabels *string `json:"mobileNavigationShowLabels,omitempty"`
	SidebarHoverExpansion      *string `json:"sidebarHoverExpansion,omitempty"`
	GlassEffectEnabled         *string `json:"glassEffectEnabled,omitempty"`
}
