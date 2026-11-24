import { queryOptions, type QueryClient } from '@tanstack/svelte-query';

import { queryKeys } from '#lib/query/query-keys.js';
import { environmentManagementService } from '#lib/services/env-mgmt-service.js';
import { emptySettings, settingsService } from '#lib/services/settings-service.js';
import type { EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
import { readEachEnvironment } from '#lib/utils/environments.js';

export function environmentListQuery(requestOptions: SearchPaginationSortRequest) {
	return queryOptions({
		queryKey: queryKeys.environments.list(requestOptions),
		queryFn: () => environmentManagementService.getEnvironments(requestOptions)
	});
}

export function environmentDetailQuery(environmentId: string) {
	return queryOptions({
		queryKey: queryKeys.environments.detail(environmentId),
		queryFn: () => environmentManagementService.get(environmentId)
	});
}

export function environmentSettingsQuery(environmentId: string, canReadSettings: boolean) {
	return queryOptions({
		queryKey: queryKeys.environments.settings(environmentId),
		queryFn: () => {
			if (canReadSettings) return settingsService.getSettingsForEnvironment(environmentId);
			return settingsService.getPublicSettingsForEnvironment(environmentId);
		}
	});
}

export function enrollmentTokensQuery() {
	return queryOptions({
		queryKey: queryKeys.environments.enrollmentTokens,
		queryFn: () => environmentManagementService.listEnrollmentTokens()
	});
}

export function environmentSettingsAcrossQuery(queryClient: QueryClient, environments: EnvironmentRef[]) {
	return queryOptions({
		queryKey: queryKeys.environments.settingsAcross(environments.map((env) => env.id).join(',')),
		queryFn: async () => {
			const sum = await readEachEnvironment(
				environments,
				(scope, config) => settingsService.getSettingsForEnvironment(scope, config),
				emptySettings
			);
			for (const { environment, data } of sum.byEnvironment) {
				queryClient.setQueryData(queryKeys.environments.settings(environment.id), data);
			}
			return sum;
		}
	});
}

/**
 * Refetches the cached settings of one environment, including multi-environment views that contain it.
 * Settings pages build their form from the cache when they open, so marking it stale is not enough.
 */
export function refetchEnvironmentSettings(queryClient: QueryClient, environmentId: string) {
	return queryClient.invalidateQueries({
		queryKey: queryKeys.environments.settingsAll,
		refetchType: 'all',
		predicate: ({ queryKey }) => {
			const [, scope, view] = queryKey;
			if (view === 'across') return String(scope).split(',').includes(environmentId);
			return scope === environmentId;
		}
	});
}
