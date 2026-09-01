<script lang="ts">
	import { toast } from 'svelte-sonner';
	import { goto } from '$app/navigation';
	import { containerService } from '#lib/services/container-service';
	import ContainerTable from './container-table.svelte';
	import { m } from '#lib/paraglide/messages';
	import { imageService } from '#lib/services/image-service';
	import { untrack } from 'svelte';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '#lib/layouts/index';
	import { environmentStore } from '#lib/stores/environment.store.svelte';
	import { hasPermission } from '#lib/utils/auth';
	import type { ContainerStatusCounts } from '#lib/types/docker';
	import { createMutation } from '@tanstack/svelte-query';
	import { BoxIcon } from '#lib/icons';
	import { queryKeys } from '#lib/query/query-keys';
	import type { SearchPaginationSortRequest } from '#lib/types/shared';
	import type { ContainerListRequestOptions } from '#lib/services/container-service';
	import ContainerEnvironmentSync from './components/container-environment-sync.svelte';
	import { activityToastOptions, extractActivityId } from '#lib/utils/activity-toast';

	let { data } = $props();

	let requestOptions = $state(untrack(() => data.containerRequestOptions));
	let selectedIds = $state<string[]>([]);
	let containers = $state(untrack(() => data.containers));
	const envId = $derived(environmentStore.selected?.id || '0');
	let displayedEnvId = $state<string | null>(untrack(() => (data.envId === envId ? data.envId : null)));
	let isRefreshing = $state(false);
	let refreshGeneration = 0;
	let groupByProject = $state(false);
	let hasSeenEnvironmentSync = $state(false);
	const resourcesReady = $derived(displayedEnvId === envId);

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

	async function refreshContainers(options: ContainerListRequestOptions = buildRequestOptions(), requestedEnvId = envId) {
		const generation = ++refreshGeneration;
		if (requestedEnvId === envId) {
			isRefreshing = true;
		}
		try {
			const next = await containerService.getContainersForEnvironment(requestedEnvId, options);
			if (requestedEnvId !== envId || generation !== refreshGeneration) {
				return containers;
			}
			containers = next;
			displayedEnvId = requestedEnvId;
			return next;
		} finally {
			if (requestedEnvId === envId && generation === refreshGeneration) {
				isRefreshing = false;
			}
		}
	}

	const checkUpdatesMutation = createMutation(() => ({
		mutationKey: queryKeys.containers.checkUpdates(envId),
		mutationFn: async () => {
			const requestedEnvId = envId;
			const result = await imageService.runAutoUpdate(undefined, requestedEnvId);
			return { requestedEnvId, result };
		},
		onSuccess: async ({ requestedEnvId, result }) => {
			toast.success(m.containers_check_updates_success(), activityToastOptions(extractActivityId(result)));
			if (requestedEnvId === envId) {
				await refreshContainers(buildRequestOptions(), requestedEnvId);
			}
		},
		onError: () => {
			toast.error(m.containers_check_updates_failed());
		}
	}));

	function handleEnvironmentChange() {
		if (!hasSeenEnvironmentSync) {
			hasSeenEnvironmentSync = true;
			if (data.envId === envId) {
				return;
			}
		}

		refreshGeneration += 1;
		isRefreshing = false;
		displayedEnvId = null;
		selectedIds = [];

		const nextOptions: SearchPaginationSortRequest = {
			...requestOptions,
			pagination: {
				page: 1,
				limit: requestOptions.pagination?.limit ?? containers.pagination?.itemsPerPage ?? 20
			}
		};
		requestOptions = nextOptions;
		if (!hasPermission('containers:list', envId)) {
			return;
		}
		return refreshContainers(buildRequestOptions(nextOptions), envId);
	}

	async function handleCheckForUpdates() {
		await checkUpdatesMutation.mutateAsync();
	}

	async function refresh() {
		await refreshContainers();
	}

	const containerStatusCounts = $derived(resourcesReady ? (containers.counts ?? countsFallback) : countsFallback);

	const canCreateContainers = $derived(hasPermission('containers:create', envId));
	const canAutoUpdate = $derived(hasPermission('containers:autoupdate', envId));

	const actionButtons: ActionButton[] = $derived(
		[
			canCreateContainers
				? {
						id: 'create',
						action: 'create',
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
						onclick: handleCheckForUpdates,
						loading: checkUpdatesMutation.isPending,
						disabled: !resourcesReady || checkUpdatesMutation.isPending
					}
				: null,
			{
				id: 'refresh',
				action: 'restart',
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
			iconColor: 'text-blue-500'
		},
		{
			title: m.common_running(),
			value: containerStatusCounts.runningContainers,
			icon: BoxIcon,
			iconColor: 'text-green-500'
		},
		{
			title: m.common_stopped(),
			value: containerStatusCounts.stoppedContainers,
			icon: BoxIcon,
			iconColor: 'text-red-500'
		}
	]);
</script>

{#key envId}
	<ContainerEnvironmentSync onActivate={handleEnvironmentChange} />
{/key}

<ResourcePageLayout title={m.containers()} subtitle={m.containers_subtitle()} {actionButtons} {statCards}>
	{#snippet mainContent()}
		{#if resourcesReady}
			<ContainerTable
				environmentId={displayedEnvId!}
				bind:containers
				bind:selectedIds
				bind:requestOptions
				bind:groupByProject
				onRefreshData={async (options) => {
					const requestedEnvId = envId;
					requestOptions = {
						search: options.search,
						pagination: options.pagination,
						sort: options.sort,
						filters: options.filters,
						includeInternal: options.includeInternal
					};
					return refreshContainers(options, requestedEnvId);
				}}
			/>
		{/if}
	{/snippet}
</ResourcePageLayout>
