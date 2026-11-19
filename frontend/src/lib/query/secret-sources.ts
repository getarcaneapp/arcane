import { queryOptions } from '@tanstack/svelte-query';

import { queryKeys } from '#lib/query/query-keys.js';
import { secretSourceService } from '#lib/services/secret-source-service.js';

export function secretSourceListQuery() {
	return queryOptions({
		queryKey: queryKeys.secretSources.list(),
		queryFn: () => secretSourceService.list()
	});
}

export function secretSourcePresetsQuery() {
	return queryOptions({
		queryKey: queryKeys.secretSources.presets(),
		queryFn: () => secretSourceService.presets()
	});
}
