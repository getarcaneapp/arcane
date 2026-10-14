<script lang="ts">
	import { createMutation, createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { untrack } from 'svelte';
	import { toast } from 'svelte-sonner';

	import CreateNetworkSheet from '#lib/components/sheets/create-network-sheet.svelte';
	import { useEnvironmentRefresh } from '#lib/hooks/use-environment-refresh.svelte.js';
	import { NetworksIcon, ConnectionIcon } from '#lib/icons/index.js';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { networkService } from '#lib/services/network-service.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import type { NetworkCreateOptions, NetworkUsageCounts } from '#lib/types/docker.js';
	import { activityToastOptions, extractActivityId } from '#lib/utils/activity-toast.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';
	import { fetchAcrossEnvironments } from '#lib/utils/environments.js';
	import { ResourceListPageState } from '#lib/utils/resource-list-page.svelte.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import NetworkTable from './components/network-table.svelte';

	let { data } = $props();
	const queryClient = useQueryClient();

	const pageState = new ResourceListPageState(untrack(() => data.networkRequestOptions));
	const countsFallback: NetworkUsageCounts = { inuse: 0, unused: 0, total: 0 };

	// The load already cached the first page under its key; seeding every key with it would hide other pages and filters.
	const networksQuery = createQuery(() => {
		const queryScope = pageState.scope;
		const environments = environmentStore.shown;
		const options = pageState.requestOptions;
		return {
			queryKey: queryKeys.networks.list(queryScope, options),
			queryFn: () =>
				fetchAcrossEnvironments(environments, options, (id, request, config) =>
					networkService.getNetworksForEnvironment(id, request, config)
				),
			placeholderData: (previous, query) => {
				if (query?.queryKey[1] === queryScope) return previous;
				return undefined;
			},
			select: (value) => ({ envId: queryScope, value })
		};
	});
	const resourcesReady = $derived(networksQuery.data?.envId === pageState.scope);
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
			if (environmentStore.shown.some((env) => env.id === variables.requestedEnvId)) {
				await loadNetworks();
				pageState.isCreateDialogOpen = false;
			}
		},
		onError: (error, variables) => {
			toast.error(m.common_create_failed({ resource: `${m.resource_network()} "${variables.name}"` }), {
				description: extractApiErrorMessage(error)
			});
		}
	}));

	useEnvironmentRefresh(() => {
		pageState.selectedIds = [];
		pageState.isCreateDialogOpen = false;
	});

	async function handleCreate(name: string, options: NetworkCreateOptions, environmentId: string) {
		await tryCatch(createNetworkMutation.mutateAsync({ name, options, requestedEnvId: environmentId }));
	}

	async function loadNetworks(options = pageState.requestOptions, requestedScope = pageState.scope) {
		const environments = environmentStore.shown;
		pageState.requestOptions = options;
		await queryClient.query({
			queryKey: queryKeys.networks.list(requestedScope, options),
			queryFn: () =>
				fetchAcrossEnvironments(environments, options, (id, request, config) =>
					networkService.getNetworksForEnvironment(id, request, config)
				),
			staleTime: 0
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
			placement: 'primary',
			label: m.common_create_button({ resource: m.resource_network_cap() }),
			onclick: () => (pageState.isCreateDialogOpen = true),
			loading: createNetworkMutation.isPending,
			disabled: !resourcesReady || createNetworkMutation.isPending
		},
		{
			id: 'refresh',
			action: 'refresh',
			placement: 'secondary',
			iconOnly: true,
			label: m.common_refresh(),
			onclick: refresh,
			loading: isRefreshing,
			disabled: networksQuery.isFetching
		}
	]);

	const statCards: StatCardConfig[] = $derived([
		{
			title: m.networks_total(),
			value: networkUsageCounts.total,
			icon: NetworksIcon,
			iconColor: 'text-info'
		},
		{
			title: m.unused_networks(),
			value: networkUsageCounts.unused,
			icon: ConnectionIcon,
			iconColor: 'text-warning'
		}
	]);
</script>

<ResourcePageLayout title={m.resource_networks_cap()} subtitle={m.networks_subtitle()} {actionButtons} {statCards}>
	{#snippet mainContent()}
		{#if resourcesReady}
			{#key pageState.scope}
				<NetworkTable
					bind:networks={
						() => networks,
						(value) => queryClient.setQueryData(queryKeys.networks.list(pageState.scope, pageState.requestOptions), value)
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
