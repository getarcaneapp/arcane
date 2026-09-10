<script lang="ts">
	import { goto } from '$app/navigation';
	import { NetworksIcon, ConnectionIcon } from '#lib/icons/index.js';
	import { GitBranchIcon } from '#lib/icons/index.js';
	import { toast } from 'svelte-sonner';
	import type { NetworkCreateOptions, NetworkUsageCounts } from '#lib/types/docker.js';
	import CreateNetworkSheet from '#lib/components/sheets/create-network-sheet.svelte';
	import NetworkTable from './network-table.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { networkService } from '#lib/services/network-service.js';
	import { ResourceListPageState } from '#lib/utils/resource-list-page.svelte.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { untrack } from 'svelte';
	import { useEnvironmentRefresh } from '#lib/hooks/use-environment-refresh.svelte.js';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '#lib/layouts/index.js';
	import { createMutation, createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { activityToastOptions, extractActivityId } from '#lib/utils/activity-toast.js';

	let { data } = $props();
	const queryClient = useQueryClient();

	const pageState = new ResourceListPageState(untrack(() => data.networkRequestOptions));
	const countsFallback: NetworkUsageCounts = { inuse: 0, unused: 0, total: 0 };

	const networksQuery = createQuery(() => {
		const queryEnvId = pageState.envId;
		const options = pageState.requestOptions;
		return {
			queryKey: queryKeys.networks.list(queryEnvId, options),
			queryFn: () => networkService.getNetworksForEnvironment(queryEnvId, options),
			placeholderData: (previous, query) => {
				if (query?.queryKey[1] === queryEnvId) return previous;
				return undefined;
			},
			initialData: data.envId === queryEnvId ? data.networks : undefined,
			select: (value) => ({ envId: queryEnvId, value })
		};
	});
	const resourcesReady = $derived(networksQuery.data?.envId === pageState.envId);
	const networks = $derived(networksQuery.data?.value ?? data.networks);

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

	useEnvironmentRefresh(() => {
		pageState.selectedIds = [];
		pageState.isCreateDialogOpen = false;
	});

	async function handleCreate(name: string, options: NetworkCreateOptions) {
		await createNetworkMutation.mutateAsync({ name, options, requestedEnvId: pageState.envId });
	}

	async function loadNetworks(options = pageState.requestOptions, requestedEnvId = pageState.envId) {
		pageState.requestOptions = options;
		await queryClient.query({
			queryKey: queryKeys.networks.list(requestedEnvId, options),
			queryFn: () => networkService.getNetworksForEnvironment(requestedEnvId, options)
		});
	}

	async function refresh() {
		await loadNetworks();
	}

	const isRefreshing = $derived(networksQuery.isFetching && !networksQuery.isPending);
	const networkUsageCounts = $derived.by(() => {
		if (resourcesReady) return networks.counts ?? countsFallback;
		return countsFallback;
	});

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
			{#key pageState.envId}
				<NetworkTable
					bind:networks={
						() => networks,
						(value) => queryClient.setQueryData(queryKeys.networks.list(pageState.envId, pageState.requestOptions), value)
					}
					bind:selectedIds={pageState.selectedIds}
					bind:requestOptions={pageState.requestOptions}
					onRefreshData={loadNetworks}
				/>
			{/key}
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
