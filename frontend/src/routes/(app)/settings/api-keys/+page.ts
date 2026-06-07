import { apiKeyService } from '$lib/services/api-key-service';
import { queryKeys } from '$lib/query/query-keys';
import type { SearchPaginationSortRequest } from '$lib/types/pagination.type';
import { resolveInitialTableRequest } from '$lib/utils/table-persistence.util';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const { queryClient } = await parent();

	const apiKeyRequestOptions = resolveInitialTableRequest('arcane-api-keys-table', {
		pagination: {
			page: 1,
			limit: 20
		},
		sort: {
			column: 'createdAt',
			direction: 'desc'
		}
	} satisfies SearchPaginationSortRequest);

	const apiKeys = await queryClient.fetchQuery({
		queryKey: queryKeys.apiKeys.list(apiKeyRequestOptions),
		queryFn: () => apiKeyService.getApiKeys(apiKeyRequestOptions)
	});

	return {
		apiKeys,
		apiKeyRequestOptions
	};
};
