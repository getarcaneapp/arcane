<script lang="ts">
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { untrack } from 'svelte';

	import SwarmKvFormSheet from '#lib/components/sheets/swarm-kv-form-sheet.svelte';
	import { swarmEnvironmentsQueryOptions, type SwarmEnvironments } from '#lib/components/swarm/swarm-environments.js';
	import { ResourcePageLayout, type StatCardConfig } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { swarmKvListQuery } from '#lib/query/swarm.js';
	import { swarmService } from '#lib/services/swarm-service.js';
	import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared.js';
	import type { SwarmKvSummary } from '#lib/types/swarm.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { destinationEnvironmentId, type EnvironmentPage, type EnvironmentRow } from '#lib/utils/environments.js';
	import { createRefreshActionButtons } from '#lib/utils/resource-actions.js';
	import { ResourceListPageState } from '#lib/utils/resource-list-page.svelte.js';
	import type { SwarmKvResource } from '#lib/utils/swarm-kv.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import SwarmKvTable from './swarm-kv-table.svelte';

	let {
		resource,
		initialSwarm,
		initialItems,
		initialRequestOptions
	}: {
		resource: SwarmKvResource;
		initialSwarm: SwarmEnvironments;
		initialItems: EnvironmentPage<Paginated<SwarmKvSummary>>;
		initialRequestOptions: SearchPaginationSortRequest;
	} = $props();

	const queryClient = useQueryClient();
	const swarmQuery = createQuery(() => swarmEnvironmentsQueryOptions('swarm:read'));
	const swarm = $derived(swarmQuery.data ?? initialSwarm);

	const pageState = new ResourceListPageState<SearchPaginationSortRequest>(untrack(() => initialRequestOptions));
	let isSheetOpen = $state(false);
	let sheetSession = $state(0);
	let itemToEdit = $state<SwarmKvSummary | null>(null);
	let sheetEnvironmentId = $state('');

	const itemsQuery = createQuery(() => {
		const scope = swarm.scope;
		return {
			...swarmKvListQuery(swarm, resource.kind, pageState.requestOptions),
			placeholderData: (previous, query) => (query?.queryKey[1] === scope ? previous : undefined),
			select: (value) => ({ envId: scope, value })
		};
	});
	const items = $derived(itemsQuery.data?.value ?? initialItems);
	const refreshing = $derived(itemsQuery.isFetching && !itemsQuery.isPending);

	async function refresh() {
		await swarmQuery.refetch();
		await handleApiResultWithCallbacks({
			result: await tryCatch(itemsQuery.refetch({ throwOnError: true })),
			message: m.common_refresh_failed({ resource: resource.title() })
		});
	}

	function openSheet(item: SwarmKvSummary | null, environmentId: string) {
		itemToEdit = item;
		sheetEnvironmentId = environmentId;
		sheetSession += 1;
		isSheetOpen = true;
	}

	async function openEditSheet(item: EnvironmentRow<SwarmKvSummary>) {
		await handleApiResultWithCallbacks({
			result: await tryCatch(swarmService.getKvItem(resource.kind, item.id, item.environmentId)),
			message: m.common_load_failed({ resource: `${resource.resource()} "${item.spec.Name}"` }),
			onSuccess: (latest) => openSheet(latest, item.environmentId)
		});
	}

	async function handleSaved() {
		isSheetOpen = false;
		await queryClient.invalidateQueries({ queryKey: queryKeys.swarm.listPrefix(swarm.scope, resource.kind) });
	}

	const actionButtons = $derived(
		createRefreshActionButtons({
			create: {
				allowed: swarm.managers.some((env) => hasPermission(resource.permission, env.id)),
				label: resource.createLabel(),
				onclick: () => openSheet(null, destinationEnvironmentId(sheetEnvironmentId, resource.permission, swarm.managers))
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

<ResourcePageLayout title={resource.title()} subtitle={resource.subtitle()} {actionButtons} {statCards}>
	{#snippet mainContent()}
		<SwarmKvTable
			{resource}
			{items}
			scope={swarm.scope}
			bind:requestOptions={pageState.requestOptions}
			onRetry={() => itemsQuery.refetch()}
			onEdit={openEditSheet}
		/>
	{/snippet}

	{#snippet additionalContent()}
		{#if sheetSession > 0}
			{#key sheetSession}
				<SwarmKvFormSheet
					bind:open={isSheetOpen}
					{resource}
					bind:environmentId={sheetEnvironmentId}
					environments={itemToEdit ? undefined : swarm.managers}
					existing={itemToEdit}
					onSaved={handleSaved}
				/>
			{/key}
		{/if}
	{/snippet}
</ResourcePageLayout>
