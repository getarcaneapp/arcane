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
	ProjectsDirectory          *string `json:"projectsDirectory,omitempty"`
	DiskUsagePath              *string `json:"diskUsagePath,omitempty"`
	AutoUpdate                 *string `json:"autoUpdate,omitempty"`
	AutoUpdateCron             *string `json:"autoUpdateCron,omitempty"`
	PollingEnabled             *string `json:"pollingEnabled,omitempty"`
	PollingInterval            *string `json:"pollingInterval,omitempty"`
	PruneMode                  *string `json:"dockerPruneMode,omitempty" binding:"omitempty,oneof=all dangling"`
	BaseServerURL              *string `json:"baseServerUrl,omitempty"`
	EnableGravatar             *string `json:"enableGravatar,omitempty"`
	DefaultShell               *string `json:"defaultShell,omitempty"`
	DockerHost                 *string `json:"dockerHost,omitempty"`
	AccentColor                *string `json:"accentColor,omitempty"`
	AuthLocalEnabled           *string `json:"authLocalEnabled,omitempty"`
	AuthOidcEnabled            *string `json:"authOidcEnabled,omitempty"`
	AuthOidcMergeAccounts      *string `json:"authOidcMergeAccounts,omitempty"`
	AuthSessionTimeout         *string `json:"authSessionTimeout,omitempty"`
	AuthPasswordPolicy         *string `json:"authPasswordPolicy,omitempty"`
	AuthOidcConfig             *string `json:"authOidcConfig,omitempty"`
	OnboardingCompleted        *string `json:"onboardingCompleted,omitempty"`
	OnboardingSteps            *string `json:"onboardingSteps,omitempty"`
	MobileNavigationMode       *string `json:"mobileNavigationMode,omitempty"`
	MobileNavigationShowLabels *string `json:"mobileNavigationShowLabels,omitempty"`
	SidebarHoverExpansion      *string `json:"sidebarHoverExpansion,omitempty"`
	GlassEffectEnabled         *string `json:"glassEffectEnabled,omitempty"`
}
