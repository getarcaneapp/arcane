<script lang="ts">
	import { LayersIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import { swarmService } from '$lib/services/swarm-service';
	import { untrack } from 'svelte';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '$lib/layouts/index.js';
	import { useEnvironmentRefresh } from '$lib/hooks/use-environment-refresh.svelte';
	import { parallelRefresh } from '$lib/utils/refresh.util';
	import SwarmStacksTable from './stacks-table.svelte';
	import { goto } from '$app/navigation';

	let { data } = $props();

	let stacks = $state(untrack(() => data.stacks));
	let requestOptions = $state(untrack(() => data.requestOptions));
	let isLoading = $state({ refresh: false });

	async function refresh() {
		await parallelRefresh(
			{
				stacks: {
					fetch: () => swarmService.getStacks(requestOptions),
					onSuccess: (data) => {
						stacks = data;
					},
					errorMessage: m.common_refresh_failed({ resource: m.swarm_stacks_title() })
				}
			},
			(v) => (isLoading.refresh = v)
		);
	}

	useEnvironmentRefresh(refresh);

	const totalStacks = $derived(stacks?.pagination?.totalItems ?? stacks?.data?.length ?? 0);

	const actionButtons: ActionButton[] = $derived([
		{
			id: 'create',
			action: 'create',
			label: m.common_create_button({ resource: m.swarm_stack() }),
			onclick: () => goto('/swarm/stacks/new')
		},
		{
			id: 'refresh',
			action: 'restart',
			label: m.common_refresh(),
			onclick: refresh,
			loading: isLoading.refresh,
			disabled: isLoading.refresh
		}
	]);

	const statCards: StatCardConfig[] = $derived([
		{
			title: m.swarm_stacks_total(),
			value: totalStacks,
			icon: LayersIcon,
			iconColor: 'text-blue-500'
		}
	]);
</script>

<ResourcePageLayout title={m.swarm_stacks_title()} subtitle={m.swarm_stacks_subtitle()} {actionButtons} {statCards}>
	{#snippet mainContent()}
		<SwarmStacksTable bind:stacks bind:requestOptions />
	{/snippet}
</ResourcePageLayout>
