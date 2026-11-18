import { environmentListQuery } from '#lib/query/environments.js';
import { secretSourceListQuery } from '#lib/query/secret-sources.js';
import { variableListQuery } from '#lib/query/variables.js';
import { GLOBAL_SCOPE } from '#lib/types/role.js';
import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
import { canReachAccessSurface } from '#lib/utils/access-policy.js';
import { hasAnyPermission } from '#lib/utils/auth.js';

import type { PageLoad } from './$types';

const environmentListOptions: SearchPaginationSortRequest = {
	pagination: { page: 1, limit: 1000 },
	sort: { column: 'name', direction: 'asc' }
};

export const load: PageLoad = async ({ parent }) => {
	const { queryClient, permissionsManifest, user } = await parent();

	const showVariables = hasAnyPermission(['variables:list', 'variables:read'], GLOBAL_SCOPE);
	const showSecretSources = canReachAccessSurface(permissionsManifest, 'route.projects.variables.sources', user);

	const [variables, sources, environmentsPage] = await Promise.all([
		showVariables ? queryClient.query(variableListQuery()) : [],
		showSecretSources ? queryClient.query(secretSourceListQuery()) : [],
		queryClient.query(environmentListQuery(environmentListOptions))
	]);

	return { variables, sources, environments: environmentsPage.data, showVariables, showSecretSources };
};
