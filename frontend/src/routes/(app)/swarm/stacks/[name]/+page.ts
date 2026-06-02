import { error } from '@sveltejs/kit';
import { swarmService } from '$lib/services/swarm-service';
import { queryKeys } from '$lib/query/query-keys';
import type { SearchPaginationSortRequest } from '$lib/types/shared';
import { resolveInitialTableRequest } from '$lib/utils/tables';
import type { PageLoad } from './$types';

type StackSourceState = 'loading' | 'available' | 'missing' | 'forbidden' | 'error';

export const load: PageLoad = async ({ params, parent }) => {
	const { queryClient } = await parent();
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
			queryClient.fetchQuery({
				queryKey: queryKeys.swarm.stacks.detail(stackName),
				queryFn: () => swarmService.getStack(stackName)
			}),
			queryClient.fetchQuery({
				queryKey: queryKeys.swarm.stacks.services(stackName, servicesRequestOptions),
				queryFn: () => swarmService.getStackServices(stackName, servicesRequestOptions)
			}),
			queryClient.fetchQuery({
				queryKey: queryKeys.swarm.stacks.tasks(stackName, tasksRequestOptions),
				queryFn: () => swarmService.getStackTasks(stackName, tasksRequestOptions)
			})
		]);

		return {
			stack,
			stackName,
			services,
			tasks,
			source: null,
			sourceState: 'loading' as StackSourceState,
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
