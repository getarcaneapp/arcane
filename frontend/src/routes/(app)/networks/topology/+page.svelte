<script lang="ts">
	import { createQuery } from '@tanstack/svelte-query';
	import NetworkDiagram from '$lib/components/network-diagram/network-diagram.svelte';
	import { queryKeys } from '$lib/query/query-keys';
	import { ResourcePageLayout, type ActionButton } from '$lib/layouts/index.js';
	import { m } from '$lib/paraglide/messages';
	import { networkService } from '$lib/services/network-service';
	import { environmentStore } from '$lib/stores/environment.store.svelte';

	let { data } = $props();

	const envId = $derived(environmentStore.selected?.id || '0');

	const topologyQuery = createQuery(() => ({
		queryKey: queryKeys.networks.topology(envId),
		queryFn: () => networkService.getNetworkTopology(envId),
		initialData: data.topology
	}));
	const topology = $derived(topologyQuery.data!);

	async function refresh() {
		await topologyQuery.refetch();
	}

	const isRefreshing = $derived(topologyQuery.isFetching && !topologyQuery.isPending);

	const actionButtons: ActionButton[] = $derived([
		{
			id: 'refresh',
			action: 'restart',
			label: m.common_refresh(),
			onclick: refresh,
			loading: isRefreshing,
			disabled: isRefreshing
		}
	]);
</script>

<ResourcePageLayout title={m.networks_topology_title()} subtitle={m.networks_topology_subtitle()} {actionButtons}>
	{#snippet mainContent()}
		<NetworkDiagram {topology} />
	{/snippet}
</ResourcePageLayout>
