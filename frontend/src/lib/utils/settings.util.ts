export type LocalSettings = {
	accentColor: string;
	mobileNavigationMode: string;
	mobileNavigationShowLabels: boolean;
	sidebarHoverExpansion: boolean;
};

const LOCAL_SETTING_KEYS = new Set([
	'accentColor',
	'mobileNavigationMode',
	'mobileNavigationShowLabels',
	'sidebarHoverExpansion',
	'authLocalEnabled',
	'authSessionTimeout',
	'authPasswordPolicy',
	'authOidcConfig',
	'oidcEnabled',
	'oidcMergeAccounts',
	'oidcSkipTlsVerify',
	'oidcAutoRedirectToProvider',
	'oidcClientId',
	'oidcClientSecret',
	'oidcIssuerUrl',
	'oidcScopes',
	'oidcAdminClaim',
	'oidcAdminValue',
	'oidcProviderName',
	'oidcProviderLogoUrl'
]);

export function isLocalSetting(key: string): boolean {
	return LOCAL_SETTING_KEYS.has(key);
}

export function extractLocalSettings(settings: Record<string, any>): Partial<LocalSettings> {
	const local: Partial<LocalSettings> = {};
	for (const key of LOCAL_SETTING_KEYS) {
		if (key in settings) {
			local[key as keyof LocalSettings] = settings[key];
		}
	}
	return local;
}

export function extractEnvironmentSettings(settings: Record<string, any>): Record<string, any> {
	const env: Record<string, any> = {};
	for (const key in settings) {
		if (!LOCAL_SETTING_KEYS.has(key)) {
			env[key] = settings[key];
		}
	}
	return env;
}
