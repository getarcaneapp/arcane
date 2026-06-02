import { swarmService } from '$lib/services/swarm-service';
import { queryKeys } from '$lib/query/query-keys';
import type { SearchPaginationSortRequest } from '$lib/types/shared';
import { resolveInitialTableRequest } from '$lib/utils/tables';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const { queryClient } = await parent();
	const requestOptions = resolveInitialTableRequest('arcane-swarm-stacks-table', {
		pagination: {
			page: 1,
			limit: 20
		},
		sort: {
			column: 'name',
			direction: 'asc'
		}
	} satisfies SearchPaginationSortRequest);

	const stacks = await queryClient.fetchQuery({
		queryKey: queryKeys.swarm.stacks.list(requestOptions),
		queryFn: () => swarmService.getStacks(requestOptions)
	});

	return {
		stacks,
		requestOptions
	};
};
