import { apiKeyListQuery } from '#lib/query/api-keys.js';
import { permissionsManifestQuery } from '#lib/query/roles.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
import { resolveInitialTableRequest } from '#lib/utils/tables.js';

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
		queryClient.query(apiKeyListQuery(apiKeyRequestOptions)),
		queryClient.query({ ...permissionsManifestQuery(), staleTime: Infinity })
	]);

	return {
		apiKeys,
		apiKeyRequestOptions,
		permissionsManifest
	};
};
