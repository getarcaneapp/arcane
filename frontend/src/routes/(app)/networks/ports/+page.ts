import { tryCatch } from '#lib/utils/try-catch.js';
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
	const operationResult = await tryCatch(
		(async () =>
			queryClient.query({
				queryKey: queryKeys.ports.list(envId, portRequestOptions),
				queryFn: () => portService.getPortsForEnvironment(envId, portRequestOptions)
			}))()
	);
	if (operationResult.error !== null) {
		const err = operationResult.error;

		throwPageLoadError(err, 'Failed to load ports');
	} else {
		ports = operationResult.data;
	}

	return {
		envId,
		ports,
		portRequestOptions
	};
};
