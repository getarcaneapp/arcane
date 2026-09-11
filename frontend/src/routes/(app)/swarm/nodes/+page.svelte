<script lang="ts">
	import { onMount } from 'svelte';
	import userStore from '#lib/stores/user-store.svelte.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import * as Alert from '#lib/components/ui/alert/index.js';
	import { AlertTriangleIcon, UsersIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { swarmService } from '#lib/services/swarm-service.js';
	import { ResourcePageLayout, type StatCardConfig } from '#lib/layouts/index.js';
	import { useEnvironmentRefresh } from '#lib/hooks/use-environment-refresh.svelte.js';
	import { simpleRefresh } from '#lib/utils/api.js';
	import SwarmNodesTable from './nodes-table.svelte';
	import { hasPermission } from '#lib/utils/auth.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import { createRefreshActionButtons } from '#lib/utils/resource-actions.js';

	let { data } = $props();

	let nodes = $derived(data.nodes);
	let requestOptions = $derived(data.requestOptions);
	let isLoading = $state({ refresh: false });

	const currentEnvironmentId = $derived(environmentStore.selected?.id ?? null);

	async function refresh() {
		const environmentId = await environmentStore.getCurrentEnvironmentId();
		await simpleRefresh(
			() => swarmService.getNodes(requestOptions),
			(data) => {
				if (environmentId === currentEnvironmentId) nodes = data;
			},
			m.common_refresh_failed({ resource: m.nodes() }),
			(loading) => (isLoading.refresh = loading)
		);
	}

	useEnvironmentRefresh(refresh);

	onMount(() => {
		let active = true;
		let reconciledEnvironmentId: string | null = null;
		async function reconcileSelectedEnvironment() {
			await environmentStore.ready;
			const environmentId = environmentStore.selected?.id;
			if (!active || !environmentId || !hasPermission('swarm:nodes', environmentId) || reconciledEnvironmentId === environmentId)
				return;
			reconciledEnvironmentId = environmentId;
			const result = await tryCatch(swarmService.reconcileNodeAgents());
			if (!active || environmentId !== environmentStore.selected?.id || result.error !== null) return;
			await refresh();
		}
		const unsubscribeUser = userStore.onChange(() => {
			void reconcileSelectedEnvironment();
		});
		const unsubscribeEnvironment = environmentStore.subscribeSelected(() => {
			void reconcileSelectedEnvironment();
		});
		return () => {
			active = false;
			unsubscribeUser();
			unsubscribeEnvironment();
		};
	});

	const totalNodes = $derived(nodes?.pagination?.totalItems ?? nodes?.data?.length ?? 0);
	const uncoveredNodes = $derived(
		(nodes?.data ?? []).filter((node) => node.agent?.state !== 'connected' || node.agent?.connected === false)
	);
	const uncoveredNodeCount = $derived(uncoveredNodes.length);

	const actionButtons = $derived(
		createRefreshActionButtons({
			refreshLabel: m.common_refresh(),
			onRefresh: refresh,
			refreshing: isLoading.refresh
		})
	);

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
