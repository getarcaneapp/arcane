import type { TemplateRegistryConfig } from './template.type';

export type SettingRawResponse = {
	key: string;
	type: string;
	value: string;
	isPublic?: boolean;
}[];

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

	authLocalEnabled: boolean;
	oidcEnabled: boolean;
	oidcMergeAccounts: boolean;
	authSessionTimeout: number;
	authPasswordPolicy: 'basic' | 'standard' | 'strong';
	oidcClientId: string;
	oidcClientSecret?: string;
	oidcIssuerUrl: string;
	oidcScopes: string;
	oidcAdminClaim: string;
	oidcAdminValue: string;

	onboardingCompleted: boolean;
	onboardingSteps: {
		welcome?: boolean;
		password?: boolean;
		docker?: boolean;
		security?: boolean;
		settings?: boolean;
	};

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

export interface OidcConfig {
	clientId: string;
	clientSecret?: string;
	issuerUrl: string;
	scopes: string;

	adminClaim?: string; // e.g., "roles", "groups", "realm_access.roles", "admin"
	adminValue?: string; // e.g., "admin" (comma-separated accepted values)
}

export interface OidcStatusInfo {
	envForced: boolean;
	envConfigured: boolean;
	mergeAccounts: boolean;
}
