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
	import { createMutation, createQuery } from '@tanstack/svelte-query';
	import { activityToastOptions, extractActivityId } from '$lib/utils/activity-toast';

	let { data } = $props();

	const pageState = new ResourceListPageState(
		untrack(() => data.networks),
		untrack(() => data.networkRequestOptions)
	);
	const countsFallback: NetworkUsageCounts = { inuse: 0, unused: 0, total: 0 };

	const networksQuery = createQuery(() => ({
		queryKey: queryKeys.networks.list(pageState.envId, pageState.requestOptions),
		queryFn: () => networkService.getNetworksForEnvironment(pageState.envId, pageState.requestOptions),
		initialData: data.networks
	}));

	const createNetworkMutation = createMutation(() => ({
		mutationKey: ['networks', 'create', pageState.envId],
		mutationFn: ({ name, options }: { name: string; options: NetworkCreateOptions }) =>
			networkService.createNetwork(name, options),
		onSuccess: async (data, variables) => {
			toast.success(
				m.common_create_success({ resource: `${m.resource_network()} "${variables.name}"` }),
				activityToastOptions(extractActivityId(data))
			);
			await networksQuery.refetch();
			pageState.isCreateDialogOpen = false;
		},
		onError: (_error, variables) => {
			toast.error(m.common_create_failed({ resource: `${m.resource_network()} "${variables.name}"` }));
		}
	}));

	$effect(() => {
		if (networksQuery.data) {
			pageState.items = networksQuery.data;
		}
	});

	async function handleCreate(name: string, options: NetworkCreateOptions) {
		await createNetworkMutation.mutateAsync({ name, options });
	}

	async function refresh() {
		await networksQuery.refetch();
	}

	const isRefreshing = $derived(networksQuery.isFetching && !networksQuery.isPending);
	const networkUsageCounts = $derived(pageState.items.counts ?? countsFallback);

	const actionButtons: ActionButton[] = $derived([
		{
			id: 'create',
			action: 'create',
			label: m.common_create_button({ resource: m.resource_network_cap() }),
			onclick: () => (pageState.isCreateDialogOpen = true),
			loading: createNetworkMutation.isPending,
			disabled: createNetworkMutation.isPending
		},
		{
			id: 'refresh',
			action: 'restart',
			label: m.common_refresh(),
			onclick: refresh,
			loading: isRefreshing,
			disabled: isRefreshing
		},
		{
			id: 'topology',
			action: 'inspect',
			label: m.networks_topology_button(),
			icon: GitBranchIcon,
			onclick: () => void goto('/networks/topology')
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

<ResourcePageLayout title={m.networks_title()} subtitle={m.networks_subtitle()} {actionButtons} {statCards}>
	{#snippet mainContent()}
		<NetworkTable
			bind:networks={pageState.items}
			bind:selectedIds={pageState.selectedIds}
			bind:requestOptions={pageState.requestOptions}
			onRefreshData={async (options) => {
				pageState.requestOptions = options;
				await networksQuery.refetch();
			}}
		/>
	{/snippet}

	{#snippet additionalContent()}
		<CreateNetworkSheet
			bind:open={pageState.isCreateDialogOpen}
			isLoading={createNetworkMutation.isPending}
			onSubmit={handleCreate}
		/>
	{/snippet}
</ResourcePageLayout>
