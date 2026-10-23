import userStore from '#lib/stores/user-store.svelte.js';
import type {
	OidcStatusInfo,
	ResolvedSettings,
	SettingKey,
	SettingState,
	Settings,
	SettingsSnapshot
} from '#lib/types/settings.js';

import BaseAPIService from './api-service';

export function parseSettingValue(type: string, value: string): string | number | boolean {
	switch (type) {
		case 'boolean':
			return value === 'true';
		case 'integer':
		case 'number':
			return Number(value);
		default:
			return value;
	}
}

class SettingsService extends BaseAPIService {
	async getSettingsForEnvironmentMerged(environmentId: string): Promise<ResolvedSettings> {
		if (environmentId === '0') return this.getSettingsForEnvironment('0');
		const [main, environment] = await Promise.all([
			userStore.hasPermission('settings:read', '0')
				? this.getSettingsForEnvironment('0')
				: this.getPublicSettingsForEnvironment('0'),
			this.getSettingsForEnvironment(environmentId)
		]);
		return this.merge(main, environment);
	}

	async getSettingsForEnvironment(environmentId: string): Promise<ResolvedSettings> {
		const res = await this.api.get<SettingsSnapshot>(`/environments/${environmentId}/settings`);
		return this.normalize(res.data);
	}

	async getPublicSettingsForEnvironment(environmentId: string, signal?: AbortSignal): Promise<ResolvedSettings> {
		const res = await this.api.get<SettingsSnapshot>(`/environments/${environmentId}/settings/public`, { signal });
		return this.normalize(res.data);
	}

	async getPublicSettings(environmentId = '0'): Promise<ResolvedSettings> {
		if (environmentId === '0') return this.getPublicSettingsForEnvironment('0');
		const [main, environment] = await Promise.all([
			this.getPublicSettingsForEnvironment('0'),
			this.getPublicSettingsForEnvironment(environmentId)
		]);
		return this.merge(main, environment);
	}

	async getReadableSettings(environmentId: string): Promise<ResolvedSettings> {
		if (!userStore.hasPermission('settings:read', environmentId)) return this.getPublicSettings(environmentId);
		return this.getSettingsForEnvironmentMerged(environmentId);
	}

	async updateSettingsForEnvironment(
		environmentId: string,
		base: ResolvedSettings,
		changes: Partial<Settings>
	): Promise<ResolvedSettings> {
		const patches: Record<SettingState['scope'], Record<string, string>> = { manager: {}, environment: {} };
		for (const [key, value] of Object.entries(changes)) {
			const meta = base.meta[key as SettingKey];
			if (!meta || meta.locked || value === undefined || String(value) === meta.value) continue;
			patches[meta.scope][key] = String(value);
		}
		const hasManagerChanges = Object.keys(patches.manager).length > 0;
		const hasEnvironmentChanges = Object.keys(patches.environment).length > 0;
		if (!hasManagerChanges && !hasEnvironmentChanges) return base;
		if (hasManagerChanges) await this.api.patch('/environments/0/settings', patches.manager);
		if (hasEnvironmentChanges) await this.api.patch(`/environments/${environmentId}/settings`, patches.environment);
		return this.getReadableSettings(environmentId);
	}

	async getOidcStatus(): Promise<OidcStatusInfo> {
		return this.handleResponse(this.api.get('/oidc/status'));
	}

	private merge(main: ResolvedSettings, environment: ResolvedSettings): ResolvedSettings {
		const scoped = (source: ResolvedSettings, scope: SettingState['scope']) =>
			Object.values(source.meta).filter((meta) => meta.scope === scope);
		return this.normalize({
			settings: [...scoped(environment, 'environment'), ...scoped(main, 'manager')],
			runtime: {
				...environment.runtime,
				frontendTracingEnabled: main.runtime.frontendTracingEnabled,
				frontendMetricsEnabled: main.runtime.frontendMetricsEnabled,
				frontendLogsEnabled: main.runtime.frontendLogsEnabled,
				edgeMTLSManagerCAAvailable: main.runtime.edgeMTLSManagerCAAvailable
			},
			readOnly: main.readOnly || environment.readOnly
		});
	}

	private normalize(snapshot: SettingsSnapshot): ResolvedSettings {
		return {
			values: Object.fromEntries(
				snapshot.settings.map((meta) => [meta.key, parseSettingValue(meta.type, meta.value)])
			) as Settings,
			meta: Object.fromEntries(snapshot.settings.map((meta) => [meta.key, meta])),
			runtime: snapshot.runtime,
			readOnly: snapshot.readOnly
		};
	}
}

export const settingsService = new SettingsService();
