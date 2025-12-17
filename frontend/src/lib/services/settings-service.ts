import BaseAPIService from './api-service';
import type { Settings, OidcStatusInfo } from '$lib/types/settings.type';
import { environmentStore } from '$lib/stores/environment.store.svelte';
import { isLocalSetting, extractLocalSettings, extractEnvironmentSettings } from '$lib/utils/settings.util';

type KeyValuePair = { key: string; value: string };

export default class SettingsService extends BaseAPIService {
	async getSettings(): Promise<Settings> {
		// Wait for environment store to be initialized before fetching settings
		await environmentStore.ready;
		const envId = await environmentStore.getCurrentEnvironmentId();

		// If we're on environment 0, just get all settings normally
		if (envId === '0') {
			const res = await this.api.get(`/environments/0/settings`);
			return this.normalize(res.data);
		}

		// For remote environments, merge:
		// - UI settings from environment 0 (main instance)
		// - Environment-specific settings from current environment
		const [mainSettings, envSettings] = await Promise.all([
			this.api.get('/environments/0/settings'),
			this.api.get(`/environments/${envId}/settings`)
		]);

		const mainNormalized = this.normalize(mainSettings.data);
		const envNormalized = this.normalize(envSettings.data);

		// Extract UI settings from main, environment settings from current env
		const uiSettings = extractLocalSettings(mainNormalized);
		const operationalSettings = extractEnvironmentSettings(envNormalized);

		// Merge them
		return { ...operationalSettings, ...uiSettings } as Settings;
	}

	async getSettingsForEnvironment(environmentId: string): Promise<Settings> {
		// When viewing a specific environment's settings page, show all its settings
		const res = await this.api.get(`/environments/${environmentId}/settings`);
		return this.normalize(res.data);
	}

	async getPublicSettings(): Promise<Settings> {
		const res = await this.api.get(`/environments/0/settings/public`);
		return this.normalize(res.data);
	}

	async updateSettings(settings: Partial<Settings>) {
		// Wait for environment store to be initialized before updating settings
		await environmentStore.ready;
		const envId = await environmentStore.getCurrentEnvironmentId();

		const uiSettings: Record<string, any> = {};
		const envSettings: Record<string, any> = {};

		// Separate UI settings from environment settings
		for (const key in settings) {
			if (isLocalSetting(key)) {
				uiSettings[key] = settings[key as keyof Settings];
			} else {
				envSettings[key] = settings[key as keyof Settings];
			}
		}

		// Update UI settings on environment 0 (main instance)
		if (Object.keys(uiSettings).length > 0) {
			const payload: Record<string, string> = {};
			for (const key in uiSettings) {
				const v = uiSettings[key];
				payload[key] = typeof v === 'object' && v !== null ? JSON.stringify(v) : String(v);
			}
			await this.api.put('/environments/0/settings', payload);
		}

		// Update environment settings on current environment
		if (Object.keys(envSettings).length > 0) {
			const payload: Record<string, string> = {};
			for (const key in envSettings) {
				const v = envSettings[key];
				payload[key] = typeof v === 'object' && v !== null ? JSON.stringify(v) : String(v);
			}
			await this.api.put(`/environments/${envId}/settings`, payload);
		}

		// Reload and return merged settings
		return this.getSettings();
	}

	async getOidcStatus(): Promise<OidcStatusInfo> {
		return this.handleResponse(this.api.get('/oidc/status'));
	}

	private normalize(data: any): Settings {
		if (data && !Array.isArray(data) && !Array.isArray(data?.settings)) {
			return data as Settings;
		}

		let list: KeyValuePair[] = [];
		if (Array.isArray(data)) list = data;
		else if (Array.isArray(data?.settings)) list = data.settings;

		const settings: Record<string, unknown> = {};
		list.forEach(({ key, value }) => {
			settings[key] = this.parseValue(key, value);
		});
		return settings as Settings;
	}

	private parseValue(key: string, value: string) {
		if (key === 'registryCredentials' || key === 'templateRegistries') {
			try {
				return JSON.parse(value);
			} catch {
				return [];
			}
		}
		if (value === 'true') return true;
		if (value === 'false') return false;
		if (/^-?\d+(\.\d+)?$/.test(value)) return Number(value);
		return value;
	}
}

export const settingsService = new SettingsService();
