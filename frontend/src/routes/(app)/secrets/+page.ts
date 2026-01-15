import { secretService } from '$lib/services/secret-service';
import type { SearchPaginationSortRequest } from '$lib/types/pagination.type';
import { resolveInitialTableRequest } from '$lib/utils/table-persistence.util';
import type { PageLoad } from './$types';

export const load: PageLoad = async () => {
	const secretRequestOptions = resolveInitialTableRequest('arcane-secrets-table', {
		pagination: {
			page: 1,
			limit: 20
		},
		sort: {
			column: 'name',
			direction: 'asc'
		}
	} satisfies SearchPaginationSortRequest);

	const secrets = await secretService.getSecrets(secretRequestOptions);

	return {
		secrets,
		secretRequestOptions
	};
};
