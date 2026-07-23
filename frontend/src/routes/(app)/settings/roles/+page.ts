import { roleService } from '#lib/services/role-service';
import type { SearchPaginationSortRequest } from '#lib/types/shared';
import { resolveInitialTableRequest } from '#lib/utils/tables';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const { queryClient } = await parent();

	const rolesRequestOptions = await resolveInitialTableRequest('arcane-roles-table', {
		pagination: {
			page: 1,
			limit: 20
		},
		sort: {
			column: 'name',
			direction: 'asc'
		}
	} satisfies SearchPaginationSortRequest);

	const roles = await queryClient.fetchQuery({
		queryKey: ['roles', 'list', rolesRequestOptions],
		queryFn: () => roleService.getRoles(rolesRequestOptions)
	});

	return {
		roles,
		rolesRequestOptions
	};
};
