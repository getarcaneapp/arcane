import { queryOptions } from '@tanstack/svelte-query';

import { queryKeys } from '#lib/query/query-keys.js';
import { environmentManagementService } from '#lib/services/env-mgmt-service.js';
import { settingsService } from '#lib/services/settings-service.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';

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
		queryFn: () =>
			canReadSettings
				? settingsService.getSettingsForEnvironment(environmentId)
				: settingsService.getPublicSettingsForEnvironment(environmentId)
	});
}

export function environmentDeploymentSnippetsQuery(environmentId: string) {
	return queryOptions({
		queryKey: queryKeys.environments.deploymentSnippets(environmentId),
		queryFn: () => environmentManagementService.getDeploymentSnippets(environmentId)
	});
}
