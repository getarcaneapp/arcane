<script lang="ts">
	import { goto } from '$app/navigation';
	import { untrack } from 'svelte';

	import { BoxIcon } from '#lib/icons/index.js';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { containerService } from '#lib/services/container-service.js';
	import type { ContainerListRequestOptions } from '#lib/services/container-service.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import type { ContainerStatusCounts } from '#lib/types/docker.js';
	import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
	import { fetchAcrossEnvironments, hasShownPermission } from '#lib/utils/environments.js';
	import { confirmAndApplyAllUpdates } from '#lib/utils/update-actions.js';

	import ContainerEnvironmentSync from './components/container-environment-sync.svelte';
	import ContainerTable from './components/container-table.svelte';

	let { data } = $props();

	let requestOptions = $derived(data.containerRequestOptions);
	let selectedIds = $state<string[]>([]);
	let containers = $derived(data.containers);
	const scope = $derived(environmentStore.shownKey);
	let displayedScope = $state<string | null>(untrack(() => (data.scope === scope ? data.scope : null)));
	let isRefreshing = $state(false);
	let refreshGeneration = 0;
	let groupByProject = $state(false);
	let hasSeenEnvironmentSync = $state(false);
	let isApplyingUpdates = $state(false);
	const resourcesReady = $derived(displayedScope === scope);

	const countsFallback: ContainerStatusCounts = {
		runningContainers: 0,
		stoppedContainers: 0,
		totalContainers: 0
	};

	function buildRequestOptions(options: SearchPaginationSortRequest = requestOptions): ContainerListRequestOptions {
		return {
			...options,
			groupByProject
		};
	}

	async function refreshContainers(options: ContainerListRequestOptions = buildRequestOptions(), requestedScope = scope) {
		const generation = ++refreshGeneration;
		if (requestedScope === scope) {
			isRefreshing = true;
		}
		try {
			const next = await fetchAcrossEnvironments(environmentStore.shown, options, (id, request, config) =>
				containerService.getContainersForEnvironment(id, request, config)
			);
			if (requestedScope !== scope || generation !== refreshGeneration) {
				return containers;
			}
			containers = next;
			displayedScope = requestedScope;
			return next;
		} finally {
			if (requestedScope === scope && generation === refreshGeneration) {
				isRefreshing = false;
			}
		}
	}

	function handleEnvironmentChange() {
		if (!hasSeenEnvironmentSync) {
			hasSeenEnvironmentSync = true;
			if (data.scope === scope) {
				return;
			}
		}

		refreshGeneration += 1;
		isRefreshing = false;
		displayedScope = null;
		selectedIds = [];

		const nextOptions: SearchPaginationSortRequest = {
			...requestOptions,
			pagination: {
				page: 1,
				limit: requestOptions.pagination?.limit ?? containers.pagination?.itemsPerPage ?? 20
			}
		};
		requestOptions = nextOptions;
		if (!hasShownPermission('containers:list')) {
			return;
		}
		return refreshContainers(buildRequestOptions(nextOptions), scope);
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
				scope={displayedScope!}
				bind:containers
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
					return refreshContainers(options, requestedScope);
				}}
			/>
		{/if}
	{/snippet}
</ResourcePageLayout>
