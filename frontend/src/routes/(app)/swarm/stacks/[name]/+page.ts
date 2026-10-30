import { error } from '@sveltejs/kit';

import { swarmStackQuery, swarmStackServiceListQuery, swarmStackTaskListQuery } from '#lib/query/swarm.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
import { environmentName, resolveDetailEnvironmentId } from '#lib/utils/environments.js';
import { resolveInitialTableRequest } from '#lib/utils/tables.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

type StackSourceState = 'loading' | 'available' | 'missing' | 'forbidden' | 'error';

export const load: PageLoad = async ({ params, parent, url }) => {
	const stackName = decodeURIComponent(params.name);
	const [{ queryClient }, envId] = await Promise.all([parent(), resolveDetailEnvironmentId(url)]);
	const environment = { id: envId, name: environmentName(envId) };
	const servicesRequestOptions = resolveInitialTableRequest(`arcane-swarm-stack-services-table-${stackName}`, {
		pagination: {
			page: 1,
			limit: 20
		},
		sort: {
			column: 'name',
			direction: 'asc'
		}
	} satisfies SearchPaginationSortRequest);
	const tasksRequestOptions = resolveInitialTableRequest(`arcane-swarm-stack-tasks-table-${stackName}`, {
		pagination: {
			page: 1,
			limit: 20
		},
		sort: {
			column: 'name',
			direction: 'asc'
		}
	} satisfies SearchPaginationSortRequest);

	const operationResult = await tryCatch(
		(async () => {
			const [stack, services, tasks] = await Promise.all([
				queryClient.query(swarmStackQuery(envId, stackName)),
				queryClient.query(swarmStackServiceListQuery(environment, stackName, servicesRequestOptions)),
				queryClient.query(swarmStackTaskListQuery(environment, stackName, tasksRequestOptions))
			]);

			return {
				envId,
				environment,
				stack,
				stackName,
				services,
				tasks,
				source: null,
				sourceState: 'loading' as StackSourceState,
				servicesRequestOptions,
				tasksRequestOptions
			};
		})()
	);
	if (operationResult.error !== null) {
		const err = operationResult.error;

		console.error('Failed to load stack details:', err);
		if ('status' in err && err.status === 404) {
			throw err;
		}
		throw error(500, err.message || 'Failed to load stack details');
	} else {
		return operationResult.data;
	}
};
