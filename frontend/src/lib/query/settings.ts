import { queryOptions } from '@tanstack/svelte-query';

import { queryKeys } from '#lib/query/query-keys.js';
import { settingsSearchService } from '#lib/services/settings-search.js';
import { settingsService } from '#lib/services/settings-service.js';

export function publicSettingsQuery(environmentId: string) {
	return queryOptions({
		queryKey: queryKeys.settings.public(environmentId),
		queryFn: ({ signal }) => settingsService.getPublicSettingsForEnvironment(environmentId, { signal }),
		retry: false,
		retryOnMount: false
	});
}

export function mergedSettingsQuery(environmentId: string) {
	return queryOptions({
		queryKey: queryKeys.settings.byEnvironment(environmentId),
		queryFn: () => settingsService.getSettingsForEnvironmentMerged(environmentId)
	});
}

export function settingsCategoriesQuery() {
	return queryOptions({
		queryKey: queryKeys.settings.categories(),
		queryFn: () => settingsSearchService.getCategories()
	});
}
