<script lang="ts">
	import HardDriveIcon from '@lucide/svelte/icons/hard-drive';
	import ArchiveRestoreIcon from '@lucide/svelte/icons/archive-restore';
	import ArchiveXIcon from '@lucide/svelte/icons/archive-x';
	import { toast } from 'svelte-sonner';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { tryCatch } from '$lib/utils/try-catch';
	import CreateVolumeSheet from '$lib/components/sheets/create-volume-sheet.svelte';
	import type { VolumeCreateOptions } from 'dockerode';
	import VolumeTable from './volume-table.svelte';
	import { m } from '$lib/paraglide/messages';
	import { volumeService } from '$lib/services/volume-service';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { untrack } from 'svelte';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '$lib/layouts/index.js';

	let { data } = $props();

	let volumes = $state(untrack(() => data.volumes));
	let volumeUsageCounts = $state(untrack(() => data.volumeUsageCounts));
	let requestOptions = $state(untrack(() => data.volumeRequestOptions));

	let selectedIds = $state<string[]>([]);
	let isCreateDialogOpen = $state(false);

	let isLoading = $state({
		creating: false,
		refresh: false,
		removingSelected: false
	});

	const totalVolumes = $derived(volumeUsageCounts.totalVolumes);
	const usedVolumes = $derived(volumeUsageCounts.volumesInuse);
	const unusedVolumes = $derived(volumeUsageCounts.volumesUnused);

	async function handleCreateVolumeSubmit(options: VolumeCreateOptions) {
		isLoading.creating = true;
		const name = options.Name?.trim() || m.common_unknown();
		handleApiResultWithCallbacks({
			result: await tryCatch(volumeService.createVolume(options)),
			message: m.common_create_failed({ resource: `${m.resource_volume()} "${name}"` }),
			setLoadingState: (value) => (isLoading.creating = value),
			onSuccess: async () => {
				toast.success(m.common_create_success({ resource: `${m.resource_volume()} "${name}"` }));
				volumes = await volumeService.getVolumes(requestOptions);
				isCreateDialogOpen = false;
			}
		});
	}

	async function refreshVolumes() {
		isLoading.refresh = true;
		let refreshingVolumeList = true;
		let refreshingVolumeCounts = true;
		handleApiResultWithCallbacks({
			result: await tryCatch(volumeService.getVolumes(requestOptions)),
			message: m.common_refresh_failed({ resource: m.volumes_title() }),
			setLoadingState: (value) => {
				refreshingVolumeList = value;
				isLoading.refresh = refreshingVolumeCounts || refreshingVolumeList;
			},
			async onSuccess(newVolumes) {
				volumes = newVolumes;
			}
		});
		handleApiResultWithCallbacks({
			result: await tryCatch(volumeService.getVolumeUsageCounts()),
			message: m.common_refresh_failed({ resource: m.volumes_title() }),
			setLoadingState: (value) => {
				refreshingVolumeCounts = value;
				isLoading.refresh = refreshingVolumeCounts || refreshingVolumeList;
			},
			async onSuccess(newVolumeCounts) {
				volumeUsageCounts = newVolumeCounts;
			}
		});
	}

	let lastEnvId: string | null = null;
	$effect(() => {
		const env = environmentStore.selected;
		if (!env) return;
		if (lastEnvId === null) {
			lastEnvId = env.id;
			return;
		}
		if (env.id !== lastEnvId) {
			lastEnvId = env.id;
			refreshVolumes();
		}
	});

	const actionButtons: ActionButton[] = $derived.by(() => [
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
			onclick: refreshVolumes,
			loading: isLoading.refresh,
			disabled: isLoading.refresh
		}
	]);

	const statCards: StatCardConfig[] = $derived([
		{
			title: m.volumes_stat_total(),
			value: totalVolumes,
			icon: HardDriveIcon,
			iconColor: 'text-blue-500',
			class: 'border-l-4 border-l-blue-500'
		},
		{
			title: m.volumes_stat_used(),
			value: usedVolumes,
			icon: ArchiveRestoreIcon,
			iconColor: 'text-green-500',
			class: 'border-l-4 border-l-green-500'
		},
		{
			title: m.volumes_stat_unused(),
			value: unusedVolumes,
			icon: ArchiveXIcon,
			iconColor: 'text-red-500',
			class: 'border-l-4 border-l-red-500'
		}
	]);
</script>

<ResourcePageLayout title={m.volumes_title()} subtitle={m.volumes_subtitle()} {actionButtons} {statCards} statCardsColumns={3}>
	{#snippet mainContent()}
		<VolumeTable bind:volumes bind:selectedIds bind:requestOptions />
	{/snippet}

	{#snippet additionalContent()}
		<CreateVolumeSheet bind:open={isCreateDialogOpen} isLoading={isLoading.creating} onSubmit={handleCreateVolumeSubmit} />
	{/snippet}
</ResourcePageLayout>
