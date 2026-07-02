<script lang="ts">
	import { VolumesIcon, VolumeUnusedIcon } from '$lib/icons';
	import { toast } from 'svelte-sonner';
	import CreateVolumeSheet from '$lib/components/sheets/create-volume-sheet.svelte';
	import type { VolumeCreateRequest, VolumeUsageCounts } from '$lib/types/docker';
	import VolumeTable from './volume-table.svelte';
	import { m } from '$lib/paraglide/messages';
	import { volumeService } from '$lib/services/volume-service';
	import { ResourceListPageState } from '$lib/utils/resource-list-page.svelte';
	import { hasPermission } from '$lib/utils/auth';
	import { queryKeys } from '$lib/query/query-keys';
	import { untrack } from 'svelte';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '$lib/layouts/index.js';
	import { createMutation, createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { activityToastOptions, extractActivityId } from '$lib/utils/activity-toast';

	let { data } = $props();
	const queryClient = useQueryClient();

	const pageState = new ResourceListPageState(
		untrack(() => data.volumes),
		untrack(() => data.volumeRequestOptions)
	);
	const countsFallback: VolumeUsageCounts = { inuse: 0, unused: 0, total: 0 };

	const volumesQuery = createQuery(() => ({
		queryKey: queryKeys.volumes.table(pageState.envId, pageState.requestOptions),
		queryFn: () => volumeService.getVolumesForEnvironment(pageState.envId, pageState.requestOptions),
		initialData: data.volumes
	}));

	const createVolumeMutation = createMutation(() => ({
		mutationKey: ['volumes', 'create', pageState.envId],
		mutationFn: (options: VolumeCreateRequest) => volumeService.createVolume(options),
		onSuccess: async (data, options) => {
			const name = options.name?.trim() || m.common_unknown();
			toast.success(
				m.common_create_success({ resource: `${m.resource_volume()} "${name}"` }),
				activityToastOptions(extractActivityId(data))
			);
			await loadVolumes();
			pageState.isCreateDialogOpen = false;
		},
		onError: (_error, options) => {
			const name = options.name?.trim() || m.common_unknown();
			toast.error(m.common_create_failed({ resource: `${m.resource_volume()} "${name}"` }));
		}
	}));

	$effect(() => {
		if (volumesQuery.data) {
			pageState.items = volumesQuery.data;
		}
	});

	async function handleCreate(options: VolumeCreateRequest) {
		await createVolumeMutation.mutateAsync(options);
	}

	async function loadVolumes(options = pageState.requestOptions) {
		pageState.requestOptions = options;
		pageState.items = await queryClient.fetchQuery({
			queryKey: queryKeys.volumes.table(pageState.envId, options),
			queryFn: () => volumeService.getVolumesForEnvironment(pageState.envId, options)
		});
	}

	async function refresh() {
		await loadVolumes();
	}

	const isRefreshing = $derived(volumesQuery.isFetching && !volumesQuery.isPending);
	const volumeUsageCounts = $derived(pageState.items.counts ?? countsFallback);

	const canCreateVolume = $derived(hasPermission('volumes:create', pageState.envId));

	const actionButtons: ActionButton[] = $derived.by(() => {
		const buttons: ActionButton[] = [];
		if (canCreateVolume) {
			buttons.push({
				id: 'create',
				action: 'create',
				label: m.common_create_button({ resource: m.resource_volume_cap() }),
				onclick: () => (pageState.isCreateDialogOpen = true),
				loading: createVolumeMutation.isPending,
				disabled: createVolumeMutation.isPending
			});
		}
		buttons.push({
			id: 'refresh',
			action: 'restart',
			label: m.common_refresh(),
			onclick: refresh,
			loading: isRefreshing,
			disabled: isRefreshing
		});
		return buttons;
	});

	const statCards: StatCardConfig[] = $derived([
		{
			title: m.volumes_stat_total(),
			value: volumeUsageCounts.total,
			icon: VolumesIcon,
			iconColor: 'text-blue-500'
		},
		{
			title: m.volumes_stat_unused(),
			value: volumeUsageCounts.unused,
			icon: VolumeUnusedIcon,
			iconColor: 'text-amber-500'
		}
	]);
</script>

<ResourcePageLayout title={m.volumes_title()} subtitle={m.volumes_subtitle()} {actionButtons} {statCards}>
	{#snippet mainContent()}
		<VolumeTable
			bind:volumes={pageState.items}
			bind:selectedIds={pageState.selectedIds}
			bind:requestOptions={pageState.requestOptions}
			onRefreshData={loadVolumes}
		/>
	{/snippet}

	{#snippet additionalContent()}
		<CreateVolumeSheet
			bind:open={pageState.isCreateDialogOpen}
			isLoading={createVolumeMutation.isPending}
			onSubmit={handleCreate}
		/>
	{/snippet}
</ResourcePageLayout>
