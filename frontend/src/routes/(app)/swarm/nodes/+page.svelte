<script lang="ts">
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { onMount, untrack } from 'svelte';

	import { swarmEnvironmentsQueryOptions, type SwarmEnvironments } from '#lib/components/swarm/swarm-environments.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { AlertTriangleIcon, UsersIcon } from '#lib/icons/index.js';
	import { ResourcePageLayout, type StatCardConfig } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { swarmService } from '#lib/services/swarm-service.js';
	import userStore from '#lib/stores/user-store.svelte.js';
	import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { fetchAcrossEnvironments, withFailedEnvironments } from '#lib/utils/environments.js';
	import { createRefreshActionButtons } from '#lib/utils/resource-actions.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import SwarmNodesTable from './components/nodes-table.svelte';

	let { data } = $props();
	const queryClient = useQueryClient();

	let requestOptions = $state<SearchPaginationSortRequest>(untrack(() => data.requestOptions));
	const swarmQuery = createQuery(() => swarmEnvironmentsQueryOptions('swarm:read'));
	const swarm = $derived(swarmQuery.data ?? data.swarm);

	function nodesQueryOptions(options: SearchPaginationSortRequest, current: SwarmEnvironments = swarm) {
		const { environments, failedEnvironments, scope } = current;
		return {
			queryKey: queryKeys.swarm.nodes(scope, options),
			queryFn: async () =>
				withFailedEnvironments(
					await fetchAcrossEnvironments(environments, options, (id, request, config) =>
						swarmService.getNodes(request, id, config)
					),
					failedEnvironments
				)
		};
	}

	const nodesQuery = createQuery(() => ({
		...nodesQueryOptions(requestOptions),
		placeholderData: (previous) => previous
	}));
	let nodes = $derived(nodesQuery.data ?? data.nodes);
	const isRefreshing = $derived(nodesQuery.isFetching && !nodesQuery.isPending);

	function fetchNodes(options: SearchPaginationSortRequest, current: SwarmEnvironments = swarm) {
		requestOptions = options;
		return queryClient.query({ ...nodesQueryOptions(options, current), staleTime: 0 });
	}

	async function refresh() {
		const discovered = await swarmQuery.refetch();
		await handleApiResultWithCallbacks({
			result: await tryCatch(fetchNodes(requestOptions, discovered.data ?? swarm)),
			message: m.common_refresh_failed({ resource: m.nodes() })
		});
	}

	onMount(() => {
		let active = true;
		const reconciled = new Set<string>();
		async function reconcileEnvironments() {
			const pending = swarm.managers.filter((env) => !reconciled.has(env.id) && hasPermission('swarm:nodes', env.id));
			if (pending.length === 0) return;
			for (const env of pending) reconciled.add(env.id);
			const results = await Promise.all(pending.map((env) => tryCatch(swarmService.reconcileNodeAgents(env.id))));
			if (!active || results.every((result) => result.error !== null)) return;
			await refresh();
		}
		const unsubscribeUser = userStore.onChange(() => {
			void reconcileEnvironments();
		});
		return () => {
			active = false;
			unsubscribeUser();
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
			refreshing: isRefreshing
		})
	);

	const statCards: StatCardConfig[] = $derived([
		{
			title: m.swarm_nodes_total(),
			value: totalNodes,
			icon: UsersIcon,
			iconColor: 'text-info'
		}
	]);
</script>

<ResourcePageLayout title={m.nodes()} subtitle={m.swarm_nodes_subtitle()} {actionButtons} {statCards}>
	{#snippet mainContent()}
		<div class="space-y-4">
			{#if uncoveredNodeCount > 0}
				<Alert.Root variant="warning-subtle">
					<AlertTriangleIcon class="size-4 text-warning" />
					<Alert.Title>{m.swarm_node_agent_warning_title()}</Alert.Title>
					<Alert.Description>{m.swarm_node_agent_warning_description({ count: uncoveredNodeCount })}</Alert.Description>
				</Alert.Root>
			{/if}

			<SwarmNodesTable bind:nodes bind:requestOptions {fetchNodes} />
		</div>
	{/snippet}
</ResourcePageLayout>
