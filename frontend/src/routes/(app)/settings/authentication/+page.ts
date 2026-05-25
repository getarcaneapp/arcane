import { oidcMappingService } from '$lib/services/oidc-mapping-service';
import { roleService } from '$lib/services/role-service';
import { environmentManagementService } from '$lib/services/env-mgmt-service';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const parentData = await parent();

	// OIDC mappings live alongside the OIDC config in this page. We load them
	// here (instead of a standalone /settings/oidc-mappings route) so admins
	// configure groups claim + the mappings that read from it in one place.
	const [mappings, roles, environmentsPage] = await Promise.all([
		oidcMappingService.list(),
		roleService.getAll(),
		environmentManagementService.getEnvironments({
			pagination: { page: 1, limit: 1000 },
			sort: { column: 'name', direction: 'asc' }
		})
	]);

	return {
		...parentData,
		oidcMappings: mappings,
		roles,
		environments: environmentsPage.data
	};
};
