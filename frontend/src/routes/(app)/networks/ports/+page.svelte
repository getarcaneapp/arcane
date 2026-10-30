<script lang="ts">
	import { createQuery } from '@tanstack/svelte-query';
	import { untrack } from 'svelte';

	import { ResourcePageLayout, type ActionButton } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { portListQuery } from '#lib/query/networks.js';
	import { readableEnvironments } from '#lib/utils/environments.js';
	import { ResourceListPageState } from '#lib/utils/resource-list-page.svelte.js';

	import PortTable from './components/port-table.svelte';

	let { data } = $props();

	const pageState = new ResourceListPageState(untrack(() => data.portRequestOptions));

	const portsQuery = createQuery(() => {
		const scope = pageState.scope;
		return {
			...portListQuery(scope, readableEnvironments('containers:list'), pageState.requestOptions),
			placeholderData: (previous, query) => (query?.queryKey[1] === scope ? previous : undefined),
			select: (value) => ({ envId: scope, value })
		};
	});

	const resourcesReady = $derived(portsQuery.data?.envId === pageState.scope);
	const ports = $derived(portsQuery.data?.value ?? data.ports);
	const isRefreshing = $derived(portsQuery.isFetching && !portsQuery.isPending);

	const actionButtons: ActionButton[] = $derived([
		{
			id: 'refresh',
			action: 'refresh',
			placement: 'secondary',
			iconOnly: true,
			label: m.common_refresh(),
			onclick: () => portsQuery.refetch(),
			loading: isRefreshing,
			disabled: portsQuery.isFetching
		}
	]);
</script>

<ResourcePageLayout title={m.common_ports()} subtitle={m.ports_subtitle()} {actionButtons}>
	{#snippet mainContent()}
		{#if resourcesReady}
			<PortTable
				{ports}
				bind:selectedIds={pageState.selectedIds}
				bind:requestOptions={pageState.requestOptions}
				onRetry={() => portsQuery.refetch()}
			/>
		{/if}
	{/snippet}
</ResourcePageLayout>
