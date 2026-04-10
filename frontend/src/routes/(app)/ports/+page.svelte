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

	const portsQuery = createQuery(() => ({
		queryKey: queryKeys.ports.list(envId, requestOptions),
		queryFn: () => portService.getPortsForEnvironment(envId, requestOptions),
		initialData: data.ports
	}));
	const ports = $derived(portsQuery.data!);

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
			disabled: isRefreshing
		}
	]);
</script>

<ResourcePageLayout title={m.ports_title()} subtitle={m.ports_subtitle()} {actionButtons}>
	{#snippet mainContent()}
		<PortTable
			{ports}
			bind:selectedIds
			bind:requestOptions
			onRefreshData={async (options) => {
				requestOptions = options;
				await portsQuery.refetch();
			}}
		/>
	{/snippet}
</ResourcePageLayout>
