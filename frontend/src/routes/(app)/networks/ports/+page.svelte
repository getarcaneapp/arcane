<script lang="ts">
	import { createQuery } from '@tanstack/svelte-query';
	import { tick } from 'svelte';

	import { ResourcePageLayout, type ActionButton } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { portService } from '#lib/services/port-service.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import { fetchAcrossEnvironments, readableEnvironments } from '#lib/utils/environments.js';

	import PortTable from './components/port-table.svelte';

	let { data } = $props();

	let requestOptions = $derived(data.portRequestOptions);
	let selectedIds = $state<string[]>([]);

	const scope = $derived(environmentStore.shownKey);

	// The load already cached the first page under its key; seeding every key with it would hide other pages and filters.
	const portsQuery = createQuery(() => {
		const queryScope = scope;
		const environments = readableEnvironments('containers:list');
		const options = requestOptions;
		return {
			queryKey: queryKeys.ports.list(queryScope, options),
			queryFn: () =>
				fetchAcrossEnvironments(environments, options, (id, request, config) =>
					portService.getPortsForEnvironment(id, request, config)
				),
			placeholderData: (previous, query) => {
				if (query?.queryKey[1] === queryScope) return previous;
				return undefined;
			},
			select: (value) => ({ envId: queryScope, value })
		};
	});
	const ports = $derived(portsQuery.data?.envId === scope ? portsQuery.data.value : null);

	async function refresh() {
		await portsQuery.refetch();
	}

	const isRefreshing = $derived(portsQuery.isFetching && !portsQuery.isPending);

	const actionButtons: ActionButton[] = $derived([
		{
			id: 'refresh',
			action: 'refresh',
			placement: 'secondary',
			iconOnly: true,
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
					// Let the query pick up the new key first; unchanged options (Retry) still refetch.
					await tick();
					await portsQuery.refetch({ cancelRefetch: false });
				}}
			/>
		{/if}
	{/snippet}
</ResourcePageLayout>
