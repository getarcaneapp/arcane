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
	import { createMutation, createQuery } from '@tanstack/svelte-query';
	import { BoxIcon } from '$lib/icons';
	import { queryKeys } from '$lib/query/query-keys';

	let { data } = $props();

	let requestOptions = $state(untrack(() => data.containerRequestOptions));
	let selectedIds = $state<string[]>([]);
	let isCreateDialogOpen = $state(false);
	let containers = $state(untrack(() => data.containers));
	const envId = $derived(environmentStore.selected?.id || '0');

	const countsFallback: ContainerStatusCounts = {
		runningContainers: 0,
		stoppedContainers: 0,
		totalContainers: 0
	};

	const containersQuery = createQuery(() => ({
		queryKey: queryKeys.containers.list(envId, requestOptions),
		queryFn: () => containerService.getContainersForEnvironment(envId, requestOptions),
		initialData: data.containers
	}));

	const checkUpdatesMutation = createMutation(() => ({
		mutationKey: queryKeys.containers.checkUpdates(envId),
		mutationFn: () => imageService.runAutoUpdate(),
		onSuccess: async () => {
			toast.success(m.containers_check_updates_success());
			await containersQuery.refetch();
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
			await containersQuery.refetch();
			isCreateDialogOpen = false;
		},
		onError: () => {
			toast.error(m.containers_create_failed());
		}
	}));

	$effect(() => {
		if (containersQuery.data) {
			containers = containersQuery.data;
		}
	});

	async function handleCheckForUpdates() {
		await checkUpdatesMutation.mutateAsync();
	}

	async function refresh() {
		await containersQuery.refetch();
	}

	const isRefreshing = $derived(containersQuery.isFetching && !containersQuery.isPending);
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

<ResourcePageLayout title={m.containers_title()} subtitle={m.containers_subtitle()} {actionButtons} {statCards}>
	{#snippet mainContent()}
		<ContainerTable
			bind:containers
			bind:selectedIds
			bind:requestOptions
			onRefreshData={async (options) => {
				requestOptions = options;
				await containersQuery.refetch();
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
