import type { TemplateRegistryConfig } from './template.type';

export type Settings = {
	projectsDirectory: string;
	diskUsagePath: string;
	autoUpdate: boolean;
	autoUpdateInterval: number;
	pollingEnabled: boolean;
	pollingInterval: number;
	environmentHealthInterval: number;
	dockerPruneMode: 'all' | 'dangling';
	maxImageUploadSize: number;
	baseServerUrl: string;
	enableGravatar: boolean;
	uiConfigDisabled: boolean;
	defaultShell: string;
	dockerHost: string;
	accentColor: string;
	autoInjectEnv: boolean;

	authLocalEnabled: boolean;
	authSessionTimeout: number;
	authPasswordPolicy: 'basic' | 'standard' | 'strong';
	oidcEnabled: boolean;
	oidcClientId: string;
	oidcClientSecret?: string;
	oidcIssuerUrl: string;
	oidcScopes: string;
	oidcAdminClaim: string;
	oidcAdminValue: string;
	oidcMergeAccounts: boolean;

	mobileNavigationMode: 'floating' | 'docked';
	mobileNavigationShowLabels: boolean;
	sidebarHoverExpansion: boolean;

	glassEffectEnabled: boolean;

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
}
