<script lang="ts">
	import SwarmKvFormSheet from '#lib/components/sheets/swarm-kv-form-sheet.svelte';
	import { useEnvironmentRefresh } from '#lib/hooks/use-environment-refresh.svelte.js';
	import { ResourcePageLayout, type StatCardConfig } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { swarmService } from '#lib/services/swarm-service.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared.js';
	import type { SwarmKvSummary } from '#lib/types/swarm.js';
	import { handleApiResultWithCallbacks, simpleRefresh } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { createRefreshActionButtons } from '#lib/utils/resource-actions.js';
	import type { SwarmKvResource } from '#lib/utils/swarm-kv.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import SwarmKvTable from './swarm-kv-table.svelte';

	let {
		resource,
		initialItems,
		initialRequestOptions
	}: {
		resource: SwarmKvResource;
		initialItems: Paginated<SwarmKvSummary>;
		initialRequestOptions: SearchPaginationSortRequest;
	} = $props();

	let items = $derived(initialItems);
	let requestOptions = $derived(initialRequestOptions);
	let refreshing = $state(false);
	let isSheetOpen = $state(false);
	let sheetSession = $state(0);
	let itemToEdit = $state<SwarmKvSummary | null>(null);

	async function refresh() {
		await simpleRefresh(
			() => swarmService.getKvItems(resource.kind, requestOptions),
			(data) => (items = data),
			m.common_refresh_failed({ resource: resource.title() }),
			(loading) => (refreshing = loading)
		);
	}

	useEnvironmentRefresh(async () => {
		isSheetOpen = false;
		await refresh();
	});

	function openSheet(item: SwarmKvSummary | null) {
		itemToEdit = item;
		sheetSession += 1;
		isSheetOpen = true;
	}

	async function openEditSheet(item: SwarmKvSummary) {
		await handleApiResultWithCallbacks({
			result: await tryCatch(swarmService.getKvItem(resource.kind, item.id)),
			message: m.common_load_failed({ resource: `${resource.resource()} "${item.spec.Name}"` }),
			onSuccess: (latest) => openSheet(latest)
		});
	}

	async function handleSaved() {
		isSheetOpen = false;
		await refresh();
	}

	const actionButtons = $derived(
		createRefreshActionButtons({
			create: {
				allowed: hasPermission(resource.permission, environmentStore.selected?.id),
				label: resource.createLabel(),
				onclick: () => openSheet(null)
			},
			refreshLabel: m.common_refresh(),
			onRefresh: refresh,
			refreshing
		})
	);

	const statCards: StatCardConfig[] = $derived([
		{
			title: resource.title(),
			value: items.pagination?.totalItems ?? items.data.length,
			icon: resource.icon,
			iconColor: 'text-info'
		}
	]);
</script>

<ResourcePageLayout title={resource.title()} subtitle={resource.subtitle()} {actionButtons} {statCards} environmentPicker>
	{#snippet mainContent()}
		<SwarmKvTable {resource} bind:items bind:requestOptions onEdit={openEditSheet} />
	{/snippet}

	{#snippet additionalContent()}
		{#if sheetSession > 0}
			{#key sheetSession}
				<SwarmKvFormSheet bind:open={isSheetOpen} {resource} existing={itemToEdit} onSaved={handleSaved} />
			{/key}
		{/if}
	{/snippet}
</ResourcePageLayout>
