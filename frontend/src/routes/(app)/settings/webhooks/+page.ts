import { webhookListQuery } from '#lib/query/webhooks.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
import { readableEnvironments } from '#lib/utils/environments.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const [{ queryClient }] = await Promise.all([parent(), environmentStore.ready]);
	const scope = environmentStore.shownKey;
	const webhookRequestOptions: SearchPaginationSortRequest = { pagination: { page: 1, limit: 20 } };

	const webhooks = await queryClient.query(webhookListQuery(scope, readableEnvironments('webhooks:list'), webhookRequestOptions));

	return {
		scope,
		webhooks,
		webhookRequestOptions
	};
};
