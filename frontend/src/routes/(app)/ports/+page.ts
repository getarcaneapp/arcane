import { portService } from '$lib/services/port-service';
import { queryKeys } from '$lib/query/query-keys';
import { resolveListPageLoadContext } from '$lib/utils/tables';
import { throwPageLoadError } from '$lib/utils/api';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const {
		queryClient,
		envId,
		requestOptions: portRequestOptions
	} = await resolveListPageLoadContext(parent, 'arcane-ports-table', {
		column: 'hostPort',
		direction: 'asc'
	});

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
