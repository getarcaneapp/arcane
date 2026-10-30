import { queryOptions } from '@tanstack/svelte-query';

import { queryKeys } from '#lib/query/query-keys.js';
import { webhookService } from '#lib/services/webhook-service.js';
import type { EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
import type { Webhook } from '#lib/types/environment.js';
import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared.js';
import { fetchAcrossEnvironments } from '#lib/utils/environments.js';

function webhookPage(items: Webhook[]): Paginated<Webhook> {
	return {
		data: items,
		pagination: {
			totalPages: 1,
			totalItems: items.length,
			currentPage: 1,
			itemsPerPage: Math.max(items.length, 1)
		}
	};
}

export function webhookListQuery(scope: string, environments: EnvironmentRef[], requestOptions: SearchPaginationSortRequest) {
	return queryOptions({
		queryKey: queryKeys.webhooks.list(scope, requestOptions),
		queryFn: () =>
			fetchAcrossEnvironments(environments, requestOptions, (id, _request, config) =>
				webhookService.getWebhooks(id, config).then(webhookPage)
			)
	});
}
