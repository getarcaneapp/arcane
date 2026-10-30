<script lang="ts">
	import { createQuery } from '@tanstack/svelte-query';
	import { untrack } from 'svelte';

	import { swarmEnvironmentsQueryOptions } from '#lib/components/swarm/swarm-environments.js';
	import { JobsIcon } from '#lib/icons/index.js';
	import { ResourcePageLayout, type StatCardConfig } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { swarmTaskListQuery } from '#lib/query/swarm.js';
	import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { createRefreshActionButtons } from '#lib/utils/resource-actions.js';
	import { ResourceListPageState } from '#lib/utils/resource-list-page.svelte.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import SwarmTasksTable from './components/tasks-table.svelte';

	let { data } = $props();

	const pageState = new ResourceListPageState<SearchPaginationSortRequest>(untrack(() => data.requestOptions));
	const nodeId = $derived(data.nodeId ?? '');
	const swarmQuery = createQuery(() => ({ ...swarmEnvironmentsQueryOptions('swarm:read'), enabled: !nodeId }));
	const swarm = $derived(nodeId ? data.swarm : (swarmQuery.data ?? data.swarm));

	const tasksQuery = createQuery(() => {
		const scope = swarm.scope;
		return {
			...swarmTaskListQuery(swarm, nodeId, pageState.requestOptions),
			placeholderData: (previous, query) => (query?.queryKey[1] === scope ? previous : undefined),
			select: (value) => ({ envId: scope, value })
		};
	});
	const tasks = $derived(tasksQuery.data?.value ?? data.tasks);
	const isRefreshing = $derived(tasksQuery.isFetching && !tasksQuery.isPending);

	async function refresh() {
		if (!nodeId) await swarmQuery.refetch();
		await handleApiResultWithCallbacks({
			result: await tryCatch(tasksQuery.refetch({ throwOnError: true })),
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
		<SwarmTasksTable {tasks} bind:requestOptions={pageState.requestOptions} onRetry={() => tasksQuery.refetch()} />
	{/snippet}
</ResourcePageLayout>
