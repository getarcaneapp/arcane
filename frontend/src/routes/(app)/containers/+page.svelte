<script lang="ts">
	import CreateContainerDialog from '$lib/components/dialogs/create-container-dialog.svelte';
	import { toast } from 'svelte-sonner';
	import { containerService } from '$lib/services/container-service';
	import ContainerTable from './container-table.svelte';
	import { m } from '$lib/paraglide/messages';
	import { imageService } from '$lib/services/image-service';
	import { untrack } from 'svelte';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '$lib/layouts/index';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import type { ContainerCreateRequest, ContainerStatusCounts } from '$lib/types/container.type';
	import { createMutation } from '@tanstack/svelte-query';
	import { BoxIcon } from '$lib/icons';
	import { queryKeys } from '$lib/query/query-keys';
	import type { SearchPaginationSortRequest } from '$lib/types/pagination.type';
	import type { ContainerListRequestOptions } from '$lib/services/container-service';
	import ContainerEnvironmentSync from './components/container-environment-sync.svelte';

	let { data } = $props();

	let requestOptions = $state(untrack(() => data.containerRequestOptions));
	let selectedIds = $state<string[]>([]);
	let isCreateDialogOpen = $state(false);
	let containers = $state(untrack(() => data.containers));
	let isRefreshing = $state(false);
	const envId = $derived(environmentStore.selected?.id || '0');
	let groupByProject = $state(false);
	let hasSeenEnvironmentSync = $state(false);

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

	async function refreshContainers(options: ContainerListRequestOptions = buildRequestOptions()) {
		isRefreshing = true;
		try {
			const next = await containerService.getContainersForEnvironment(envId, options);
			containers = next;
			return next;
		} finally {
			isRefreshing = false;
		}
	}

	const checkUpdatesMutation = createMutation(() => ({
		mutationKey: queryKeys.containers.checkUpdates(envId),
		mutationFn: () => imageService.runAutoUpdate(),
		onSuccess: async () => {
			toast.success(m.containers_check_updates_success());
			await refreshContainers();
		},
		onError: () => {
			toast.error(m.containers_check_updates_failed());
		}
	}));

	const createContainerMutation = createMutation(() => ({
		mutationKey: queryKeys.containers.create(envId),
		mutationFn: (options: ContainerCreateRequest) => containerService.createContainer(options),
		onSuccess: async () => {
			toast.success(m.common_create_success({ resource: m.resource_container() }));
			await refreshContainers();
			isCreateDialogOpen = false;
		},
		onError: () => {
			toast.error(m.containers_create_failed());
		}
	}));

	function handleEnvironmentChange() {
		if (!hasSeenEnvironmentSync) {
			hasSeenEnvironmentSync = true;
			return;
		}

		const nextOptions: SearchPaginationSortRequest = {
			...requestOptions,
			pagination: {
				page: 1,
				limit: requestOptions.pagination?.limit ?? containers.pagination?.itemsPerPage ?? 20
			}
		};
		requestOptions = nextOptions;
		return refreshContainers(buildRequestOptions(nextOptions));
	}

	async function handleCheckForUpdates() {
		await checkUpdatesMutation.mutateAsync();
	}

	async function refresh() {
		await refreshContainers();
	}

	const containerStatusCounts = $derived(containers.counts ?? countsFallback);

	const actionButtons: ActionButton[] = $derived([
		{
			id: 'create',
			action: 'create',
			label: m.common_create_button({ resource: m.resource_container_cap() }),
			onclick: () => (isCreateDialogOpen = true),
			loading: createContainerMutation.isPending,
			disabled: createContainerMutation.isPending
		},
		{
			id: 'check-updates',
			action: 'update',
			label: m.containers_check_updates(),
			onclick: handleCheckForUpdates,
			loading: checkUpdatesMutation.isPending,
			disabled: checkUpdatesMutation.isPending
		},
		{
			id: 'refresh',
			action: 'restart',
			label: m.common_refresh(),
			onclick: refresh,
			loading: isRefreshing,
			disabled: isRefreshing
		}
	]);

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

<ResourcePageLayout title={m.containers_title()} subtitle={m.containers_subtitle()} {actionButtons} {statCards}>
	{#snippet mainContent()}
		<ContainerTable
			bind:containers
			bind:selectedIds
			bind:requestOptions
			bind:groupByProject
			onRefreshData={async (options) => {
				requestOptions = {
					search: options.search,
					pagination: options.pagination,
					sort: options.sort,
					filters: options.filters,
					includeInternal: options.includeInternal
				};
				return refreshContainers(options);
			}}
		/>
	{/snippet}

	{#snippet additionalContent()}
		<CreateContainerDialog
			bind:open={isCreateDialogOpen}
			isLoading={createContainerMutation.isPending}
			onSubmit={(options) => createContainerMutation.mutate(options)}
		/>
	{/snippet}
</ResourcePageLayout>
