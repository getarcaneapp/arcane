import { error } from '@sveltejs/kit';

import { m } from '#lib/paraglide/messages.js';
import { swarmStackQuery, swarmStackServiceListQuery, swarmStackTaskListQuery } from '#lib/query/swarm.js';
import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
import { extractApiErrorStatus } from '#lib/utils/api.js';
import { environmentName, resolveDetailEnvironmentId } from '#lib/utils/environments.js';
import { resolveInitialTableRequest } from '#lib/utils/tables.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

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

	const stackData = await tryCatch(
		Promise.all([
			queryClient.query(swarmStackQuery(envId, stackName)),
			queryClient.query(swarmStackServiceListQuery(environment, stackName, servicesRequestOptions)),
			queryClient.query(swarmStackTaskListQuery(environment, stackName, tasksRequestOptions))
		])
	);
	if (stackData.error !== null) {
		console.error('Failed to load stack details:', stackData.error);
		if (extractApiErrorStatus(stackData.error) === 404) {
			throw stackData.error;
		}
		throw error(500, stackData.error.message || m.failed_to_load_stack_details());
	}
	const [stack, services, tasks] = stackData.data;

	return {
		envId,
		environment,
		stack,
		stackName,
		services,
		tasks,
		servicesRequestOptions,
		tasksRequestOptions
	};
};
