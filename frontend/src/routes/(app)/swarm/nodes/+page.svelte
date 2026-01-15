<script lang="ts">
	import { UsersIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import { swarmService } from '$lib/services/swarm-service';
	import { untrack } from 'svelte';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '$lib/layouts/index.js';
	import { useEnvironmentRefresh } from '$lib/hooks/use-environment-refresh.svelte';
	import { parallelRefresh } from '$lib/utils/refresh.util';
	import SwarmNodesTable from './nodes-table.svelte';

	let { data } = $props();

	let nodes = $state(untrack(() => data.nodes));
	let requestOptions = $state(untrack(() => data.requestOptions));
	let isLoading = $state({ refresh: false });

	async function refresh() {
		await parallelRefresh(
			{
				nodes: {
					fetch: () => swarmService.getNodes(requestOptions),
					onSuccess: (data) => {
						nodes = data;
					},
					errorMessage: m.common_refresh_failed({ resource: m.swarm_nodes_title() })
				}
			},
			(v) => (isLoading.refresh = v)
		);
	}

	useEnvironmentRefresh(refresh);

	const totalNodes = $derived(nodes?.pagination?.totalItems ?? nodes?.data?.length ?? 0);

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
			title: m.swarm_nodes_total(),
			value: totalNodes,
			icon: UsersIcon,
			iconColor: 'text-blue-500'
		}
	]);
</script>

<ResourcePageLayout title={m.swarm_nodes_title()} subtitle={m.swarm_nodes_subtitle()} {actionButtons} {statCards}>
	{#snippet mainContent()}
		<SwarmNodesTable bind:nodes bind:requestOptions />
	{/snippet}
</ResourcePageLayout>
