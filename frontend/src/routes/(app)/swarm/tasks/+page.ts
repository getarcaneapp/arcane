import type { QueryClient } from '@tanstack/svelte-query';

import { swarmEnvironmentsQueryOptions, type SwarmEnvironments } from '#lib/components/swarm/swarm-environments.js';
import { m } from '#lib/paraglide/messages.js';
import { swarmTaskListQuery } from '#lib/query/swarm.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
import { throwPageLoadError } from '#lib/utils/api.js';
import { environmentName, resolveDetailEnvironmentId } from '#lib/utils/environments.js';
import { resolveInitialTableRequest } from '#lib/utils/tables.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import type { PageLoad } from './$types';

async function taskEnvironments(queryClient: QueryClient, nodeId: string, url: URL): Promise<SwarmEnvironments> {
	if (!nodeId) {
		return queryClient.query(swarmEnvironmentsQueryOptions('swarm:read'));
	}
	const envId = await resolveDetailEnvironmentId(url);
	return {
		environments: [{ id: envId, name: environmentName(envId) }],
		managers: [{ id: envId, name: environmentName(envId) }],
		failedEnvironments: [],
		scope: envId
	};
}

export const load: PageLoad = async ({ parent, url }) => {
	const [{ queryClient }] = await Promise.all([parent(), environmentStore.ready]);
	const searchParam = url.searchParams.get('search') || '';
	const nodeId = url.searchParams.get('nodeId') || '';
	const requestOptions = resolveInitialTableRequest('arcane-swarm-tasks-table', {
		pagination: {
			page: 1,
			limit: 20
		},
		sort: {
			column: 'service',
			direction: 'asc'
		}
	} satisfies SearchPaginationSortRequest);

	if (searchParam) {
		requestOptions.search = searchParam;
	}

	const result = await tryCatch(
		(async () => {
			const swarm = await taskEnvironments(queryClient, nodeId, url);
			const tasks = await queryClient.query(swarmTaskListQuery(swarm, nodeId, requestOptions));
			return { swarm, tasks };
		})()
	);
	if (result.error !== null) {
		throwPageLoadError(result.error, m.common_load_failed({ resource: m.tasks() }));
	}

	return {
		swarm: result.data.swarm,
		tasks: result.data.tasks,
		requestOptions,
		nodeId
	};
};
