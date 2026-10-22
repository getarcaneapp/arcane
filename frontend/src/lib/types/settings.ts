import type { JobRun, JobRunStatus, WorkerHealth } from './job';
import type { UpdateRequest } from './shared';

// --- Application settings ---

export type ApplicationTheme =
	| 'default'
	| 'graphite'
	| 'carbon'
	| 'ocean'
	| 'amber'
	| 'github'
	| 'nord'
	| 'everforest'
	| 'rosepine';
export type IconCatalog = 'selfhst' | 'dashboard-icons';

export type UpdateCheckRegistry = 'auto' | 'ghcr.io' | 'docker.io' | 'public.ecr.aws';

export type Settings = {
	projectsDirectory: string;
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
	dockerClientRefreshInterval?: string;
	environmentHealthInterval: number;
	activityHistoryRetentionDays: number;
	upgradeLogRetentionDays: number;
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
	projectWorkspaceMaxFileSizeMb?: number;
	volumeWorkspaceMaxFileSizeMb?: number;
	baseServerUrl: string;
	enableGravatar: boolean;
	experimentalFeaturesEnabled: boolean;
	developmentBrandingEnabled: boolean;
	apnsEnabled?: boolean;
	avatarMaxUploadSizeMb: number;
	allEnvironmentsLimit: number;
	defaultShell: string;
	dockerHost: string;
	autoInjectEnv: boolean;

	authLocalEnabled: boolean;
	authSessionTimeout: number;
	authPasswordPolicy: 'basic' | 'standard' | 'strong';
	defaultUserRoleId: string;
	auditRetentionDays: number;
	auditDenialsEnabled: boolean;
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
	buildProvider: 'local' | 'depot';
	buildsDirectory: string;
	buildTimeout: number;
	// Missing on agents that predate the setting.
	buildHistoryRetentionDays?: number;
	depotProjectId: string;
	depotToken?: string;
};

export type SettingKey = keyof Settings;

export interface SettingState {
	key: string;
	value: string;
	type: string;
	default: string;
	source: 'default' | 'db' | 'env';
	locked: boolean;
	lockReason?: 'env' | 'readOnly';
	sensitive: boolean;
	scope: 'manager' | 'environment';
	envName: string;
}

export interface SettingsRuntime {
	frontendTracingEnabled?: boolean;
	frontendMetricsEnabled?: boolean;
	frontendLogsEnabled?: boolean;
	backupVolumeName?: string;
	edgeMTLSManagerCAAvailable?: boolean;
	depotConfigured?: boolean;
}

export interface SettingsSnapshot {
	settings: SettingState[];
	runtime: SettingsRuntime;
	readOnly: boolean;
}

export interface ResolvedSettings {
	values: Settings;
	meta: Partial<Record<SettingKey, SettingState>>;
	runtime: SettingsRuntime;
	readOnly: boolean;
}

export interface OidcStatusInfo {
	mergeAccounts: boolean;
	providerName: string;
	providerLogoUrl: string;
}

// --- App version info ---

export interface AppVersionInformation {
	currentVersion: string;
	currentTag?: string;
	currentDigest?: string;
	displayVersion: string;
	revision: string;
	shortRevision: string;
	goVersion: string;
	nodeVersion: string;
	svelteKitVersion: string;
	enabledFeatures?: string[];
	buildTime?: string;
	isSemverVersion: boolean;
	newestVersion?: string;
	newestDigest?: string;
	updateAvailable?: boolean;
	releaseUrl?: string;
	releaseNotes?: string;
	releasedAt?: string;
}

// --- Job schedules ---

export type JobScheduleConfig = {
	environmentHealthInterval: string;
	eventCleanupInterval: string;
	expiredSessionsCleanupInterval: string;
	autoUpdateInterval: string;
	dockerClientRefreshInterval: string;
	pollingInterval: string;
	scheduledPruneInterval: string;
	vulnerabilityScanInterval: string;
	imageAutoPatchInterval: string;
	autoHealInterval: string;
};

export type JobScheduleConfigUpdate = UpdateRequest<JobScheduleConfig, keyof JobScheduleConfig>;

export type JobPrerequisite = {
	settingKey: string;
	label: string;
	isMet: boolean;
	settingsUrl?: string;
};

export type JobStatus = {
	currentRun?: JobRun;
	lastRun?: JobRun;
	lastSuccess?: string;
	lastError?: string;
	workerHealth?: WorkerHealth;
	children?: JobStatus[];
	id: string;
	name: string;
	description: string;
	category: string;
	schedule: string;
	nextRun?: string;
	enabled: boolean;
	managerOnly: boolean;
	isContinuous: boolean;
	canRunManually: boolean;
	prerequisites: JobPrerequisite[];
	settingsKey?: string;
};

export type JobListResponse = {
	durableRuns: boolean;
	observedAt: string;
	offline?: boolean;
	jobs: JobStatus[];
	isAgent: boolean;
};

export type JobRunResponse = {
	runId: string;
	status: JobRunStatus;
	success: boolean;
	message: string;
};
