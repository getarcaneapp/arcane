import { LOCAL_DOCKER_ENVIRONMENT_ID } from '#lib/stores/environment.store.svelte.js';
import userStore from '#lib/stores/user-store.svelte.js';
import { GLOBAL_SCOPE } from '#lib/types/role.js';
import type { ProjectsDirectoryEntry, SettingKey, SettingState, Settings, SettingsSnapshot } from '#lib/types/settings.js';
import type { ResolvedSettings } from '#lib/types/ui/settings.js';

import BaseAPIService from './api-service';

export function parseSettingValue(type: string, value: string): string | number | boolean | ProjectsDirectoryEntry[] {
	switch (type) {
		case 'json':
			return JSON.parse(value) as ProjectsDirectoryEntry[];
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
		const publicMain = this.getPublicSettingsForEnvironment(LOCAL_DOCKER_ENVIRONMENT_ID);
		const [main, manager, authentication, environment] = await Promise.all([
			publicMain,
			userStore.hasPermission('settings:read', GLOBAL_SCOPE) ? this.getManagerSettings() : undefined,
			userStore.isGlobalAdmin() ? this.getAuthenticationSettings() : undefined,
			this.getEnvironmentSettingsOrPublic(environmentId, publicMain)
		]);
		return this.merge(environment, main, manager, authentication);
	}

	async getSettingsForEnvironment(environmentId: string): Promise<ResolvedSettings> {
		const res = await this.api.get<SettingsSnapshot>(`/agents/${environmentId}/settings`);
		return this.normalize(res.data);
	}

	async getManagerSettings(): Promise<ResolvedSettings> {
		const res = await this.api.get<SettingsSnapshot>('/settings');
		return this.normalize(res.data);
	}

	async getAuthenticationSettings(): Promise<ResolvedSettings> {
		const res = await this.api.get<SettingsSnapshot>('/settings/authentication');
		return this.normalize(res.data);
	}

	async getPublicSettingsForEnvironment(environmentId: string, signal?: AbortSignal): Promise<ResolvedSettings> {
		const res = await this.api.get<SettingsSnapshot>(`/agents/${environmentId}/settings/public`, { signal });
		return this.normalize(res.data);
	}

	async updateSettingsForEnvironment(
		environmentId: string,
		base: ResolvedSettings,
		changes: Partial<Settings>
	): Promise<ResolvedSettings> {
		const patches: Record<SettingState['scope'], Record<string, string>> = { manager: {}, authentication: {}, environment: {} };
		for (const [key, value] of Object.entries(changes)) {
			const meta = base.meta[key as SettingKey];
			const serialized = typeof value === 'object' ? JSON.stringify(value) : String(value);
			if (!meta || meta.locked || value === undefined || serialized === meta.value) continue;
			patches[meta.scope][key] = serialized;
		}
		const endpoints: Record<SettingState['scope'], string> = {
			manager: '/settings',
			authentication: '/settings/authentication',
			environment: `/agents/${environmentId}/settings`
		};
		const pending = (Object.keys(patches) as SettingState['scope'][]).filter((scope) => Object.keys(patches[scope]).length > 0);
		if (pending.length === 0) return base;
		for (const scope of pending) await this.api.patch(endpoints[scope], patches[scope]);
		return this.getSettingsForEnvironmentMerged(environmentId);
	}

	private async getEnvironmentSettingsOrPublic(
		environmentId: string,
		publicMain: Promise<ResolvedSettings>
	): Promise<ResolvedSettings> {
		if (userStore.hasPermission('environment-settings:read', environmentId)) return this.getSettingsForEnvironment(environmentId);
		if (environmentId === LOCAL_DOCKER_ENVIRONMENT_ID) return publicMain;
		return this.getPublicSettingsForEnvironment(environmentId);
	}

	private merge(
		environment: ResolvedSettings,
		main: ResolvedSettings,
		manager: ResolvedSettings | undefined,
		authentication: ResolvedSettings | undefined
	): ResolvedSettings {
		const appWideMeta = new Map<string, SettingState>();
		for (const source of [main, manager, authentication]) {
			for (const meta of Object.values(source?.meta ?? {})) {
				if (meta.scope !== 'environment') appWideMeta.set(meta.key, meta);
			}
		}
		const managerRuntime = (manager ?? main).runtime;
		return this.normalize({
			settings: [...Object.values(environment.meta).filter((meta) => meta.scope === 'environment'), ...appWideMeta.values()],
			runtime: {
				...environment.runtime,
				frontendTracingEnabled: managerRuntime.frontendTracingEnabled,
				frontendMetricsEnabled: managerRuntime.frontendMetricsEnabled,
				frontendLogsEnabled: managerRuntime.frontendLogsEnabled,
				agentMTLSManagerCAAvailable: managerRuntime.agentMTLSManagerCAAvailable
			},
			readOnly: [environment, main, manager, authentication].some((source) => source?.readOnly)
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
