<script lang="ts">
	import { JobsIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import { swarmService } from '$lib/services/swarm-service';
	import { untrack } from 'svelte';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '$lib/layouts/index.js';
	import { useEnvironmentRefresh } from '$lib/hooks/use-environment-refresh.svelte';
	import { parallelRefresh } from '$lib/utils/refresh.util';
	import SwarmTasksTable from './tasks-table.svelte';

	let { data } = $props();

	let tasks = $state(untrack(() => data.tasks));
	let requestOptions = $state(untrack(() => data.requestOptions));
	let isLoading = $state({ refresh: false });

	async function refresh() {
		await parallelRefresh(
			{
				tasks: {
					fetch: () => swarmService.getTasks(requestOptions),
					onSuccess: (data) => {
						tasks = data;
					},
					errorMessage: m.common_refresh_failed({ resource: m.swarm_tasks_title() })
				}
			},
			(v) => (isLoading.refresh = v)
		);
	}

	useEnvironmentRefresh(refresh);

	const totalTasks = $derived(tasks?.pagination?.totalItems ?? tasks?.data?.length ?? 0);

	const actionButtons: ActionButton[] = $derived([
		{
			id: 'refresh',
			action: 'restart',
			label: m.common_refresh(),
			onclick: refresh,
			loading: isLoading.refresh,
			disabled: isLoading.refresh
		}
	]);

	const statCards: StatCardConfig[] = $derived([
		{
			title: m.swarm_tasks_total(),
			value: totalTasks,
			icon: JobsIcon,
			iconColor: 'text-blue-500'
		}
	]);
</script>

<ResourcePageLayout title={m.swarm_tasks_title()} subtitle={m.swarm_tasks_subtitle()} {actionButtons} {statCards}>
	{#snippet mainContent()}
		<SwarmTasksTable bind:tasks bind:requestOptions />
	{/snippet}
</ResourcePageLayout>
