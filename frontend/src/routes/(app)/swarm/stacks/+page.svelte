<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { createQuery } from '@tanstack/svelte-query';
	import { untrack } from 'svelte';

	import { swarmEnvironmentsQueryOptions } from '#lib/components/swarm/swarm-environments.js';
	import { LayersIcon } from '#lib/icons/index.js';
	import { ResourcePageLayout, type StatCardConfig } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { swarmStackListQuery, swarmStacksScope } from '#lib/query/swarm.js';
	import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { createRefreshActionButtons } from '#lib/utils/resource-actions.js';
	import { ResourceListPageState } from '#lib/utils/resource-list-page.svelte.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import SwarmStacksTable from './components/stacks-table.svelte';

	let { data } = $props();

	const pageState = new ResourceListPageState<SearchPaginationSortRequest>(untrack(() => data.requestOptions));
	const swarmQuery = createQuery(() => swarmEnvironmentsQueryOptions('swarm:read'));
	const swarm = $derived(swarmQuery.data ?? data.swarm);

	const stacksScope = $derived(swarmStacksScope(swarm));
	const stacksQuery = createQuery(() => {
		const scope = stacksScope;
		return {
			...swarmStackListQuery(swarm, pageState.requestOptions),
			placeholderData: (previous, query) => (query?.queryKey[1] === scope ? previous : undefined),
			select: (value) => ({ envId: scope, value })
		};
	});
	const stacks = $derived(stacksQuery.data?.value ?? data.stacks);
	const isRefreshing = $derived(stacksQuery.isFetching && !stacksQuery.isPending);

	async function refresh() {
		await swarmQuery.refetch();
		await handleApiResultWithCallbacks({
			result: await tryCatch(stacksQuery.refetch({ throwOnError: true })),
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
				onclick: () => goto(resolve('/(app)/swarm/stacks/new'))
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
		<SwarmStacksTable
			{stacks}
			scope={stacksScope}
			bind:requestOptions={pageState.requestOptions}
			onRetry={() => stacksQuery.refetch()}
		/>
	{/snippet}
</ResourcePageLayout>
