import { refreshAll } from '$app/navigation';
import { page } from '$app/state';
import { createQueries, useQueryClient } from '@tanstack/svelte-query';

import { environmentSettingsQuery } from '#lib/query/environments.js';
import { queryKeys } from '#lib/query/query-keys.js';
import { settingsService } from '#lib/services/settings-service.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import type { ResolvedSettings, SettingKey, Settings } from '#lib/types/settings.js';
import { hasPermission } from '#lib/utils/auth.js';
import { isEnvironmentOnline } from '#lib/utils/docker.js';
import { settleAcrossEnvironments } from '#lib/utils/environments.js';

export function useEnvironmentSettingsScope(permissions: () => string[]) {
	const queryClient = useQueryClient();
	const selectedId = $derived(page.url.searchParams.get('env') ?? '');
	const readable = $derived(
		environmentStore.shown.filter((env) =>
			['settings:read', ...permissions()].every((permission) => hasPermission(permission, env.id))
		)
	);
	const candidates = $derived(
		environmentStore.available.filter(
			(env) => readable.some((ref) => ref.id === env.id) && (!selectedId || env.id === selectedId)
		)
	);
	const online = $derived(candidates.filter(isEnvironmentOnline));
	const results = createQueries(() => ({
		queries: online.map((env) => ({
			...environmentSettingsQuery(env.id, true),
			refetchOnWindowFocus: false,
			refetchOnReconnect: false
		}))
	}));
	const entries = $derived(
		online.flatMap((environment, index) => {
			const settings = results[index]?.data;
			return settings ? [{ environment, settings }] : [];
		})
	);
	const first = $derived(entries[0]?.settings);
	const mixedKeys = $derived(
		new Set(
			(Object.keys(first?.values ?? {}) as SettingKey[]).filter(
				(key) =>
					first?.meta[key]?.scope === 'environment' && entries.some(({ settings }) => settings.values[key] !== first.values[key])
			)
		)
	);

	function environmentScoped(settings: ResolvedSettings, changes: Partial<Settings>): Partial<Settings> {
		return Object.fromEntries(
			Object.entries(changes).filter(([key]) => settings.meta[key as SettingKey]?.scope === 'environment')
		) as Partial<Settings>;
	}

	async function save(changes: Partial<Settings>) {
		const managerEnvironmentId = entries[0]?.environment.id;
		await settleAcrossEnvironments(
			entries.map(({ environment }) => environment),
			(environmentId) => {
				const settings = entries.find(({ environment }) => environment.id === environmentId)!.settings;
				return settingsService.updateSettingsForEnvironment(
					environmentId,
					settings,
					environmentId === managerEnvironmentId ? changes : environmentScoped(settings, changes)
				);
			}
		);
		await Promise.all(
			entries.flatMap(({ environment }) => [
				queryClient.invalidateQueries({ queryKey: queryKeys.environments.settings(environment.id) }),
				queryClient.invalidateQueries({ queryKey: queryKeys.jobs.list(environment.id) })
			])
		);
		await queryClient.invalidateQueries({ queryKey: queryKeys.environments.all });
		await refreshAll();
	}

	return {
		get selectedId() {
			return selectedId;
		},
		get environments() {
			return readable;
		},
		get entries() {
			return entries;
		},
		get isLoading() {
			return results.some((result) => result.isPending);
		},
		get offlineCount() {
			return candidates.length - entries.length - results.filter((result) => result.isPending).length;
		},
		get primaryId() {
			return entries[0]?.environment.id ?? selectedId;
		},
		get version() {
			return [selectedId, ...results.map((result) => result.dataUpdatedAt)].join(':');
		},
		get settings() {
			return first;
		},
		get values() {
			return first?.values;
		},
		mixed: (key: SettingKey) => mixedKeys.has(key),
		lock: (key: SettingKey) => entries.find(({ settings }) => settings.meta[key]?.locked)?.settings.meta[key],
		save
	};
}
