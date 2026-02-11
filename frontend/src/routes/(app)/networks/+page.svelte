<script lang="ts">
	import { NetworksIcon, ConnectionIcon } from '$lib/icons';
	import { toast } from 'svelte-sonner';
	import type { NetworkCreateOptions, NetworkUsageCounts } from '$lib/types/network.type';
	import CreateNetworkSheet from '$lib/components/sheets/create-network-sheet.svelte';
	import NetworkTable from './network-table.svelte';
	import { m } from '$lib/paraglide/messages';
	import { networkService } from '$lib/services/network-service';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { queryKeys } from '$lib/query/query-keys';
	import { untrack } from 'svelte';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '$lib/layouts/index.js';
	import { createMutation, createQuery } from '@tanstack/svelte-query';

	let { data } = $props();

	let networks = $state(untrack(() => data.networks));
	let requestOptions = $state(untrack(() => data.networkRequestOptions));
	let selectedIds = $state<string[]>([]);
	let isCreateDialogOpen = $state(false);
	const envId = $derived(environmentStore.selected?.id || '0');
	const countsFallback: NetworkUsageCounts = { inuse: 0, unused: 0, total: 0 };

	const networksQuery = createQuery(() => ({
		queryKey: queryKeys.networks.list(envId, requestOptions),
		queryFn: () => networkService.getNetworksForEnvironment(envId, requestOptions),
		initialData: data.networks
	}));

	const createNetworkMutation = createMutation(() => ({
		mutationKey: ['networks', 'create', envId],
		mutationFn: ({ name, options }: { name: string; options: NetworkCreateOptions }) =>
			networkService.createNetwork(name, options),
		onSuccess: async (_data, variables) => {
			toast.success(m.common_create_success({ resource: `${m.resource_network()} "${variables.name}"` }));
			await networksQuery.refetch();
			isCreateDialogOpen = false;
		},
		onError: (_error, variables) => {
			toast.error(m.common_create_failed({ resource: `${m.resource_network()} "${variables.name}"` }));
		}
	}));

	$effect(() => {
		if (networksQuery.data) {
			networks = networksQuery.data;
		}
	});

	async function handleCreate(name: string, options: NetworkCreateOptions) {
		await createNetworkMutation.mutateAsync({ name, options });
	}

	async function refresh() {
		await networksQuery.refetch();
	}

	const isRefreshing = $derived(networksQuery.isFetching && !networksQuery.isPending);
	const networkUsageCounts = $derived(networks.counts ?? countsFallback);

	const actionButtons: ActionButton[] = $derived([
		{
			id: 'create',
			action: 'create',
			label: m.common_create_button({ resource: m.resource_network_cap() }),
			onclick: () => (isCreateDialogOpen = true),
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
			bind:networks
			bind:selectedIds
			bind:requestOptions
			onRefreshData={async (options) => {
				requestOptions = options;
				await networksQuery.refetch();
			}}
		/>
	{/snippet}

	{#snippet additionalContent()}
		<CreateNetworkSheet bind:open={isCreateDialogOpen} isLoading={createNetworkMutation.isPending} onSubmit={handleCreate} />
	{/snippet}
</ResourcePageLayout>
