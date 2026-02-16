import type { TemplateRegistryConfig } from './template.type';

export type Settings = {
	projectsDirectory: string;
	diskUsagePath: string;
	autoUpdate: boolean;
	autoUpdateInterval: number;
	autoUpdateExcludedContainers?: string;
	pollingEnabled: boolean;
	pollingInterval: number;
	environmentHealthInterval: number;
	dockerPruneMode: 'all' | 'dangling';
	scheduledPruneEnabled?: boolean;
	scheduledPruneInterval?: number;
	scheduledPruneContainers?: boolean;
	scheduledPruneImages?: boolean;
	scheduledPruneVolumes?: boolean;
	scheduledPruneNetworks?: boolean;
	scheduledPruneBuildCache?: boolean;
	vulnerabilityScanEnabled?: boolean;
	vulnerabilityScanInterval?: number;
	maxImageUploadSize: number;
	baseServerUrl: string;
	enableGravatar: boolean;
	uiConfigDisabled: boolean;
	defaultShell: string;
	dockerHost: string;
	accentColor: string;
	autoInjectEnv: boolean;
	backupVolumeName?: string;

	authLocalEnabled: boolean;
	authSessionTimeout: number;
	authPasswordPolicy: 'basic' | 'standard' | 'strong';
	trivyImage: string;
	trivyResourceLimitsEnabled: boolean;
	trivyCpuLimit: number;
	trivyMemoryLimitMb: number;
	oidcEnabled: boolean;
	oidcClientId: string;
	oidcClientSecret?: string;
	oidcIssuerUrl: string;
	oidcScopes: string;
	oidcAdminClaim: string;
	oidcAdminValue: string;
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
