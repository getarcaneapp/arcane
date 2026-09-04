<script lang="ts">
	import { LayersIcon } from '#lib/icons';
	import { m } from '#lib/paraglide/messages';
	import { swarmService } from '#lib/services/swarm-service';
	import { ResourcePageLayout, type StatCardConfig } from '#lib/layouts/index.js';
	import { useEnvironmentRefresh } from '#lib/hooks/use-environment-refresh.svelte';
	import { simpleRefresh } from '#lib/utils/api';
	import { createRefreshActionButtons } from '#lib/utils/resource-actions';
	import SwarmStacksTable from './stacks-table.svelte';
	import { goto } from '$app/navigation';
	import { hasPermission } from '#lib/utils/auth';
	import { environmentStore } from '#lib/stores/environment.store.svelte';

	let { data } = $props();

	let stacks = $derived(data.stacks);
	let requestOptions = $derived(data.requestOptions);
	let isLoading = $state({ refresh: false });

	async function refresh() {
		await simpleRefresh(
			() => swarmService.getStacks(requestOptions),
			(data) => (stacks = data),
			m.common_refresh_failed({ resource: m.swarm_stacks_title() }),
			(loading) => (isLoading.refresh = loading)
		);
	}

	useEnvironmentRefresh(refresh);

	const totalStacks = $derived(stacks?.pagination?.totalItems ?? stacks?.data?.length ?? 0);

	const currentEnvId = $derived(environmentStore.selected?.id);
	const canCreateStack = $derived(hasPermission('swarm:stacks', currentEnvId));

	const actionButtons = $derived.by(() =>
		createRefreshActionButtons({
			create: {
				allowed: canCreateStack,
				label: m.common_create_button({ resource: m.swarm_stack() }),
				onclick: () => goto('/swarm/stacks/new')
			},
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
