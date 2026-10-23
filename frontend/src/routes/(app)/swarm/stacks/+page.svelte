<script lang="ts">
	import { goto } from '$app/navigation';
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { untrack } from 'svelte';

	import { swarmEnvironmentsQueryOptions, type SwarmEnvironments } from '#lib/components/swarm/swarm-environments.js';
	import { LayersIcon } from '#lib/icons/index.js';
	import { ResourcePageLayout, type StatCardConfig } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { swarmService } from '#lib/services/swarm-service.js';
	import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { fetchAcrossEnvironments, withFailedEnvironments } from '#lib/utils/environments.js';
	import { createRefreshActionButtons } from '#lib/utils/resource-actions.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import SwarmStacksTable from './components/stacks-table.svelte';

	let { data } = $props();
	const queryClient = useQueryClient();

	let requestOptions = $state<SearchPaginationSortRequest>(untrack(() => data.requestOptions));
	const swarmQuery = createQuery(() => swarmEnvironmentsQueryOptions('swarm:read'));
	const swarm = $derived(swarmQuery.data ?? data.swarm);

	function stacksQueryOptions(options: SearchPaginationSortRequest, current: SwarmEnvironments = swarm) {
		const { managers, failedEnvironments } = current;
		return {
			queryKey: queryKeys.swarm.stacks(managers.map((env) => env.id).join(','), options),
			queryFn: async () =>
				withFailedEnvironments(
					await fetchAcrossEnvironments(managers, options, (id, request, config) => swarmService.getStacks(request, id, config)),
					failedEnvironments
				)
		};
	}

	const stacksQuery = createQuery(() => ({
		...stacksQueryOptions(requestOptions),
		placeholderData: (previous) => previous
	}));
	let stacks = $derived(stacksQuery.data ?? data.stacks);
	const isRefreshing = $derived(stacksQuery.isFetching && !stacksQuery.isPending);

	function fetchStacks(options: SearchPaginationSortRequest, current: SwarmEnvironments = swarm) {
		requestOptions = options;
		return queryClient.query({ ...stacksQueryOptions(options, current), staleTime: 0 });
	}

	async function refresh() {
		const discovered = await swarmQuery.refetch();
		await handleApiResultWithCallbacks({
			result: await tryCatch(fetchStacks(requestOptions, discovered.data ?? swarm)),
			message: m.common_refresh_failed({ resource: m.swarm_stacks_title() })
		});
	}

	const totalStacks = $derived(stacks?.pagination?.totalItems ?? stacks?.data?.length ?? 0);

	const canCreateStack = $derived(swarm.managers.some((env) => hasPermission('swarm:stacks', env.id)));

	const actionButtons = $derived.by(() =>
		createRefreshActionButtons({
			create: {
				allowed: canCreateStack,
				label: m.common_create_button({ resource: m.swarm_stack() }),
				onclick: () => goto('/swarm/stacks/new')
			},
			refreshLabel: m.common_refresh(),
			onRefresh: refresh,
			refreshing: isRefreshing
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

<ResourcePageLayout title={m.swarm_stacks_title()} subtitle={m.swarm_stacks_subtitle()} {actionButtons} {statCards}>
	{#snippet mainContent()}
		<SwarmStacksTable bind:stacks bind:requestOptions {fetchStacks} />
	{/snippet}
</ResourcePageLayout>
