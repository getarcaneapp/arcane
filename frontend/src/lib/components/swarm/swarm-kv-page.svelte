<script lang="ts">
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { untrack } from 'svelte';

	import SwarmKvFormSheet from '#lib/components/sheets/swarm-kv-form-sheet.svelte';
	import { swarmEnvironmentsQueryOptions, type SwarmEnvironments } from '#lib/components/swarm/swarm-environments.js';
	import { ResourcePageLayout, type StatCardConfig } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { swarmService } from '#lib/services/swarm-service.js';
	import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared.js';
	import type { SwarmKvSummary } from '#lib/types/swarm.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import {
		destinationEnvironmentId,
		fetchAcrossEnvironments,
		withFailedEnvironments,
		type EnvironmentPage,
		type EnvironmentRow
	} from '#lib/utils/environments.js';
	import { createRefreshActionButtons } from '#lib/utils/resource-actions.js';
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

	let requestOptions = $state<SearchPaginationSortRequest>(untrack(() => initialRequestOptions));
	let isSheetOpen = $state(false);
	let sheetSession = $state(0);
	let itemToEdit = $state<SwarmKvSummary | null>(null);
	let sheetEnvironmentId = $state('');

	function itemsQueryOptions(options: SearchPaginationSortRequest, current: SwarmEnvironments = swarm) {
		const kind = resource.kind;
		const { environments, failedEnvironments, scope } = current;
		return {
			queryKey: queryKeys.swarm[kind](scope, options),
			queryFn: async () =>
				withFailedEnvironments(
					await fetchAcrossEnvironments(environments, options, (id, request, config) =>
						swarmService.getKvItems(kind, request, id, config)
					),
					failedEnvironments
				)
		};
	}

	const itemsQuery = createQuery(() => ({
		...itemsQueryOptions(requestOptions),
		placeholderData: (previous) => previous
	}));
	let items = $derived(itemsQuery.data ?? initialItems);
	const refreshing = $derived(itemsQuery.isFetching && !itemsQuery.isPending);

	function fetchItems(options: SearchPaginationSortRequest, current: SwarmEnvironments = swarm) {
		requestOptions = options;
		return queryClient.query({ ...itemsQueryOptions(options, current), staleTime: 0 });
	}

	async function refresh() {
		const discovered = await swarmQuery.refetch();
		await handleApiResultWithCallbacks({
			result: await tryCatch(fetchItems(requestOptions, discovered.data ?? swarm)),
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
		await refresh();
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
		<SwarmKvTable {resource} bind:items bind:requestOptions {fetchItems} onEdit={openEditSheet} />
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
