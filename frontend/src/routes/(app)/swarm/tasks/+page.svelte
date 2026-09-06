<script lang="ts">
	import { JobsIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { swarmService } from '#lib/services/swarm-service.js';
	import { ResourcePageLayout, type StatCardConfig } from '#lib/layouts/index.js';
	import { useEnvironmentRefresh } from '#lib/hooks/use-environment-refresh.svelte.js';
	import { simpleRefresh } from '#lib/utils/api.js';
	import SwarmTasksTable from './tasks-table.svelte';
	import { createRefreshActionButtons } from '#lib/utils/resource-actions.js';

	let { data } = $props();

	let tasks = $derived(data.tasks);
	let requestOptions = $derived(data.requestOptions);
	let nodeId = $derived(data.nodeId ?? '');
	let isLoading = $state({ refresh: false });

	async function fetchTasks(options: typeof requestOptions) {
		if (nodeId) {
			return swarmService.getNodeTasks(nodeId, options);
		}
		return swarmService.getTasks(options);
	}

	async function refresh() {
		await simpleRefresh(
			() => fetchTasks(requestOptions),
			(data) => (tasks = data),
			m.common_refresh_failed({ resource: m.tasks() }),
			(loading) => (isLoading.refresh = loading)
		);
	}

	useEnvironmentRefresh(refresh);

	const totalTasks = $derived(tasks?.pagination?.totalItems ?? tasks?.data?.length ?? 0);

	const actionButtons = $derived(
		createRefreshActionButtons({
			refreshLabel: m.common_refresh(),
			onRefresh: refresh,
			refreshing: isLoading.refresh
		})
	);

	const statCards: StatCardConfig[] = $derived([
		{
			title: m.swarm_tasks_total(),
			value: totalTasks,
			icon: JobsIcon,
			iconColor: 'text-blue-500'
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
