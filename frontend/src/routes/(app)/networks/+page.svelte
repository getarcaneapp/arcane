<script lang="ts">
	import { goto } from '$app/navigation';
	import { NetworksIcon, ConnectionIcon } from '$lib/icons';
	import { GitBranchIcon } from '$lib/icons';
	import { toast } from 'svelte-sonner';
	import type { NetworkCreateOptions, NetworkUsageCounts } from '$lib/types/docker';
	import CreateNetworkSheet from '$lib/components/sheets/create-network-sheet.svelte';
	import NetworkTable from './network-table.svelte';
	import { m } from '$lib/paraglide/messages';
	import { networkService } from '$lib/services/network-service';
	import { ResourceListPageState } from '$lib/utils/resource-list-page.svelte';
	import { queryKeys } from '$lib/query/query-keys';
	import { untrack } from 'svelte';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '$lib/layouts/index.js';
	import { createMutation, createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { activityToastOptions, extractActivityId } from '$lib/utils/activity-toast';

	let { data } = $props();
	const queryClient = useQueryClient();

	const pageState = new ResourceListPageState(
		untrack(() => data.networks),
		untrack(() => data.networkRequestOptions)
	);
	let previousEnvId = untrack(() => pageState.envId);
	const countsFallback: NetworkUsageCounts = { inuse: 0, unused: 0, total: 0 };

	const networksQuery = createQuery(() => {
		const queryEnvId = pageState.envId;
		return {
			queryKey: queryKeys.networks.list(queryEnvId, pageState.requestOptions),
			queryFn: () => networkService.getNetworksForEnvironment(queryEnvId, pageState.requestOptions),
			initialData: data.envId === queryEnvId ? data.networks : undefined,
			select: (value) => ({ envId: queryEnvId, value })
		};
	});
	let displayedEnvId = $state<string | null>(untrack(() => (data.envId === pageState.envId ? data.envId : null)));
	const resourcesReady = $derived(displayedEnvId === pageState.envId);

	const createNetworkMutation = createMutation(() => ({
		mutationKey: ['networks', 'create', pageState.envId],
		mutationFn: ({ name, options, requestedEnvId }: { name: string; options: NetworkCreateOptions; requestedEnvId: string }) =>
			networkService.createNetwork(name, options, requestedEnvId),
		onSuccess: async (data, variables) => {
			toast.success(
				m.common_create_success({ resource: `${m.resource_network()} "${variables.name}"` }),
				activityToastOptions(extractActivityId(data))
			);
			if (variables.requestedEnvId === pageState.envId) {
				await loadNetworks(pageState.requestOptions, variables.requestedEnvId);
				pageState.isCreateDialogOpen = false;
			}
		},
		onError: (_error, variables) => {
			toast.error(m.common_create_failed({ resource: `${m.resource_network()} "${variables.name}"` }));
		}
	}));

	$effect(() => {
		if (networksQuery.data?.envId === pageState.envId) {
			pageState.items = networksQuery.data.value;
			displayedEnvId = pageState.envId;
		}
	});

	$effect(() => {
		if (pageState.envId === previousEnvId) return;
		previousEnvId = pageState.envId;
		displayedEnvId = null;
		pageState.selectedIds = [];
		pageState.isCreateDialogOpen = false;
	});

	async function handleCreate(name: string, options: NetworkCreateOptions) {
		await createNetworkMutation.mutateAsync({ name, options, requestedEnvId: pageState.envId });
	}

	async function loadNetworks(options = pageState.requestOptions, requestedEnvId = pageState.envId) {
		pageState.requestOptions = options;
		const next = await queryClient.fetchQuery({
			queryKey: queryKeys.networks.list(requestedEnvId, options),
			queryFn: () => networkService.getNetworksForEnvironment(requestedEnvId, options)
		});
		if (requestedEnvId !== pageState.envId) {
			return;
		}
		pageState.items = next;
		displayedEnvId = requestedEnvId;
	}

	async function refresh() {
		await loadNetworks();
	}

	const isRefreshing = $derived(networksQuery.isFetching && !networksQuery.isPending);
	const networkUsageCounts = $derived(resourcesReady ? (pageState.items.counts ?? countsFallback) : countsFallback);

	const actionButtons: ActionButton[] = $derived([
		{
			id: 'create',
			action: 'create',
			label: m.common_create_button({ resource: m.resource_network_cap() }),
			onclick: () => (pageState.isCreateDialogOpen = true),
			loading: createNetworkMutation.isPending,
			disabled: !resourcesReady || createNetworkMutation.isPending
		},
		{
			id: 'refresh',
			action: 'restart',
			label: m.common_refresh(),
			onclick: refresh,
			loading: isRefreshing,
			disabled: networksQuery.isFetching
		},
		{
			id: 'topology',
			action: 'inspect',
			label: m.networks_topology_button(),
			icon: GitBranchIcon,
			onclick: () => void goto('/networks/topology'),
			disabled: !resourcesReady
		}
	]);

	const statCards: StatCardConfig[] = $derived([
		{
			title: m.networks_total(),
			value: networkUsageCounts.total,
			icon: NetworksIcon,
			iconColor: 'text-blue-500'
		},
		{
			title: m.unused_networks(),
			value: networkUsageCounts.unused,
			icon: ConnectionIcon,
			iconColor: 'text-amber-500'
		}
	]);
</script>

<ResourcePageLayout title={m.resource_networks_cap()} subtitle={m.networks_subtitle()} {actionButtons} {statCards}>
	{#snippet mainContent()}
		{#if resourcesReady}
			<NetworkTable
				bind:networks={pageState.items}
				bind:selectedIds={pageState.selectedIds}
				bind:requestOptions={pageState.requestOptions}
				onRefreshData={loadNetworks}
			/>
		{/if}
	{/snippet}

	{#snippet additionalContent()}
		<CreateNetworkSheet
			bind:open={pageState.isCreateDialogOpen}
			isLoading={createNetworkMutation.isPending}
			onSubmit={handleCreate}
		/>
	{/snippet}
</ResourcePageLayout>
