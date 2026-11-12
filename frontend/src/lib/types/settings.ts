export type UpdateCheckRegistry = 'auto' | 'ghcr.io' | 'docker.io' | 'public.ecr.aws';

export type ProjectsDirectoryEntry = {
	path: string;
	enabled: boolean;
};

export type Settings = {
	projectsDirectory: string;
	projectsDirectories: ProjectsDirectoryEntry[];
	templatesDirectory: string;
	followProjectSymlinks: boolean;
	swarmStackSourcesDirectory: string;
	diskUsagePath: string;
	autoUpdate: boolean;
	autoUpdateInterval: number;
	autoUpdateDelay?: number;
	autoUpdateExcludedContainers?: string;
	pollingEnabled: boolean;
	imageEventWatcherEnabled: boolean;
	pollingInterval: number;
	githubToken?: string;
	dockerClientRefreshInterval?: string;
	environmentHealthInterval: number;
	activityHistoryRetentionDays: number;
	upgradeLogRetentionDays: number;
	upgradeMaxParallelAgents: number;
	activityHistoryMaxEntries: number;
	maxConcurrentActivities: number;
	defaultDeployPullPolicy: 'missing' | 'always' | 'never';
	scheduledPruneEnabled?: boolean;
	scheduledPruneInterval?: number;
	scheduledPruneContainers?: boolean;
	scheduledPruneImages?: boolean;
	scheduledPruneVolumes?: boolean;
	scheduledPruneNetworks?: boolean;
	scheduledPruneBuildCache?: boolean;
	pruneContainerMode?: 'none' | 'stopped' | 'olderThan';
	pruneContainerUntil?: string;
	pruneImageMode?: 'none' | 'dangling' | 'all' | 'olderThan';
	pruneImageUntil?: string;
	pruneVolumeMode?: 'none' | 'anonymous' | 'all';
	pruneNetworkMode?: 'none' | 'unused' | 'olderThan';
	pruneNetworkUntil?: string;
	pruneBuildCacheMode?: 'none' | 'unused' | 'all' | 'olderThan';
	pruneBuildCacheUntil?: string;
	vulnerabilityScanEnabled?: boolean;
	featureVulnerabilityManagementEnabled?: boolean;
	featureSwarmEnabled?: boolean;
	vulnerabilityScanInterval?: number;
	autoHealEnabled?: boolean;
	autoHealExcludedContainers?: string;
	autoHealMaxRestarts?: number;
	autoHealRestartWindow?: number;
	volumeHelperIdleTimeout?: number;
	maxImageUploadSize: number;
	gitSyncMaxFiles: number;
	gitSyncMaxTotalSizeMb: number;
	gitSyncMaxBinarySizeMb: number;
	unmanagedBackupRetentionDays: number;
	projectWorkspaceMaxFileSizeMb?: number;
	volumeWorkspaceMaxFileSizeMb?: number;
	baseServerUrl: string;
	enableGravatar: boolean;
	experimentalFeaturesEnabled: boolean;
	developmentBrandingEnabled: boolean;
	customLogoHash?: string;
	customLogoFullHash?: string;
	customFaviconHash?: string;
	customPwaIconHash?: string;
	apnsEnabled?: boolean;
	notificationsIncludeReleaseLinks?: boolean;
	avatarMaxUploadSizeMb: number;
	allEnvironmentsLimit: number;
	defaultShell: string;
	dockerHost: string;
	autoInjectEnv: boolean;

	authLocalEnabled: boolean;
	authSessionTimeout: number;
	authPasswordMinLength: number;
	authPasswordRequireUppercase: boolean;
	authPasswordRequireLowercase: boolean;
	authPasswordRequireNumber: boolean;
	authPasswordRequireSymbol: boolean;
	hstsEnabled: boolean;
	toolsImageRegistry: 'ghcr.io' | 'docker.io';
	updateCheckRegistry: UpdateCheckRegistry;
	trivyDbRegistry: 'ghcr.io' | 'docker.io';
	trivyNetwork: string;
	trivySecurityOpts: string;
	trivyPrivileged: boolean;
	trivyResourceLimitsEnabled: boolean;
	trivyCpuLimit: number;
	trivyMemoryLimitMb: number;
	trivyConcurrentScanContainers: number;
	trivyServerEnabled: boolean;
	trivyServerUrl: string;
	trivyServerToken: string;
	trivyIgnoreUnfixed: boolean;
	vulnerabilityThreatIntelEnabled: boolean;
	trivyConfig: string;
	trivyIgnore: string;
	imagePatchSuffix: string;
	imagePatchTimeoutSec: number;
	imagePatchAllPlatforms: boolean;
	imageAutoPatchEnabled: boolean;
	imageAutoPatchInterval: string;
	lifecycleEnabled: boolean;
	lifecycleDefaultRunnerImage: string;
	lifecycleMaxTimeoutSec: number;
	oidcEnabled: boolean;
	oidcClientId: string;
	oidcClientSecret?: string;
	oidcIssuerUrl: string;
	oidcScopes: string;
	oidcGroupsClaim: string;
	oidcSkipTlsVerify: boolean;
	oidcAutoRedirectToProvider: boolean;
	oidcMergeAccounts: boolean;
	oidcProviderName: string;
	oidcProviderLogoUrl: string;

	dockerApiTimeout: number;
	dockerImagePullTimeout: number;
	deployWaitTimeout: number;
	trivyScanTimeout: number;
	gitOperationTimeout: number;
	httpClientTimeout: number;
	registryTimeout: number;
	registryTagTimeout: number;
	proxyRequestTimeout: number;
	http2ReceiveWindowMb?: number;
	agentTunnelStreamWindowMb?: number;
	buildProvider: 'local' | 'depot';
	buildsDirectory: string;
	buildTimeout: number;
	// Missing on agents that predate the setting.
	buildHistoryRetentionDays?: number;
	depotProjectId: string;
	depotToken?: string;
};

export type SettingKey = keyof Settings;

export type BrandImageSlot = 'mark' | 'full' | 'favicon' | 'pwa';

export interface SettingState {
	key: string;
	value: string;
	type: string;
	default: string;
	source: 'default' | 'db' | 'env';
	locked: boolean;
	lockReason?: 'env' | 'readOnly';
	sensitive: boolean;
	scope: 'manager' | 'authentication' | 'environment';
	envName: string;
}

export interface SettingsRuntime {
	frontendTracingEnabled?: boolean;
	frontendMetricsEnabled?: boolean;
	frontendLogsEnabled?: boolean;
	backupVolumeName?: string;
	agentMTLSManagerCAAvailable?: boolean;
	depotConfigured?: boolean;
}

export interface SettingsSnapshot {
	settings: SettingState[];
	runtime: SettingsRuntime;
	readOnly: boolean;
}
