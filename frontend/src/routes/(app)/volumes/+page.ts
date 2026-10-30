import { volumeListQuery } from '#lib/query/volumes.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
import { readableEnvironments } from '#lib/utils/environments.js';
import { resolveInitialTableRequest } from '#lib/utils/tables.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const [{ queryClient }] = await Promise.all([parent(), environmentStore.ready]);
	const scope = environmentStore.shownKey;

	const volumeRequestOptions = resolveInitialTableRequest(
		'arcane-volumes-table',
		{
			pagination: {
				page: 1,
				limit: 20
			},
			sort: {
				column: 'name',
				direction: 'asc'
			}
		} satisfies SearchPaginationSortRequest,
		{ deferredSortColumns: ['size'] }
	);

	const volumes = await queryClient.query(volumeListQuery(scope, readableEnvironments('volumes:list'), volumeRequestOptions));

	return {
		scope,
		volumes,
		volumeRequestOptions
	};
};
