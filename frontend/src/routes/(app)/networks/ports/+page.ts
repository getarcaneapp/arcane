import { queryKeys } from '#lib/query/query-keys.js';
import { portService } from '#lib/services/port-service.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import { fetchAcrossEnvironments, readableEnvironments } from '#lib/utils/environments.js';
import { resolveListPageLoadContext } from '#lib/utils/tables.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent }) => {
	const {
		queryClient,
		scope,
		requestOptions: portRequestOptions
	} = await resolveListPageLoadContext(parent, 'arcane-ports-table', {
		column: 'hostPort',
		direction: 'asc'
	});
	const environments = readableEnvironments('containers:list');

	let ports;
	const operationResult = await tryCatch(
		(async () =>
			queryClient.query({
				queryKey: queryKeys.ports.list(scope, portRequestOptions),
				queryFn: () =>
					fetchAcrossEnvironments(environments, portRequestOptions, (id, request, config) =>
						portService.getPortsForEnvironment(id, request, config)
					)
			}))()
	);
	if (operationResult.error !== null) {
		const err = operationResult.error;

		throwPageLoadError(err, 'Failed to load ports');
	} else {
		ports = operationResult.data;
	}

	return {
		scope,
		ports,
		portRequestOptions
	};
};
