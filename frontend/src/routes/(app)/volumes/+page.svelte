<script lang="ts">
	import { VolumesIcon, VolumeUnusedIcon, VolumeUsedIcon } from '$lib/icons';
	import { toast } from 'svelte-sonner';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { tryCatch } from '$lib/utils/try-catch';
	import CreateVolumeSheet from '$lib/components/sheets/create-volume-sheet.svelte';
	import type { VolumeCreateRequest } from '$lib/types/volume.type';
	import VolumeTable from './volume-table.svelte';
	import { m } from '$lib/paraglide/messages';
	import { volumeService } from '$lib/services/volume-service';
	import { untrack } from 'svelte';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '$lib/layouts/index.js';
	import { useEnvironmentRefresh } from '$lib/hooks/use-environment-refresh.svelte';
	import { parallelRefresh } from '$lib/utils/refresh.util';

	let { data } = $props();

	let volumes = $state(untrack(() => data.volumes));
	let volumeUsageCounts = $state(untrack(() => data.volumeUsageCounts));
	let requestOptions = $state(untrack(() => data.volumeRequestOptions));
	let selectedIds = $state<string[]>([]);
	let isCreateDialogOpen = $state(false);
	let isLoading = $state({ creating: false, refresh: false });

	async function refresh() {
		await parallelRefresh(
			{
				volumes: {
					fetch: () => volumeService.getVolumes(requestOptions),
					onSuccess: (data) => {
						volumes = data;
						// Extract counts from the response - they're included in the list endpoint
						volumeUsageCounts = data.counts ?? { inuse: 0, unused: 0, total: 0 };
					},
					errorMessage: m.common_refresh_failed({ resource: m.volumes_title() })
				}
			},
			(v) => (isLoading.refresh = v)
		);
	}

	useEnvironmentRefresh(refresh);

	async function handleCreate(options: VolumeCreateRequest) {
		isLoading.creating = true;
		const name = options.name?.trim() || m.common_unknown();
		handleApiResultWithCallbacks({
			result: await tryCatch(volumeService.createVolume(options)),
			message: m.common_create_failed({ resource: `${m.resource_volume()} "${name}"` }),
			setLoadingState: (v) => (isLoading.creating = v),
			onSuccess: async () => {
				toast.success(m.common_create_success({ resource: `${m.resource_volume()} "${name}"` }));
				volumes = await volumeService.getVolumes(requestOptions);
				isCreateDialogOpen = false;
			}
		});
	}

	const actionButtons: ActionButton[] = $derived([
		{
			id: 'create',
			action: 'create',
			label: m.common_create_button({ resource: m.resource_volume_cap() }),
			onclick: () => (isCreateDialogOpen = true),
			loading: isLoading.creating,
			disabled: isLoading.creating
		},
		{
			id: 'refresh',
			action: 'restart',
			label: m.common_refresh(),
			onclick: refresh,
			loading: isLoading.refresh,
			disabled: isLoading.refresh
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
			iconColor: 'text-red-500'
		}
	]);
</script>

<ResourcePageLayout title={m.volumes_title()} subtitle={m.volumes_subtitle()} {actionButtons} {statCards}>
	{#snippet mainContent()}
		<VolumeTable bind:volumes bind:selectedIds bind:requestOptions />
	{/snippet}

	{#snippet additionalContent()}
		<CreateVolumeSheet bind:open={isCreateDialogOpen} isLoading={isLoading.creating} onSubmit={handleCreate} />
	{/snippet}
</ResourcePageLayout>
