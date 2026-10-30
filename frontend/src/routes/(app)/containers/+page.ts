import { containerListQuery } from '#lib/query/containers.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import { readableEnvironments } from '#lib/utils/environments.js';
import { resolveInitialTableRequest } from '#lib/utils/tables.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const [{ queryClient }] = await Promise.all([parent(), environmentStore.ready]);
	const scope = environmentStore.shownKey;

	const containerRequestOptions = resolveInitialTableRequest('arcane-container-table', {
		pagination: { page: 1, limit: 20 },
		sort: { column: 'created', direction: 'desc' }
	} satisfies SearchPaginationSortRequest);

	const containers = await queryClient
		.query(
			containerListQuery(scope, readableEnvironments('containers:list'), { ...containerRequestOptions, groupByProject: false })
		)
		.catch((err: unknown) => throwPageLoadError(err, 'Failed to load containers'));

	return {
		containers,
		containerRequestOptions
	};
};
