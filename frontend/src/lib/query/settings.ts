import { queryOptions } from '@tanstack/svelte-query';

import { queryKeys } from '#lib/query/query-keys.js';
import { settingsService } from '#lib/services/settings-service.js';

export function publicSettingsQuery(environmentId: string) {
	return queryOptions({
		queryKey: queryKeys.settings.public(environmentId),
		queryFn: ({ signal }) => settingsService.getPublicSettingsForEnvironment(environmentId, signal),
		retry: false,
		retryOnMount: false
	});
}
