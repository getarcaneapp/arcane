import { environmentListQuery } from '#lib/query/environments.js';
import { federatedCredentialListQuery } from '#lib/query/federated-credentials.js';
import { oidcMappingListQuery } from '#lib/query/oidc-mappings.js';
import { roleAllQuery } from '#lib/query/roles.js';
import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
import { resolveInitialTableRequest } from '#lib/utils/tables.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const parentData = await parent();
	const { queryClient } = parentData;
	const federatedCredentialRequestOptions = resolveInitialTableRequest('arcane-federated-credentials-table', {
		pagination: {
			page: 1,
			limit: 20
		},
		sort: {
			column: 'createdAt',
			direction: 'desc'
		}
	} satisfies SearchPaginationSortRequest);

	const [mappings, federatedCredentials, roles, environmentsPage] = await Promise.all([
		queryClient.query(oidcMappingListQuery()),
		queryClient.query(federatedCredentialListQuery(federatedCredentialRequestOptions)),
		queryClient.query(roleAllQuery()),
		queryClient.query(
			environmentListQuery({
				pagination: { page: 1, limit: 1000 },
				sort: { column: 'name', direction: 'asc' }
			})
		)
	]);

	return {
		...parentData,
		oidcMappings: mappings,
		federatedCredentials,
		federatedCredentialRequestOptions,
		roles,
		environments: environmentsPage.data
	};
};
