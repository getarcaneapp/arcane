<script lang="ts">
	import { goto } from '$app/navigation';

	import { useEnvironmentRefresh } from '#lib/hooks/use-environment-refresh.svelte.js';
	import { LayersIcon } from '#lib/icons/index.js';
	import { ResourcePageLayout, type StatCardConfig } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { PERMISSIONS } from '#lib/permissions.generated.js';
	import { swarmService } from '#lib/services/swarm-service.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import { simpleRefresh } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { createRefreshActionButtons } from '#lib/utils/resource-actions.js';

	import SwarmStacksTable from './components/stacks-table.svelte';

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
	const canCreateStack = $derived(hasPermission(PERMISSIONS.SWARM_STACKS, currentEnvId));

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
			iconColor: 'text-info'
		}
	]);
</script>

<ResourcePageLayout
	title={m.swarm_stacks_title()}
	subtitle={m.swarm_stacks_subtitle()}
	{actionButtons}
	{statCards}
	environmentPicker
>
	{#snippet mainContent()}
		<SwarmStacksTable bind:stacks bind:requestOptions />
	{/snippet}
</ResourcePageLayout>
