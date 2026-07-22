<script lang="ts">
	import { untrack } from 'svelte';
	import { createQuery } from '@tanstack/svelte-query';
	import { ResourcePageLayout, type ActionButton } from '$lib/layouts/index.js';
	import { m } from '$lib/paraglide/messages';
	import { portService } from '$lib/services/port-service';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { queryKeys } from '$lib/query/query-keys';
	import PortTable from './port-table.svelte';

	let { data } = $props();

	let requestOptions = $state(untrack(() => data.portRequestOptions));
	let selectedIds = $state<string[]>([]);

	const envId = $derived(environmentStore.selected?.id || '0');
	let previousEnvId = untrack(() => envId);

	const portsQuery = createQuery(() => {
		const queryEnvId = envId;
		return {
			queryKey: queryKeys.ports.list(queryEnvId, requestOptions),
			queryFn: () => portService.getPortsForEnvironment(queryEnvId, requestOptions),
			initialData: data.envId === queryEnvId ? data.ports : undefined,
			select: (value) => ({ envId: queryEnvId, value })
		};
	});
	const ports = $derived(portsQuery.data?.envId === envId ? portsQuery.data.value : null);

	$effect(() => {
		if (envId === previousEnvId) return;
		previousEnvId = envId;
		selectedIds = [];
	});

	async function refresh() {
		await portsQuery.refetch();
	}

	const isRefreshing = $derived(portsQuery.isFetching && !portsQuery.isPending);

	const actionButtons: ActionButton[] = $derived([
		{
			id: 'refresh',
			action: 'restart',
			label: m.common_refresh(),
			onclick: refresh,
			loading: isRefreshing,
			disabled: portsQuery.isFetching
		}
	]);
</script>

<ResourcePageLayout title={m.common_ports()} subtitle={m.ports_subtitle()} {actionButtons}>
	{#snippet mainContent()}
		{#if ports}
			<PortTable
				{ports}
				bind:selectedIds
				bind:requestOptions
				onRefreshData={async (options) => {
					requestOptions = options;
				}}
			/>
		{/if}
	{/snippet}
</ResourcePageLayout>
