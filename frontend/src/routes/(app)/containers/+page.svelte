<script lang="ts">
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';

	import { BoxIcon } from '#lib/icons/index.js';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { containerService } from '#lib/services/container-service.js';
	import type { ContainerListRequestOptions } from '#lib/services/container-service.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import type { ContainerStatusCounts } from '#lib/types/docker.js';
	import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
	import { goto } from '#lib/utils/base-path.js';
	import {
		ENVIRONMENT_FILTER,
		environmentFilterIds,
		fetchAcrossEnvironments,
		hasShownPermission,
		readableEnvironments
	} from '#lib/utils/environments.js';
	import { confirmAndApplyAllUpdates } from '#lib/utils/update-actions.js';

	import ContainerEnvironmentSync from './components/container-environment-sync.svelte';
	import ContainerTable from './components/container-table.svelte';

	let { data } = $props();
	const queryClient = useQueryClient();

	let requestOptions = $derived(data.containerRequestOptions);
	let selectedIds = $state<string[]>([]);
	const scope = $derived(environmentStore.shownKey);
	let groupByProject = $state(false);
	let hasSeenEnvironmentSync = $state(false);
	let isApplyingUpdates = $state(false);

	const countsFallback: ContainerStatusCounts = {
		runningContainers: 0,
		stoppedContainers: 0,
		totalContainers: 0,
		updatesAvailable: 0
	};

	function buildRequestOptions(options: SearchPaginationSortRequest = requestOptions): ContainerListRequestOptions {
		return {
			...options,
			groupByProject
		};
	}

	const containersQuery = createQuery(() => {
		const queryScope = scope;
		const environments = readableEnvironments('containers:list');
		const options = buildRequestOptions();
		return {
			queryKey: queryKeys.containers.list(queryScope, options),
			queryFn: () =>
				fetchAcrossEnvironments(environments, options, (id, request, config) =>
					containerService.getContainersForEnvironment(id, request, config)
				),
			placeholderData: (previous, query) => {
				if (query?.queryKey[1] !== queryScope || !previous) return undefined;
				const picked = environmentFilterIds(options.filters?.[ENVIRONMENT_FILTER]);
				if (picked.length === 0 || previous.data.every((row) => picked.includes(row.environmentId))) return previous;
				return { ...previous, data: [], groups: [] };
			},
			select: (value) => ({ envId: queryScope, value })
		};
	});

	const containers = $derived(containersQuery.data?.value ?? data.containers);
	const resourcesReady = $derived(containersQuery.data?.envId === scope);
	const isRefreshing = $derived(containersQuery.isFetching && !containersQuery.isPending);

	async function refreshContainers(options: ContainerListRequestOptions = buildRequestOptions(), requestedScope = scope) {
		const environments = readableEnvironments('containers:list');
		return queryClient.fetchQuery({
			queryKey: queryKeys.containers.list(requestedScope, options),
			queryFn: () =>
				fetchAcrossEnvironments(environments, options, (id, request, config) =>
					containerService.getContainersForEnvironment(id, request, config)
				),
			staleTime: 0
		});
	}

	function handleEnvironmentChange() {
		if (!hasSeenEnvironmentSync) {
			hasSeenEnvironmentSync = true;
			return;
		}

		selectedIds = [];
		requestOptions = {
			...requestOptions,
			pagination: {
				page: 1,
				limit: requestOptions.pagination?.limit ?? containers.pagination?.itemsPerPage ?? 20
			}
		};
	}

	async function refresh() {
		await refreshContainers();
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
						onclick: () => goto('/containers/new'),
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
								onRefresh: () => refreshContainers()
							}),
						loading: isApplyingUpdates,
						disabled: !resourcesReady || isApplyingUpdates
					}
				: null,
			{
				id: 'refresh',
				action: 'refresh',
				placement: 'secondary',
				iconOnly: true,
				label: m.common_refresh(),
				onclick: refresh,
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

{#key scope}
	<ContainerEnvironmentSync onActivate={handleEnvironmentChange} />
{/key}

<ResourcePageLayout title={m.containers()} subtitle={m.containers_subtitle()} {actionButtons} {statCards}>
	{#snippet mainContent()}
		{#if resourcesReady}
			<ContainerTable
				{scope}
				bind:containers={
					() => containers, (value) => queryClient.setQueryData(queryKeys.containers.list(scope, buildRequestOptions()), value)
				}
				bind:selectedIds
				bind:requestOptions
				bind:groupByProject
				onRefreshData={async (options) => {
					const requestedScope = scope;
					requestOptions = {
						search: options.search,
						pagination: options.pagination,
						sort: options.sort,
						filters: options.filters,
						includeInternal: options.includeInternal,
						includeHidden: options.includeHidden
					};
					return refreshContainers(buildRequestOptions(), requestedScope);
				}}
			/>
		{/if}
	{/snippet}
</ResourcePageLayout>
