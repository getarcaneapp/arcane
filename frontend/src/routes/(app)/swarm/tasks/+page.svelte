<script lang="ts">
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { untrack } from 'svelte';

	import { swarmEnvironmentsQueryOptions, type SwarmEnvironments } from '#lib/components/swarm/swarm-environments.js';
	import { JobsIcon } from '#lib/icons/index.js';
	import { ResourcePageLayout, type StatCardConfig } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { swarmService } from '#lib/services/swarm-service.js';
	import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { fetchAcrossEnvironments, withFailedEnvironments } from '#lib/utils/environments.js';
	import { createRefreshActionButtons } from '#lib/utils/resource-actions.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import SwarmTasksTable from './components/tasks-table.svelte';

	let { data } = $props();
	const queryClient = useQueryClient();

	let requestOptions = $state<SearchPaginationSortRequest>(untrack(() => data.requestOptions));
	let nodeId = $derived(data.nodeId ?? '');
	const swarmQuery = createQuery(() => ({ ...swarmEnvironmentsQueryOptions('swarm:read'), enabled: !nodeId }));
	const swarm = $derived(nodeId ? data.swarm : (swarmQuery.data ?? data.swarm));

	function tasksQueryOptions(options: SearchPaginationSortRequest, current: SwarmEnvironments = swarm) {
		const { environments, failedEnvironments, scope } = current;
		const node = nodeId;
		return {
			queryKey: queryKeys.swarm.tasks(scope, node, options),
			queryFn: async () =>
				withFailedEnvironments(
					await fetchAcrossEnvironments(environments, options, (id, request, config) => {
						if (node) return swarmService.getNodeTasks(node, request, id, config);
						return swarmService.getTasks(request, id, config);
					}),
					failedEnvironments
				)
		};
	}

	const tasksQuery = createQuery(() => ({
		...tasksQueryOptions(requestOptions),
		placeholderData: (previous) => previous
	}));
	let tasks = $derived(tasksQuery.data ?? data.tasks);
	const isRefreshing = $derived(tasksQuery.isFetching && !tasksQuery.isPending);

	function fetchTasks(options: SearchPaginationSortRequest, current: SwarmEnvironments = swarm) {
		requestOptions = options;
		return queryClient.query({ ...tasksQueryOptions(options, current), staleTime: 0 });
	}

	async function refresh() {
		const discovered = nodeId ? undefined : (await swarmQuery.refetch()).data;
		await handleApiResultWithCallbacks({
			result: await tryCatch(fetchTasks(requestOptions, discovered ?? swarm)),
			message: m.common_refresh_failed({ resource: m.tasks() })
		});
	}

	const totalTasks = $derived(tasks?.pagination?.totalItems ?? tasks?.data?.length ?? 0);

	const actionButtons = $derived(
		createRefreshActionButtons({
			refreshLabel: m.common_refresh(),
			onRefresh: refresh,
			refreshing: isRefreshing
		})
	);

	const statCards: StatCardConfig[] = $derived([
		{
			title: m.swarm_tasks_total(),
			value: totalTasks,
			icon: JobsIcon,
			iconColor: 'text-info'
		}
	]);
</script>

<ResourcePageLayout
	title={m.tasks()}
	subtitle={nodeId ? m.swarm_tasks_subtitle_node_scoped() : m.swarm_tasks_subtitle()}
	{actionButtons}
	{statCards}
>
	{#snippet mainContent()}
		<SwarmTasksTable bind:tasks bind:requestOptions {fetchTasks} />
	{/snippet}
</ResourcePageLayout>
