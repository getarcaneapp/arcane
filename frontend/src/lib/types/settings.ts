import type { TemplateRegistryConfig } from './swarm';

// --- Application settings ---

export type ApplicationTheme = 'default' | 'graphite' | 'ocean' | 'amber' | 'github' | 'nord' | 'everforest' | 'rosepine';
export type IconCatalog = 'selfhst' | 'dashboard-icons';

export type Settings = {
	projectsDirectory: string;
	templatesDirectory: string;
	followProjectSymlinks: boolean;
	swarmStackSourcesDirectory: string;
	diskUsagePath: string;
	autoUpdate: boolean;
	autoUpdateInterval: number;
	autoUpdateExcludedContainers?: string;
	pollingEnabled: boolean;
	pollingInterval: number;
	dockerClientRefreshInterval?: string;
	environmentHealthInterval: number;
	activityHistoryRetentionDays: number;
	activityHistoryMaxEntries: number;
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
	vulnerabilityScanInterval?: number;
	autoHealEnabled?: boolean;
	autoHealExcludedContainers?: string;
	autoHealMaxRestarts?: number;
	autoHealRestartWindow?: number;
	volumeBrowserHelperIdleTimeout?: number;
	maxImageUploadSize: number;
	gitSyncMaxFiles: number;
	gitSyncMaxTotalSizeMb: number;
	gitSyncMaxBinarySizeMb: number;
	baseServerUrl: string;
	enableGravatar: boolean;
	uiConfigDisabled: boolean;
	defaultShell: string;
	dockerHost: string;
	applicationTheme: ApplicationTheme;
	iconCatalog: IconCatalog;
	accentColor: string;
	oledMode: boolean;
	autoInjectEnv: boolean;
	backupVolumeName?: string;
	edgeMTLSManagerCAAvailable?: boolean;

	authLocalEnabled: boolean;
	authSessionTimeout: number;
	authPasswordPolicy: 'basic' | 'standard' | 'strong';
	trivyImage: string;
	trivyNetwork: string;
	trivySecurityOpts: string;
	trivyPrivileged: boolean;
	trivyPreserveCacheOnVolumePrune: boolean;
	trivyResourceLimitsEnabled: boolean;
	trivyCpuLimit: number;
	trivyMemoryLimitMb: number;
	trivyConcurrentScanContainers: number;
	trivyServerEnabled: boolean;
	trivyServerUrl: string;
	trivyServerToken: string;
	trivyIgnoreUnfixed: boolean;
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

	mobileNavigationMode: 'floating' | 'docked';
	mobileNavigationShowLabels: boolean;
	sidebarHoverExpansion: boolean;
	keyboardShortcutsEnabled: boolean;

	dockerApiTimeout: number;
	dockerImagePullTimeout: number;
	trivyScanTimeout: number;
	gitOperationTimeout: number;
	httpClientTimeout: number;
	registryTimeout: number;
	proxyRequestTimeout: number;
	buildProvider: 'local' | 'depot';
	buildsDirectory: string;
	buildTimeout: number;
	depotProjectId: string;
	depotToken?: string;
	depotConfigured?: boolean;

	registryCredentials: RegistryCredential[];
	templateRegistries: TemplateRegistryConfig[];
};

export interface RegistryCredential {
	url: string;
	username: string;
	password: string;
}

export interface OidcStatusInfo {
	envForced: boolean;
	envConfigured: boolean;
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

export type JobSchedules = {
	environmentHealthInterval: string;
	eventCleanupInterval: string;
	expiredSessionsCleanupInterval: string;
	autoUpdateInterval: string;
	dockerClientRefreshInterval: string;
	pollingInterval: string;
	scheduledPruneInterval: string;
	vulnerabilityScanInterval: string;
	autoHealInterval: string;
};

export type JobSchedulesUpdate = Partial<JobSchedules>;

export type JobPrerequisite = {
	settingKey: string;
	label: string;
	isMet: boolean;
	settingsUrl?: string;
};

export type JobStatus = {
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
	jobs: JobStatus[];
	isAgent: boolean;
};

export type JobRunResponse = {
	success: boolean;
	message: string;
};
