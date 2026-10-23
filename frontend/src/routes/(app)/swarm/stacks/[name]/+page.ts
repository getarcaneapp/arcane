import { error } from '@sveltejs/kit';

import { swarmService } from '#lib/services/swarm-service.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
import { environmentName, fetchAcrossEnvironments, resolveDetailEnvironmentId } from '#lib/utils/environments.js';
import { resolveInitialTableRequest } from '#lib/utils/tables.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

type StackSourceState = 'loading' | 'available' | 'missing' | 'forbidden' | 'error';

export const load: PageLoad = async ({ params, url }) => {
	const stackName = decodeURIComponent(params.name);
	const envId = await resolveDetailEnvironmentId(url);
	const environments = [{ id: envId, name: environmentName(envId) }];
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
				swarmService.getStack(stackName, envId),
				fetchAcrossEnvironments(environments, servicesRequestOptions, (id, request, config) =>
					swarmService.getStackServices(stackName, request, id, config)
				),
				fetchAcrossEnvironments(environments, tasksRequestOptions, (id, request, config) =>
					swarmService.getStackTasks(stackName, request, id, config)
				)
			]);

			return {
				envId,
				environments,
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
