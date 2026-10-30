import { networkListQuery } from '#lib/query/networks.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import { readableEnvironments } from '#lib/utils/environments.js';
import { resolveInitialListPageRequest } from '#lib/utils/tables.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const [{ queryClient }] = await Promise.all([parent(), environmentStore.ready]);
	const scope = environmentStore.shownKey;
	const networkRequestOptions = resolveInitialListPageRequest('arcane-networks-table', {
		column: 'name',
		direction: 'asc'
	});

	const networks = await queryClient
		.query(networkListQuery(scope, readableEnvironments('networks:list'), networkRequestOptions))
		.catch((err: unknown) => throwPageLoadError(err, 'Failed to load networks'));

	return {
		scope,
		networks,
		networkRequestOptions
	};
};
