import { portListQuery } from '#lib/query/networks.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import { readableEnvironments } from '#lib/utils/environments.js';
import { resolveInitialListPageRequest } from '#lib/utils/tables.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const [{ queryClient }] = await Promise.all([parent(), environmentStore.ready]);
	const scope = environmentStore.shownKey;
	const portRequestOptions = resolveInitialListPageRequest('arcane-ports-table', {
		column: 'hostPort',
		direction: 'asc'
	});

	const ports = await queryClient
		.query(portListQuery(scope, readableEnvironments('containers:list'), portRequestOptions))
		.catch((err: unknown) => throwPageLoadError(err, 'Failed to load ports'));

	return {
		scope,
		ports,
		portRequestOptions
	};
};
