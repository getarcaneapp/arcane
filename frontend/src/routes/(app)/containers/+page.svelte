<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { untrack } from 'svelte';

	import { BoxIcon } from '#lib/icons/index.js';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { containerListQuery } from '#lib/query/containers.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import type { ContainerStatusCounts } from '#lib/types/docker.js';
	import { pinViewAction } from '#lib/utils/dashboard-layout.js';
	import { ENVIRONMENT_FILTER, environmentFilterIds, hasShownPermission, readableEnvironments } from '#lib/utils/environments.js';
	import { ResourceListPageState } from '#lib/utils/resource-list-page.svelte.js';
	import { confirmAndApplyAllUpdates } from '#lib/utils/update-actions.js';

	import ContainerEnvironmentSync from './components/container-environment-sync.svelte';
	import ContainerTable from './components/container-table.svelte';

	let { data } = $props();
	const queryClient = useQueryClient();

	const pageState = new ResourceListPageState(untrack(() => data.containerRequestOptions));
	let groupByProject = $state(false);
	let hasSeenEnvironmentSync = $state(false);
	let isApplyingUpdates = $state(false);

	const countsFallback: ContainerStatusCounts = {
		runningContainers: 0,
		stoppedContainers: 0,
		totalContainers: 0,
		unhealthyContainers: 0,
		updatesAvailable: 0
	};

	const containersQuery = createQuery(() => {
		const scope = pageState.scope;
		const options = { ...pageState.requestOptions, groupByProject };
		return {
			...containerListQuery(scope, readableEnvironments('containers:list'), options),
			placeholderData: (previous, query) => {
				if (query?.queryKey[1] !== scope || !previous) return undefined;
				const picked = environmentFilterIds(options.filters?.[ENVIRONMENT_FILTER]);
				if (picked.length === 0 || previous.data.every((row) => picked.includes(row.environmentId))) return previous;
				return { ...previous, data: [], groups: [] };
			},
			select: (value) => ({ envId: scope, value })
		};
	});

	const containers = $derived(containersQuery.data?.value ?? data.containers);
	const resourcesReady = $derived(containersQuery.data?.envId === pageState.scope);
	const isRefreshing = $derived(containersQuery.isFetching && !containersQuery.isPending);

	function handleEnvironmentChange() {
		if (!hasSeenEnvironmentSync) {
			hasSeenEnvironmentSync = true;
			return;
		}

		pageState.selectedIds = [];
		pageState.requestOptions = {
			...pageState.requestOptions,
			pagination: {
				page: 1,
				limit: pageState.requestOptions.pagination?.limit ?? containers.pagination?.itemsPerPage ?? 20
			}
		};
	}

	const containerStatusCounts = $derived(resourcesReady ? (containers.counts ?? countsFallback) : countsFallback);

	const canCreateContainers = $derived(hasShownPermission('containers:create'));
	const canAutoUpdate = $derived(hasShownPermission('containers:autoupdate'));

	const actionButtons: ActionButton[] = $derived(
		[
			canCreateContainers
				? {
						id: 'create',
						action: 'create',
						placement: 'primary',
						label: m.common_create_button({ resource: m.container() }),
						onclick: () => goto(resolve('/(app)/containers/new')),
						disabled: !resourcesReady
					}
				: null,
			canAutoUpdate
				? {
						id: 'check-updates',
						action: 'update',
						label: m.containers_check_updates(),
						onclick: () =>
							confirmAndApplyAllUpdates({
								setLoading: (loading) => (isApplyingUpdates = loading),
								onRefresh: () => queryClient.invalidateQueries({ queryKey: queryKeys.containers.listPrefix(pageState.scope) })
							}),
						loading: isApplyingUpdates,
						disabled: !resourcesReady || isApplyingUpdates
					}
				: null,
			pinViewAction('containers', pageState.requestOptions, readableEnvironments('containers:list')),
			{
				id: 'refresh',
				action: 'refresh',
				placement: 'secondary',
				iconOnly: true,
				label: m.common_refresh(),
				onclick: () => containersQuery.refetch(),
				loading: isRefreshing,
				disabled: isRefreshing
			}
		].filter((b) => b !== null) as ActionButton[]
	);

	const statCards: StatCardConfig[] = $derived([
		{
			title: m.common_total(),
			value: containerStatusCounts.totalContainers,
			icon: BoxIcon,
			iconColor: 'text-info'
		},
		{
			title: m.common_running(),
			value: containerStatusCounts.runningContainers,
			icon: BoxIcon,
			iconColor: 'text-success'
		},
		{
			title: m.common_stopped(),
			value: containerStatusCounts.stoppedContainers,
			icon: BoxIcon,
			iconColor: 'text-destructive'
		}
	]);
</script>

{#key pageState.scope}
	<ContainerEnvironmentSync onActivate={handleEnvironmentChange} />
{/key}

<ResourcePageLayout title={m.containers()} subtitle={m.containers_subtitle()} {actionButtons} {statCards}>
	{#snippet mainContent()}
		{#if resourcesReady}
			<ContainerTable
				scope={pageState.scope}
				{containers}
				bind:selectedIds={pageState.selectedIds}
				bind:requestOptions={pageState.requestOptions}
				bind:groupByProject
				onRetry={() => containersQuery.refetch()}
			/>
		{/if}
	{/snippet}
</ResourcePageLayout>
