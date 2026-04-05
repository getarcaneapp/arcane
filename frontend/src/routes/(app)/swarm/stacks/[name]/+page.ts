import { error, redirect } from '@sveltejs/kit';
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
		const stack = await swarmService.getStack(stackName);
		const [services, tasks, stackProjectResult] = await Promise.all([
			swarmService.getStackServices(stackName, servicesRequestOptions),
			swarmService.getStackTasks(stackName, tasksRequestOptions),
			swarmService
				.getStackProject(stackName)
				.then((stackProject) => ({ state: 'available' as const, stackProject }))
				.catch((err: any) => {
					if (err?.status === 404) {
						return { state: 'missing' as const, stackProject: null };
					}

					console.warn('Failed to load saved swarm stack project:', err);
					return { state: 'error' as const, stackProject: null };
				})
		]);

		return {
			stack,
			stackName,
			services,
			tasks,
			stackProject: stackProjectResult.stackProject,
			stackProjectState: stackProjectResult.state,
			servicesRequestOptions,
			tasksRequestOptions
		};
	} catch (err: any) {
		console.error('Failed to load stack details:', err);
		if (err.status === 404) {
			let hasSavedProject = false;

			try {
				await swarmService.getStackProject(stackName);
				hasSavedProject = true;
			} catch (projectErr: any) {
				if (projectErr?.status === 404) {
					throw error(404, 'Swarm stack not found');
				}
				throw error(projectErr?.status || 500, projectErr?.message || 'Failed to load saved swarm stack project');
			}

			if (hasSavedProject) {
				throw redirect(307, `/projects/swarm/${encodeURIComponent(stackName)}`);
			}
		}
		throw error(500, err.message || 'Failed to load stack details');
	}
};
