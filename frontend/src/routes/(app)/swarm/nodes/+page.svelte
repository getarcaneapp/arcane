<script lang="ts">
	import * as Alert from '$lib/components/ui/alert';
	import { AlertTriangleIcon, UsersIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import { swarmService } from '$lib/services/swarm-service';
	import { untrack } from 'svelte';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '$lib/layouts/index.js';
	import { useEnvironmentRefresh } from '$lib/hooks/use-environment-refresh.svelte';
	import { parallelRefresh } from '$lib/utils/api';
	import SwarmNodesTable from './nodes-table.svelte';
	import { hasPermission } from '$lib/utils/auth';
	import { environmentStore } from '$lib/stores/environment.store.svelte';

	let { data } = $props();

	let nodes = $state(untrack(() => data.nodes));
	let requestOptions = $state(untrack(() => data.requestOptions));
	let isLoading = $state({ refresh: false });
	let reconciledEnvironmentId = $state<string | null>(null);
	const currentEnvironmentId = $derived(environmentStore.selected?.id ?? null);
	const canManageNodes = $derived(hasPermission('swarm:nodes', currentEnvironmentId ?? undefined));

	async function refresh() {
		await parallelRefresh(
			{
				nodes: {
					fetch: () => swarmService.getNodes(requestOptions),
					onSuccess: (data) => {
						nodes = data;
					},
					errorMessage: m.common_refresh_failed({ resource: m.nodes() })
				}
			},
			(v) => (isLoading.refresh = v)
		);
	}

	useEnvironmentRefresh(refresh);

	$effect(() => {
		const environmentId = currentEnvironmentId;
		if (!environmentId || !canManageNodes || reconciledEnvironmentId === environmentId) return;
		reconciledEnvironmentId = environmentId;
		void swarmService
			.reconcileNodeAgents()
			.then(refresh)
			.catch(() => undefined);
	});

	const totalNodes = $derived(nodes?.pagination?.totalItems ?? nodes?.data?.length ?? 0);
	const uncoveredNodes = $derived(
		(nodes?.data ?? []).filter((node) => node.agent?.state !== 'connected' || node.agent?.connected === false)
	);
	const uncoveredNodeCount = $derived(uncoveredNodes.length);

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

<ResourcePageLayout title={m.nodes()} subtitle={m.swarm_nodes_subtitle()} {actionButtons} {statCards}>
	{#snippet mainContent()}
		<div class="space-y-4">
			{#if uncoveredNodeCount > 0}
				<Alert.Root class="border-amber-500/30 bg-amber-500/10 text-amber-900 dark:text-amber-100">
					<AlertTriangleIcon class="size-4 text-amber-600 dark:text-amber-300" />
					<Alert.Title>{m.swarm_node_agent_warning_title()}</Alert.Title>
					<Alert.Description>{m.swarm_node_agent_warning_description({ count: uncoveredNodeCount })}</Alert.Description>
				</Alert.Root>
			{/if}

			<SwarmNodesTable bind:nodes bind:requestOptions />
		</div>
	{/snippet}
</ResourcePageLayout>
