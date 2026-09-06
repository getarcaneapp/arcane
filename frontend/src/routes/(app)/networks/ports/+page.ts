import { portService } from '#lib/services/port-service.js';
import { queryKeys } from '#lib/query/query-keys.js';
import { resolveListPageLoadContext } from '#lib/utils/tables.js';
import { throwPageLoadError } from '#lib/utils/api.js';
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
		envId,
		ports,
		portRequestOptions
	};
};
