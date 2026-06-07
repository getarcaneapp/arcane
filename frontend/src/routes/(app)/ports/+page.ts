import { portService } from '$lib/services/port-service';
import { queryKeys } from '$lib/query/query-keys';
import type { SearchPaginationSortRequest } from '$lib/types/pagination.type';
import { resolveInitialTableRequest } from '$lib/utils/table-persistence.util';
import { throwPageLoadError } from '$lib/utils/page-load-error.util';
import type { PageLoad } from './$types';
import { environmentStore } from '$lib/stores/environment.store.svelte';

export const load: PageLoad = async ({ parent }) => {
	const { queryClient } = await parent();
	const envId = await environmentStore.getCurrentEnvironmentId();

	const portRequestOptions = resolveInitialTableRequest('arcane-ports-table', {
		pagination: {
			page: 1,
			limit: 20
		},
		sort: {
			column: 'hostPort',
			direction: 'asc'
		}
	} satisfies SearchPaginationSortRequest);

	let ports;
	try {
		ports = await queryClient.fetchQuery({
			queryKey: queryKeys.ports.list(envId, portRequestOptions),
			queryFn: () => portService.getPortsForEnvironment(envId, portRequestOptions)
		});
	} catch (err) {
		throwPageLoadError(err, 'Failed to load ports');
	}

	return {
		ports,
		portRequestOptions
	};
};
