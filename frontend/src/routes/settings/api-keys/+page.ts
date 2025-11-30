import { apiKeyService } from '$lib/services/api-key-service';
import type { SearchPaginationSortRequest } from '$lib/types/pagination.type';
import { resolveInitialTableRequest } from '$lib/utils/table-persistence.util';

export const load = async () => {
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

	const apiKeys = await apiKeyService.getApiKeys(apiKeyRequestOptions);

	return {
		apiKeys,
		apiKeyRequestOptions
	};
};
