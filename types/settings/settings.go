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
	ProjectsDirectory *string `json:"projectsDirectory,omitzero"`

	// TemplatesDirectory is the directory path where local compose template folders are discovered.
	// Must be an absolute path.
	//
	// Required: false
	TemplatesDirectory *string `json:"templatesDirectory,omitzero"`

	// FollowProjectSymlinks controls whether symlinked child directories in the projects directory are discovered as projects.
	//
	// Required: false
	FollowProjectSymlinks *string `json:"followProjectSymlinks,omitzero"`

	// SwarmStackSourcesDirectory is the directory path where swarm stack source files are stored.
	// Must be an absolute path.
	//
	// Required: false
	SwarmStackSourcesDirectory *string `json:"swarmStackSourcesDirectory,omitzero"`

	// DiskUsagePath is the path to monitor for disk usage.
	//
	// Required: false
	DiskUsagePath *string `json:"diskUsagePath,omitzero"`

	// AutoUpdate indicates if automatic updates are enabled.
	//
	// Required: false
	AutoUpdate *string `json:"autoUpdate,omitzero"`

	// AutoUpdateInterval is the interval for checking automatic updates.
	//
	// Required: false
	AutoUpdateInterval *string `json:"autoUpdateInterval,omitzero"`

	// PollingEnabled indicates if polling is enabled.
	//
	// Required: false
	PollingEnabled *string `json:"pollingEnabled,omitzero"`

	// PollingInterval is the interval for polling operations.
	//
	// Required: false
	PollingInterval *string `json:"pollingInterval,omitzero"`

	// ImageEventWatcherEnabled indicates if Docker image events trigger image checks.
	//
	// Required: false
	ImageEventWatcherEnabled *string `json:"imageEventWatcherEnabled,omitzero"`

	// DockerClientRefreshInterval is the cron expression for refreshing the cached Docker client.
	//
	// Required: false
	DockerClientRefreshInterval *string `json:"dockerClientRefreshInterval,omitzero"`

	// AutoInjectEnv indicates if project .env variables should be automatically injected into all containers.
	//
	// Required: false
	AutoInjectEnv *string `json:"autoInjectEnv,omitzero"`

	// EnvironmentHealthInterval is the interval for checking environment health.
	//
	// Required: false
	EnvironmentHealthInterval *string `json:"environmentHealthInterval,omitzero"`

	// ActivityHistoryRetentionDays is the number of days of completed Activity Center history to retain.
	//
	// Required: false
	ActivityHistoryRetentionDays *string `json:"activityHistoryRetentionDays,omitzero"`

	// ActivityHistoryMaxEntries is the maximum completed Activity Center entries to retain per environment.
	//
	// Required: false
	ActivityHistoryMaxEntries *string `json:"activityHistoryMaxEntries,omitzero"`

	// MaxConcurrentActivities is the maximum long-running activities per environment before new ones queue (0 = unlimited).
	//
	// Required: false
	MaxConcurrentActivities *string `json:"maxConcurrentActivities,omitzero"`

	// DefaultDeployPullPolicy is the default image pull policy used for project deploys.
	//
	// Required: false
	DefaultDeployPullPolicy *string `json:"defaultDeployPullPolicy,omitzero" binding:"omitempty,oneof=missing always never"`

	// ScheduledPruneEnabled indicates if scheduled pruning is enabled.
	//
	// Required: false
	ScheduledPruneEnabled *string `json:"scheduledPruneEnabled,omitzero"`

	// ScheduledPruneInterval is the interval in minutes between prune operations.
	//
	// Required: false
	ScheduledPruneInterval *string `json:"scheduledPruneInterval,omitzero"`

	// PruneContainerMode controls how containers are pruned during scheduled prune.
	//
	// Required: false
	PruneContainerMode *string `json:"pruneContainerMode,omitzero" binding:"omitempty,oneof=none stopped olderThan"`

	// PruneContainerUntil is the Docker duration string used when the container prune mode is olderThan.
	//
	// Required: false
	PruneContainerUntil *string `json:"pruneContainerUntil,omitzero"`

	// PruneImageMode controls how images are pruned during scheduled prune.
	//
	// Required: false
	PruneImageMode *string `json:"pruneImageMode,omitzero" binding:"omitempty,oneof=none dangling all olderThan"`

	// PruneImageUntil is the Docker duration string used when the image prune mode is olderThan.
	//
	// Required: false
	PruneImageUntil *string `json:"pruneImageUntil,omitzero"`

	// PruneVolumeMode controls how volumes are pruned during scheduled prune.
	//
	// Required: false
	PruneVolumeMode *string `json:"pruneVolumeMode,omitzero" binding:"omitempty,oneof=none anonymous all"`

	// PruneNetworkMode controls how networks are pruned during scheduled prune.
	//
	// Required: false
	PruneNetworkMode *string `json:"pruneNetworkMode,omitzero" binding:"omitempty,oneof=none unused olderThan"`

	// PruneNetworkUntil is the Docker duration string used when the network prune mode is olderThan.
	//
	// Required: false
	PruneNetworkUntil *string `json:"pruneNetworkUntil,omitzero"`

	// PruneBuildCacheMode controls how build cache is pruned during scheduled prune.
	//
	// Required: false
	PruneBuildCacheMode *string `json:"pruneBuildCacheMode,omitzero" binding:"omitempty,oneof=none unused all olderThan"`

	// PruneBuildCacheUntil is the Docker duration string used when the build cache prune mode is olderThan.
	//
	// Required: false
	PruneBuildCacheUntil *string `json:"pruneBuildCacheUntil,omitzero"`

	// VulnerabilityScanEnabled indicates if scheduled vulnerability scanning is enabled.
	//
	// Required: false
	VulnerabilityScanEnabled *string `json:"vulnerabilityScanEnabled,omitzero"`

	// VulnerabilityScanInterval is the cron expression for scheduled vulnerability scans.
	//
	// Required: false
	VulnerabilityScanInterval *string `json:"vulnerabilityScanInterval,omitzero"`

	// MaxImageUploadSize is the maximum size for image uploads.
	//
	// Required: false
	MaxImageUploadSize *string `json:"maxImageUploadSize,omitzero"`

	// GitSyncMaxFiles is the maximum number of repository files copied during a Git sync.
	// Set to "0" to disable the environment cap.
	//
	// Required: false
	GitSyncMaxFiles *string `json:"gitSyncMaxFiles,omitzero"`

	// GitSyncMaxTotalSizeMb is the maximum combined size in megabytes for files copied during a Git sync.
	// Set to "0" to disable the environment cap.
	//
	// Required: false
	GitSyncMaxTotalSizeMb *string `json:"gitSyncMaxTotalSizeMb,omitzero"`

	// GitSyncMaxBinarySizeMb is the maximum size in megabytes for a single binary file copied during a Git sync.
	// Set to "0" to disable the environment cap.
	//
	// Required: false
	GitSyncMaxBinarySizeMb *string `json:"gitSyncMaxBinarySizeMb,omitzero"`

	// LifecycleEnabled gates whether GitOps syncs may configure pre-deploy
	// lifecycle scripts. Disabled by default because scripts are repo-trusted
	// code that runs on every deploy.
	//
	// Required: false
	LifecycleEnabled *string `json:"lifecycleEnabled,omitzero"`

	// LifecycleDefaultRunnerImage is the default container image used to run
	// GitOps pre-deploy lifecycle scripts when a sync does not override it.
	//
	// Required: false
	LifecycleDefaultRunnerImage *string `json:"lifecycleDefaultRunnerImage,omitzero"`

	// LifecycleMaxTimeoutSec caps the per-sync pre-deploy timeout admins can
	// configure. Zero disables the cap.
	//
	// Required: false
	LifecycleMaxTimeoutSec *string `json:"lifecycleMaxTimeoutSec,omitzero"`

	// BaseServerURL is the base URL of the server.
	//
	// Required: false
	BaseServerURL *string `json:"baseServerUrl,omitzero"`

	// EnableGravatar indicates if Gravatar is enabled for user avatars.
	//
	// Required: false
	EnableGravatar *string `json:"enableGravatar,omitzero"`

	// AvatarMaxUploadSizeMb is the maximum size in megabytes for profile picture uploads.
	//
	// Required: false
	AvatarMaxUploadSizeMb *string `json:"avatarMaxUploadSizeMb,omitzero"`

	ExperimentalFeaturesEnabled *string `json:"experimentalFeaturesEnabled,omitzero"`

	// DefaultShell is the default shell used for container execution.
	//
	// Required: false
	DefaultShell *string `json:"defaultShell,omitzero"`

	// DockerHost is the Docker host connection string.
	//
	// Required: false
	DockerHost *string `json:"dockerHost,omitzero"`

	// AuthLocalEnabled indicates if local authentication is enabled.
	//
	// Required: false
	AuthLocalEnabled *string `json:"authLocalEnabled,omitzero"`

	// OidcEnabled indicates if OIDC authentication is enabled.
	//
	// Required: false
	OidcEnabled *string `json:"oidcEnabled,omitzero"`

	// OidcMergeAccounts indicates if OIDC accounts should be merged with local accounts.
	//
	// Required: false
	OidcMergeAccounts *string `json:"oidcMergeAccounts,omitzero"`

	// AuthSessionTimeout is the session timeout duration.
	//
	// Required: false
	AuthSessionTimeout *string `json:"authSessionTimeout,omitzero"`

	// AuthPasswordPolicy is the password policy rules.
	//
	// Required: false
	AuthPasswordPolicy *string `json:"authPasswordPolicy,omitzero"`

	// TrivyImage overrides the container image used for vulnerability scans.
	//
	// Required: false
	TrivyImage *string `json:"trivyImage,omitzero"`

	// TrivyNetwork sets the Docker network mode/network name for Trivy scan containers.
	// Leave empty to inherit Arcane's network automatically, with bridge as the final fallback.
	//
	// Required: false
	TrivyNetwork *string `json:"trivyNetwork,omitzero"`

	// TrivySecurityOpts applies Docker security options to Trivy scan containers.
	// Accepts comma-separated or newline-separated values.
	//
	// Required: false
	TrivySecurityOpts *string `json:"trivySecurityOpts,omitzero"`

	// TrivyPrivileged controls whether Trivy scan containers run in privileged mode.
	//
	// Required: false
	TrivyPrivileged *string `json:"trivyPrivileged,omitzero"`

	// TrivyPreserveCacheOnVolumePrune controls whether the Trivy cache volume is excluded from manual and scheduled volume prune runs.
	//
	// Required: false
	TrivyPreserveCacheOnVolumePrune *string `json:"trivyPreserveCacheOnVolumePrune,omitzero"`

	// TrivyResourceLimitsEnabled controls whether CPU and memory limits are applied to Trivy scan containers.
	//
	// Required: false
	TrivyResourceLimitsEnabled *string `json:"trivyResourceLimitsEnabled,omitzero"`

	// TrivyCpuLimit is the CPU limit in cores for Trivy scan containers.
	// Supports decimals (for example: "1.5"). Set to "0" to disable the CPU limit.
	//
	// Required: false
	TrivyCpuLimit *string `json:"trivyCpuLimit,omitzero"`

	// TrivyMemoryLimitMb is the memory limit in megabytes for Trivy scan containers.
	// Set to "0" to disable the memory limit.
	//
	// Required: false
	TrivyMemoryLimitMb *string `json:"trivyMemoryLimitMb,omitzero"`

	// TrivyConcurrentScanContainers is the maximum number of concurrent Trivy scan containers.
	// Applies to manual and scheduled scans. Minimum value is "1".
	//
	// Required: false
	TrivyConcurrentScanContainers *string `json:"trivyConcurrentScanContainers,omitzero"`

	// TrivyServerEnabled enables Trivy client/server mode, scanning against a remote
	// Trivy server instead of opening a local vulnerability database.
	//
	// Required: false
	TrivyServerEnabled *string `json:"trivyServerEnabled,omitzero"`

	// TrivyServerUrl is the URL of the remote Trivy server used in client/server mode.
	//
	// Required: false
	TrivyServerUrl *string `json:"trivyServerUrl,omitzero"`

	// TrivyServerToken is the optional authentication token sent to the remote Trivy server.
	//
	// Required: false
	TrivyServerToken *string `json:"trivyServerToken,omitzero"`

	// TrivyIgnoreUnfixed restricts scan results to vulnerabilities that have a known fix.
	//
	// Required: false
	TrivyIgnoreUnfixed *string `json:"trivyIgnoreUnfixed,omitzero"`

	// ImagePatchSuffix is the suffix appended to the source tag when patching an image.
	//
	// Required: false
	ImagePatchSuffix *string `json:"imagePatchSuffix,omitzero"`

	// ImagePatchTimeoutSec is the timeout for a single image patch operation in seconds.
	//
	// Required: false
	ImagePatchTimeoutSec *string `json:"imagePatchTimeoutSec,omitzero"`

	// ImagePatchAllPlatforms patches every platform in a multi-platform image
	// instead of only the platform the server runs on.
	//
	// Required: false
	ImagePatchAllPlatforms *string `json:"imagePatchAllPlatforms,omitzero"`

	// ImageAutoPatchEnabled enables scheduled patching of images with fixable vulnerabilities.
	//
	// Required: false
	ImageAutoPatchEnabled *string `json:"imageAutoPatchEnabled,omitzero"`

	// ImageAutoPatchInterval is the cron expression for scheduled image patching.
	//
	// Required: false
	ImageAutoPatchInterval *string `json:"imageAutoPatchInterval,omitzero"`

	// OidcClientId is the OIDC client identifier.
	//
	// Required: false
	OidcClientId *string `json:"oidcClientId,omitzero"`

	// OidcClientSecret is the OIDC client secret.
	//
	// Required: false
	OidcClientSecret *string `json:"oidcClientSecret,omitzero"`

	// OidcIssuerUrl is the OIDC issuer URL.
	//
	// Required: false
	OidcIssuerUrl *string `json:"oidcIssuerUrl,omitzero"`

	// OidcScopes is the list of OIDC scopes to request.
	//
	// Required: false
	OidcScopes *string `json:"oidcScopes,omitzero"`

	// OidcGroupsClaim is the OIDC claim path read on every login to drive
	// role assignment via oidc_role_mappings. Default: "groups".
	//
	// Required: false
	OidcGroupsClaim *string `json:"oidcGroupsClaim,omitzero"`

	// OidcSkipTlsVerify indicates if TLS verification should be skipped for OIDC.
	//
	// Required: false
	OidcSkipTlsVerify *string `json:"oidcSkipTlsVerify,omitzero"`

	// OidcAutoRedirectToProvider indicates if the login page should automatically redirect to OIDC provider.
	//
	// Required: false
	OidcAutoRedirectToProvider *string `json:"oidcAutoRedirectToProvider,omitzero"`

	// OidcProviderName is the custom display name for the OIDC provider.
	//
	// Required: false
	OidcProviderName *string `json:"oidcProviderName,omitzero"`

	// OidcProviderLogoUrl is the custom logo URL for the OIDC provider.
	//
	// Required: false
	OidcProviderLogoUrl *string `json:"oidcProviderLogoUrl,omitzero"`

	// DockerApiTimeout is the timeout for Docker API operations in seconds.
	//
	// Required: false
	DockerApiTimeout *string `json:"dockerApiTimeout,omitzero"`

	// DockerImagePullTimeout is the timeout for Docker image pulls in seconds.
	//
	// Required: false
	DockerImagePullTimeout *string `json:"dockerImagePullTimeout,omitzero"`

	// TrivyScanTimeout is the timeout for Trivy image vulnerability scans in seconds.
	//
	// Required: false
	TrivyScanTimeout *string `json:"trivyScanTimeout,omitzero"`

	// GitOperationTimeout is the timeout for Git clone/fetch operations in seconds.
	//
	// Required: false
	GitOperationTimeout *string `json:"gitOperationTimeout,omitzero"`

	// HttpClientTimeout is the default timeout for HTTP requests in seconds.
	//
	// Required: false
	HttpClientTimeout *string `json:"httpClientTimeout,omitzero"`

	// RegistryTimeout is the timeout for container registry operations in seconds.
	//
	// Required: false
	RegistryTimeout *string `json:"registryTimeout,omitzero"`

	// ProxyRequestTimeout is the timeout for proxied requests to remote environments in seconds.
	//
	// Required: false
	ProxyRequestTimeout *string `json:"proxyRequestTimeout,omitzero"`

	// DeployWaitTimeout is the timeout waiting for services to become healthy or complete during a deploy in seconds.
	//
	// Required: false
	DeployWaitTimeout *string `json:"deployWaitTimeout,omitzero"`

	// AutoUpdateExcludedContainers is a comma-separated list of container names to exclude from auto-update.
	//
	// Required: false
	AutoUpdateExcludedContainers *string `json:"autoUpdateExcludedContainers,omitzero"`

	// AutoHealEnabled indicates if automatic container healing is enabled.
	//
	// Required: false
	AutoHealEnabled *string `json:"autoHealEnabled,omitzero"`

	// AutoHealInterval is the cron expression for how often to check container health.
	//
	// Required: false
	AutoHealInterval *string `json:"autoHealInterval,omitzero"`

	// AutoHealExcludedContainers is a comma-separated list of container names to exclude from auto-heal.
	//
	// Required: false
	AutoHealExcludedContainers *string `json:"autoHealExcludedContainers,omitzero"`

	// AutoHealMaxRestarts is the maximum number of auto-heal restarts per container within the restart window.
	//
	// Required: false
	AutoHealMaxRestarts *string `json:"autoHealMaxRestarts,omitzero"`

	// AutoHealRestartWindow is the time window in minutes for counting auto-heal restarts.
	//
	// Required: false
	AutoHealRestartWindow *string `json:"autoHealRestartWindow,omitzero"`

	// VolumeHelperIdleTimeout is the number of minutes a volume helper
	// container may sit idle before it is automatically removed (0 disables).
	//
	// Required: false
	VolumeHelperIdleTimeout *string `json:"volumeHelperIdleTimeout,omitzero"`

	// BuildProvider is the default build provider (local|depot).
	//
	// Required: false
	BuildProvider *string `json:"buildProvider,omitzero"`

	// BuildsDirectory is the root directory for manual build workspaces.
	//
	// Required: false
	BuildsDirectory *string `json:"buildsDirectory,omitzero"`

	// BuildTimeout is the timeout for BuildKit builds in seconds.
	//
	// Required: false
	BuildTimeout *string `json:"buildTimeout,omitzero"`

	// DepotProjectId is the Depot project identifier.
	//
	// Required: false
	DepotProjectId *string `json:"depotProjectId,omitzero"`

	// DepotToken is the Depot API token.
	//
	// Required: false
	DepotToken *string `json:"depotToken,omitzero"`
}
