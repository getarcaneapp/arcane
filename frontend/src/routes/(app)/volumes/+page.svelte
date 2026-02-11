<script lang="ts">
	import { VolumesIcon, VolumeUnusedIcon } from '$lib/icons';
	import { toast } from 'svelte-sonner';
	import CreateVolumeSheet from '$lib/components/sheets/create-volume-sheet.svelte';
	import type { VolumeCreateRequest, VolumeUsageCounts } from '$lib/types/volume.type';
	import VolumeTable from './volume-table.svelte';
	import { m } from '$lib/paraglide/messages';
	import { volumeService } from '$lib/services/volume-service';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { queryKeys } from '$lib/query/query-keys';
	import { untrack } from 'svelte';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '$lib/layouts/index.js';
	import { createMutation, createQuery } from '@tanstack/svelte-query';

	let { data } = $props();

	let volumes = $state(untrack(() => data.volumes));
	let requestOptions = $state(untrack(() => data.volumeRequestOptions));
	let selectedIds = $state<string[]>([]);
	let isCreateDialogOpen = $state(false);
	const envId = $derived(environmentStore.selected?.id || '0');
	const countsFallback: VolumeUsageCounts = { inuse: 0, unused: 0, total: 0 };

	const volumesQuery = createQuery(() => ({
		queryKey: queryKeys.volumes.table(envId, requestOptions),
		queryFn: () => volumeService.getVolumesForEnvironment(envId, requestOptions),
		initialData: data.volumes
	}));

	const createVolumeMutation = createMutation(() => ({
		mutationKey: ['volumes', 'create', envId],
		mutationFn: (options: VolumeCreateRequest) => volumeService.createVolume(options),
		onSuccess: async (_data, options) => {
			const name = options.name?.trim() || m.common_unknown();
			toast.success(m.common_create_success({ resource: `${m.resource_volume()} "${name}"` }));
			await volumesQuery.refetch();
			isCreateDialogOpen = false;
		},
		onError: (_error, options) => {
			const name = options.name?.trim() || m.common_unknown();
			toast.error(m.common_create_failed({ resource: `${m.resource_volume()} "${name}"` }));
		}
	}));

	$effect(() => {
		if (volumesQuery.data) {
			volumes = volumesQuery.data;
		}
	});

	async function handleCreate(options: VolumeCreateRequest) {
		await createVolumeMutation.mutateAsync(options);
	}

	async function refresh() {
		await volumesQuery.refetch();
	}

	const isRefreshing = $derived(volumesQuery.isFetching && !volumesQuery.isPending);
	const volumeUsageCounts = $derived(volumes.counts ?? countsFallback);

	const actionButtons: ActionButton[] = $derived([
		{
			id: 'create',
			action: 'create',
			label: m.common_create_button({ resource: m.resource_volume_cap() }),
			onclick: () => (isCreateDialogOpen = true),
			loading: createVolumeMutation.isPending,
			disabled: createVolumeMutation.isPending
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
			bind:volumes
			bind:selectedIds
			bind:requestOptions
			onRefreshData={async (options) => {
				requestOptions = options;
				await volumesQuery.refetch();
			}}
		/>
	{/snippet}

	{#snippet additionalContent()}
		<CreateVolumeSheet bind:open={isCreateDialogOpen} isLoading={createVolumeMutation.isPending} onSubmit={handleCreate} />
	{/snippet}
</ResourcePageLayout>
