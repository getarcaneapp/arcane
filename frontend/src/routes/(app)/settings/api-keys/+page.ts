import { apiKeyService } from '$lib/services/api-key-service';
import { roleService } from '$lib/services/role-service';
import { queryKeys } from '$lib/query/query-keys';
import type { SearchPaginationSortRequest } from '$lib/types/shared';
import { resolveInitialTableRequest } from '$lib/utils/tables';
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

	const [apiKeys, permissionsManifest] = await Promise.all([
		queryClient.fetchQuery({
			queryKey: queryKeys.apiKeys.list(apiKeyRequestOptions),
			queryFn: () => apiKeyService.getApiKeys(apiKeyRequestOptions)
		}),
		queryClient.fetchQuery({
			queryKey: ['permissions', 'manifest'],
			queryFn: () => roleService.getPermissionsManifest(),
			staleTime: Infinity
		})
	]);

	return {
		apiKeys,
		apiKeyRequestOptions,
		permissionsManifest
	};
};
