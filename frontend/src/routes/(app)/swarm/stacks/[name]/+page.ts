import { error } from '@sveltejs/kit';
import { swarmService } from '$lib/services/swarm-service';
import type { SearchPaginationSortRequest } from '$lib/types/pagination.type';
import { resolveInitialTableRequest } from '$lib/utils/table-persistence.util';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ params }) => {
	const stackName = decodeURIComponent(params.name);
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

	try {
		const [stack, services, tasks] = await Promise.all([
			swarmService.getStack(stackName),
			swarmService.getStackServices(stackName, servicesRequestOptions),
			swarmService.getStackTasks(stackName, tasksRequestOptions)
		]);

		return {
			stack,
			stackName,
			services,
			tasks,
			servicesRequestOptions,
			tasksRequestOptions
		};
	} catch (err: any) {
		console.error('Failed to load stack details:', err);
		if (err.status === 404) {
			throw err;
		}
		throw error(500, err.message || 'Failed to load stack details');
	}
};
