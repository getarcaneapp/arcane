import { oidcMappingService } from '$lib/services/oidc-mapping-service';
import { federatedCredentialService } from '$lib/services/federated-credential-service';
import { roleService } from '$lib/services/role-service';
import { environmentManagementService } from '$lib/services/env-mgmt-service';
import { queryKeys } from '$lib/query/query-keys';
import type { SearchPaginationSortRequest } from '$lib/types/shared';
import { resolveInitialTableRequest } from '$lib/utils/tables';
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

	// OIDC mappings live alongside the OIDC config in this page. We load them
	// here (instead of a standalone /settings/oidc-mappings route) so admins
	// configure groups claim + the mappings that read from it in one place.
	const [mappings, federatedCredentials, roles, environmentsPage] = await Promise.all([
		oidcMappingService.list(),
		queryClient.fetchQuery({
			queryKey: queryKeys.federatedCredentials.list(federatedCredentialRequestOptions),
			queryFn: () => federatedCredentialService.list(federatedCredentialRequestOptions)
		}),
		queryClient.fetchQuery({
			queryKey: ['roles', 'all'],
			queryFn: () => roleService.getAll()
		}),
		queryClient.fetchQuery({
			queryKey: queryKeys.environments.list({
				pagination: { page: 1, limit: 1000 },
				sort: { column: 'name', direction: 'asc' }
			}),
			queryFn: () =>
				environmentManagementService.getEnvironments({
					pagination: { page: 1, limit: 1000 },
					sort: { column: 'name', direction: 'asc' }
				})
		})
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
