import { queryKeys } from '#lib/query/query-keys.js';
import { gitOpsSyncService } from '#lib/services/gitops-sync-service.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import { fetchAcrossEnvironments, readableEnvironments } from '#lib/utils/environments.js';
import { resolveInitialTableRequest } from '#lib/utils/tables.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const { queryClient } = await parent();
	await environmentStore.ready;
	const environments = readableEnvironments('gitops:list');
	const scope = environmentStore.shownKey;

	const syncRequestOptions = resolveInitialTableRequest('arcane-gitops-syncs-table', {
		pagination: {
			page: 1,
			limit: 20
		},
		sort: {
			column: 'name',
			direction: 'asc'
		}
	} satisfies SearchPaginationSortRequest);

	const result = await tryCatch(
		queryClient.query({
			queryKey: queryKeys.gitOpsSyncs.list(scope, syncRequestOptions),
			queryFn: () =>
				fetchAcrossEnvironments(environments, syncRequestOptions, (id, request, config) =>
					gitOpsSyncService.getSyncs(id, request, config)
				)
		})
	);
	if (result.error !== null) throwPageLoadError(result.error, 'Failed to load GitOps syncs');

	return { scope, syncs: result.data, syncRequestOptions };
};
