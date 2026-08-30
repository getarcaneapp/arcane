<script lang="ts">
	import { JobsIcon } from '#lib/icons';
	import { m } from '#lib/paraglide/messages';
	import { swarmService } from '#lib/services/swarm-service';
	import { untrack } from 'svelte';
	import { ResourcePageLayout, type StatCardConfig } from '#lib/layouts/index.js';
	import { useEnvironmentRefresh } from '#lib/hooks/use-environment-refresh.svelte';
	import { simpleRefresh } from '#lib/utils/api';
	import SwarmTasksTable from './tasks-table.svelte';
	import { createRefreshActionButtons } from '#lib/utils/resource-actions';

	let { data } = $props();

	let tasks = $state(untrack(() => data.tasks));
	let requestOptions = $state(untrack(() => data.requestOptions));
	let nodeId = $state(untrack(() => data.nodeId ?? ''));
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
