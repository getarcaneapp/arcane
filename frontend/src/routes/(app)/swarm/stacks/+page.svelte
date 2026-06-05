<script lang="ts">
	import { LayersIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import { swarmService } from '$lib/services/swarm-service';
	import { untrack } from 'svelte';
	import { ResourcePageLayout, type StatCardConfig } from '$lib/layouts/index.js';
	import { useEnvironmentRefresh } from '$lib/hooks/use-environment-refresh.svelte';
	import { parallelRefresh } from '$lib/utils/api';
	import { createRefreshActionButtons } from '$lib/utils/resource-actions';
	import SwarmStacksTable from './stacks-table.svelte';
	import { goto } from '$app/navigation';
	import { hasPermission } from '$lib/utils/auth';
	import { environmentStore } from '$lib/stores/environment.store.svelte';

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

	const currentEnvId = $derived(environmentStore.selected?.id);
	const canCreateStack = $derived(hasPermission('swarm:stacks', currentEnvId));

	const actionButtons = $derived.by(() =>
		createRefreshActionButtons({
			canCreate: canCreateStack,
			createLabel: m.common_create_button({ resource: m.swarm_stack() }),
			onCreate: () => goto('/swarm/stacks/new'),
			refreshLabel: m.common_refresh(),
			onRefresh: refresh,
			refreshing: isLoading.refresh
		})
	);

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
