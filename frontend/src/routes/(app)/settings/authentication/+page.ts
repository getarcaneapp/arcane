import { PERMISSIONS } from '#lib/permissions.generated.js';
import { queryKeys } from '#lib/query/query-keys.js';
import { environmentManagementService } from '#lib/services/env-mgmt-service.js';
import { federatedCredentialService } from '#lib/services/federated-credential-service.js';
import { groupService } from '#lib/services/group-service.js';
import { oidcMappingService } from '#lib/services/oidc-mapping-service.js';
import { roleService } from '#lib/services/role-service.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
import { userHasPermission } from '#lib/utils/auth.js';
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

	// OIDC mappings live alongside the OIDC config in this page. We load them
	// here (instead of a standalone /settings/oidc-mappings route) so admins
	// configure groups claim + the mappings that read from it in one place.
	const [mappings, groups, federatedCredentials, roles, environmentsPage] = await Promise.all([
		userHasPermission(parentData.user, PERMISSIONS.OIDCMAPPINGS_LIST) ? oidcMappingService.list() : [],
		userHasPermission(parentData.user, PERMISSIONS.GROUPS_LIST)
			? queryClient.query({
					queryKey: queryKeys.groups.options(),
					queryFn: () => groupService.getAll()
				})
			: [],
		queryClient.query({
			queryKey: queryKeys.federatedCredentials.list(federatedCredentialRequestOptions),
			queryFn: () => federatedCredentialService.list(federatedCredentialRequestOptions)
		}),
		queryClient.query({
			queryKey: queryKeys.roles.all(),
			queryFn: () => roleService.getAll()
		}),
		queryClient.query({
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
		groups,
		federatedCredentials,
		federatedCredentialRequestOptions,
		roles,
		environments: environmentsPage.data
	};
};
