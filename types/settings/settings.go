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

	// TemplatesDirectory is the directory path where local compose template folders are discovered.
	// Must be an absolute path.
	//
	// Required: false
	TemplatesDirectory *string `json:"templatesDirectory,omitempty"`

	// FollowProjectSymlinks controls whether symlinked child directories in the projects directory are discovered as projects.
	//
	// Required: false
	FollowProjectSymlinks *string `json:"followProjectSymlinks,omitempty"`

	// SwarmStackSourcesDirectory is the directory path where swarm stack source files are stored.
	// Must be an absolute path.
	//
	// Required: false
	SwarmStackSourcesDirectory *string `json:"swarmStackSourcesDirectory,omitempty"`

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

	// DockerClientRefreshInterval is the cron expression for refreshing the cached Docker client.
	//
	// Required: false
	DockerClientRefreshInterval *string `json:"dockerClientRefreshInterval,omitempty"`

	// AutoInjectEnv indicates if project .env variables should be automatically injected into all containers.
	//
	// Required: false
	AutoInjectEnv *string `json:"autoInjectEnv,omitempty"`

	// EnvironmentHealthInterval is the interval for checking environment health.
	//
	// Required: false
	EnvironmentHealthInterval *string `json:"environmentHealthInterval,omitempty"`

	// ActivityHistoryRetentionDays is the number of days of completed Activity Center history to retain.
	//
	// Required: false
	ActivityHistoryRetentionDays *string `json:"activityHistoryRetentionDays,omitempty"`

	// ActivityHistoryMaxEntries is the maximum completed Activity Center entries to retain per environment.
	//
	// Required: false
	ActivityHistoryMaxEntries *string `json:"activityHistoryMaxEntries,omitempty"`

	// DefaultDeployPullPolicy is the default image pull policy used for project deploys.
	//
	// Required: false
	DefaultDeployPullPolicy *string `json:"defaultDeployPullPolicy,omitempty" binding:"omitempty,oneof=missing always never"`

	// ScheduledPruneEnabled indicates if scheduled pruning is enabled.
	//
	// Required: false
	ScheduledPruneEnabled *string `json:"scheduledPruneEnabled,omitempty"`

	// ScheduledPruneInterval is the interval in minutes between prune operations.
	//
	// Required: false
	ScheduledPruneInterval *string `json:"scheduledPruneInterval,omitempty"`

	// PruneContainerMode controls how containers are pruned during scheduled prune.
	//
	// Required: false
	PruneContainerMode *string `json:"pruneContainerMode,omitempty" binding:"omitempty,oneof=none stopped olderThan"`

	// PruneContainerUntil is the Docker duration string used when the container prune mode is olderThan.
	//
	// Required: false
	PruneContainerUntil *string `json:"pruneContainerUntil,omitempty"`

	// PruneImageMode controls how images are pruned during scheduled prune.
	//
	// Required: false
	PruneImageMode *string `json:"pruneImageMode,omitempty" binding:"omitempty,oneof=none dangling all olderThan"`

	// PruneImageUntil is the Docker duration string used when the image prune mode is olderThan.
	//
	// Required: false
	PruneImageUntil *string `json:"pruneImageUntil,omitempty"`

	// PruneVolumeMode controls how volumes are pruned during scheduled prune.
	//
	// Required: false
	PruneVolumeMode *string `json:"pruneVolumeMode,omitempty" binding:"omitempty,oneof=none anonymous all"`

	// PruneNetworkMode controls how networks are pruned during scheduled prune.
	//
	// Required: false
	PruneNetworkMode *string `json:"pruneNetworkMode,omitempty" binding:"omitempty,oneof=none unused olderThan"`

	// PruneNetworkUntil is the Docker duration string used when the network prune mode is olderThan.
	//
	// Required: false
	PruneNetworkUntil *string `json:"pruneNetworkUntil,omitempty"`

	// PruneBuildCacheMode controls how build cache is pruned during scheduled prune.
	//
	// Required: false
	PruneBuildCacheMode *string `json:"pruneBuildCacheMode,omitempty" binding:"omitempty,oneof=none unused all olderThan"`

	// PruneBuildCacheUntil is the Docker duration string used when the build cache prune mode is olderThan.
	//
	// Required: false
	PruneBuildCacheUntil *string `json:"pruneBuildCacheUntil,omitempty"`

	// VulnerabilityScanEnabled indicates if scheduled vulnerability scanning is enabled.
	//
	// Required: false
	VulnerabilityScanEnabled *string `json:"vulnerabilityScanEnabled,omitempty"`

	// VulnerabilityScanInterval is the cron expression for scheduled vulnerability scans.
	//
	// Required: false
	VulnerabilityScanInterval *string `json:"vulnerabilityScanInterval,omitempty"`

	// MaxImageUploadSize is the maximum size for image uploads.
	//
	// Required: false
	MaxImageUploadSize *string `json:"maxImageUploadSize,omitempty"`

	// GitSyncMaxFiles is the maximum number of repository files copied during a Git sync.
	// Set to "0" to disable the environment cap.
	//
	// Required: false
	GitSyncMaxFiles *string `json:"gitSyncMaxFiles,omitempty"`

	// GitSyncMaxTotalSizeMb is the maximum combined size in megabytes for files copied during a Git sync.
	// Set to "0" to disable the environment cap.
	//
	// Required: false
	GitSyncMaxTotalSizeMb *string `json:"gitSyncMaxTotalSizeMb,omitempty"`

	// GitSyncMaxBinarySizeMb is the maximum size in megabytes for a single binary file copied during a Git sync.
	// Set to "0" to disable the environment cap.
	//
	// Required: false
	GitSyncMaxBinarySizeMb *string `json:"gitSyncMaxBinarySizeMb,omitempty"`

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

	// ApplicationTheme is the overall application theme preset.
	//
	// Required: false
	ApplicationTheme *string `json:"applicationTheme,omitempty"`

	// IconCatalog is the catalog used to resolve icon slugs.
	//
	// Required: false
	IconCatalog *string `json:"iconCatalog,omitempty"`

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

	// TrivyImage overrides the container image used for vulnerability scans.
	//
	// Required: false
	TrivyImage *string `json:"trivyImage,omitempty"`

	// TrivyNetwork sets the Docker network mode/network name for Trivy scan containers.
	// Leave empty to inherit Arcane's network automatically, with bridge as the final fallback.
	//
	// Required: false
	TrivyNetwork *string `json:"trivyNetwork,omitempty"`

	// TrivySecurityOpts applies Docker security options to Trivy scan containers.
	// Accepts comma-separated or newline-separated values.
	//
	// Required: false
	TrivySecurityOpts *string `json:"trivySecurityOpts,omitempty"`

	// TrivyPrivileged controls whether Trivy scan containers run in privileged mode.
	//
	// Required: false
	TrivyPrivileged *string `json:"trivyPrivileged,omitempty"`

	// TrivyPreserveCacheOnVolumePrune controls whether the Trivy cache volume is excluded from manual and scheduled volume prune runs.
	//
	// Required: false
	TrivyPreserveCacheOnVolumePrune *string `json:"trivyPreserveCacheOnVolumePrune,omitempty"`

	// TrivyResourceLimitsEnabled controls whether CPU and memory limits are applied to Trivy scan containers.
	//
	// Required: false
	TrivyResourceLimitsEnabled *string `json:"trivyResourceLimitsEnabled,omitempty"`

	// TrivyCpuLimit is the CPU limit in cores for Trivy scan containers.
	// Supports decimals (for example: "1.5"). Set to "0" to disable the CPU limit.
	//
	// Required: false
	TrivyCpuLimit *string `json:"trivyCpuLimit,omitempty"`

	// TrivyMemoryLimitMb is the memory limit in megabytes for Trivy scan containers.
	// Set to "0" to disable the memory limit.
	//
	// Required: false
	TrivyMemoryLimitMb *string `json:"trivyMemoryLimitMb,omitempty"`

	// TrivyConcurrentScanContainers is the maximum number of concurrent Trivy scan containers.
	// Applies to manual and scheduled scans. Minimum value is "1".
	//
	// Required: false
	TrivyConcurrentScanContainers *string `json:"trivyConcurrentScanContainers,omitempty"`

	// TrivyServerEnabled enables Trivy client/server mode, scanning against a remote
	// Trivy server instead of opening a local vulnerability database.
	//
	// Required: false
	TrivyServerEnabled *string `json:"trivyServerEnabled,omitempty"`

	// TrivyServerUrl is the URL of the remote Trivy server used in client/server mode.
	//
	// Required: false
	TrivyServerUrl *string `json:"trivyServerUrl,omitempty"`

	// TrivyServerToken is the optional authentication token sent to the remote Trivy server.
	//
	// Required: false
	TrivyServerToken *string `json:"trivyServerToken,omitempty"`

	// TrivyIgnoreUnfixed restricts scan results to vulnerabilities that have a known fix.
	//
	// Required: false
	TrivyIgnoreUnfixed *string `json:"trivyIgnoreUnfixed,omitempty"`

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

	// OidcGroupsClaim is the OIDC claim path read on every login to drive
	// role assignment via oidc_role_mappings. Default: "groups".
	//
	// Required: false
	OidcGroupsClaim *string `json:"oidcGroupsClaim,omitempty"`

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

	// KeyboardShortcutsEnabled indicates if keyboard shortcuts are enabled.
	//
	// Required: false
	KeyboardShortcutsEnabled *string `json:"keyboardShortcutsEnabled,omitempty"`

	// DockerApiTimeout is the timeout for Docker API operations in seconds.
	//
	// Required: false
	DockerApiTimeout *string `json:"dockerApiTimeout,omitempty"`

	// DockerImagePullTimeout is the timeout for Docker image pulls in seconds.
	//
	// Required: false
	DockerImagePullTimeout *string `json:"dockerImagePullTimeout,omitempty"`

	// TrivyScanTimeout is the timeout for Trivy image vulnerability scans in seconds.
	//
	// Required: false
	TrivyScanTimeout *string `json:"trivyScanTimeout,omitempty"`

	// GitOperationTimeout is the timeout for Git clone/fetch operations in seconds.
	//
	// Required: false
	GitOperationTimeout *string `json:"gitOperationTimeout,omitempty"`

	// HttpClientTimeout is the default timeout for HTTP requests in seconds.
	//
	// Required: false
	HttpClientTimeout *string `json:"httpClientTimeout,omitempty"`

	// RegistryTimeout is the timeout for container registry operations in seconds.
	//
	// Required: false
	RegistryTimeout *string `json:"registryTimeout,omitempty"`

	// ProxyRequestTimeout is the timeout for proxied requests to remote environments in seconds.
	//
	// Required: false
	ProxyRequestTimeout *string `json:"proxyRequestTimeout,omitempty"`

	// AutoUpdateExcludedContainers is a comma-separated list of container names to exclude from auto-update.
	//
	// Required: false
	AutoUpdateExcludedContainers *string `json:"autoUpdateExcludedContainers,omitempty"`

	// AutoHealEnabled indicates if automatic container healing is enabled.
	//
	// Required: false
	AutoHealEnabled *string `json:"autoHealEnabled,omitempty"`

	// AutoHealInterval is the cron expression for how often to check container health.
	//
	// Required: false
	AutoHealInterval *string `json:"autoHealInterval,omitempty"`

	// AutoHealExcludedContainers is a comma-separated list of container names to exclude from auto-heal.
	//
	// Required: false
	AutoHealExcludedContainers *string `json:"autoHealExcludedContainers,omitempty"`

	// AutoHealMaxRestarts is the maximum number of auto-heal restarts per container within the restart window.
	//
	// Required: false
	AutoHealMaxRestarts *string `json:"autoHealMaxRestarts,omitempty"`

	// AutoHealRestartWindow is the time window in minutes for counting auto-heal restarts.
	//
	// Required: false
	AutoHealRestartWindow *string `json:"autoHealRestartWindow,omitempty"`

	// VolumeBrowserHelperIdleTimeout is the number of minutes a volume-browser helper
	// container may sit idle before it is automatically removed (0 disables).
	//
	// Required: false
	VolumeBrowserHelperIdleTimeout *string `json:"volumeBrowserHelperIdleTimeout,omitempty"`

	// BuildProvider is the default build provider (local|depot).
	//
	// Required: false
	BuildProvider *string `json:"buildProvider,omitempty"`

	// BuildsDirectory is the root directory for manual build workspaces.
	//
	// Required: false
	BuildsDirectory *string `json:"buildsDirectory,omitempty"`

	// BuildTimeout is the timeout for BuildKit builds in seconds.
	//
	// Required: false
	BuildTimeout *string `json:"buildTimeout,omitempty"`

	// DepotProjectId is the Depot project identifier.
	//
	// Required: false
	DepotProjectId *string `json:"depotProjectId,omitempty"`

	// DepotToken is the Depot API token.
	//
	// Required: false
	DepotToken *string `json:"depotToken,omitempty"`

	// OledMode sets whether OLED dark mode is enabled or not.
	//
	// Required: false
	OledMode *string `json:"oledMode,omitempty"`
}
