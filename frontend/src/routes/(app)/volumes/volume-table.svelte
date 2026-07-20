<script lang="ts">
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import RowActionsMenu from '$lib/components/arcane-table/row-actions-menu.svelte';
	import { goto } from '$app/navigation';
	import { toast } from 'svelte-sonner';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import InUseStatus from '$lib/components/arcane-table/cells/in-use-status.svelte';
	import { handleApiResultWithCallbacks } from '$lib/utils/api';
	import { tryCatch } from '$lib/utils/api';
	import { formatDateTimeShort, truncateString } from '$lib/utils/formatting';
	import type { Paginated, SearchPaginationSortRequest } from '$lib/types/shared';
	import type { VolumeSummaryDto, VolumeSizeInfo } from '$lib/types/docker';
	import type { ColumnSpec, MobileFieldVisibility, BulkAction } from '$lib/components/arcane-table';
	import { UniversalMobileCard } from '$lib/components/arcane-table/index.js';
	import { m } from '$lib/paraglide/messages';
	import { volumeService } from '$lib/services/volume-service';
	import { bytes } from '$lib/utils/formatting';
	import { inUseBadge } from '$lib/utils/mobile-card-badges';
	import { TrashIcon, InspectIcon, VolumesIcon, CalendarIcon } from '$lib/icons';
	import { Spinner } from '$lib/components/ui/spinner';
	import settingsStore from '$lib/stores/config-store';
	import { untrack } from 'svelte';
	import { SvelteMap } from 'svelte/reactivity';
	import type { ColumnVisibilityState } from '@tanstack/table-core';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { hasPermission } from '$lib/utils/auth';
	import { activityToastOptions, extractActivityId } from '$lib/utils/activity-toast';
	import { bulkConfirmAndRun } from '$lib/utils/bulk-actions';

	let {
		volumes = $bindable(),
		selectedIds = $bindable(),
		requestOptions = $bindable(),
		onRefreshData
	}: {
		volumes: Paginated<VolumeSummaryDto>;
		selectedIds: string[];
		requestOptions: SearchPaginationSortRequest;
		onRefreshData?: (options: SearchPaginationSortRequest) => Promise<void>;
	} = $props();

	let isLoading = $state({
		removing: false
	});

	const currentEnvId = $derived(environmentStore.selected?.id || '0');
	const canDeleteVolume = $derived(hasPermission('volumes:delete', currentEnvId));
	let customSettings = $state<Record<string, unknown>>({});
	let showInternal = $derived.by(() => {
		return (customSettings['showInternalVolumes'] as boolean) ?? false;
	});

	async function refreshVolumes(options: SearchPaginationSortRequest = requestOptions, refreshSizes = true) {
		if (onRefreshData) {
			await onRefreshData(options);
		} else {
			volumes = await volumeService.getVolumes(options);
		}
		if (refreshSizes && tablePreferencesReady && columnVisibility['size'] !== false) {
			void loadVolumeSizes(currentEnvId);
		}
	}

	function getCurrentLimit() {
		return requestOptions?.pagination?.limit ?? volumes?.pagination?.itemsPerPage ?? 20;
	}

	function setShowInternal(value: boolean) {
		const currentSetting = (customSettings['showInternalVolumes'] as boolean) ?? false;
		const currentRequest = requestOptions?.includeInternal ?? false;
		if (value === currentSetting && value === currentRequest) return;

		customSettings = { ...customSettings, showInternalVolumes: value };
		const nextOptions: SearchPaginationSortRequest = {
			...requestOptions,
			includeInternal: value,
			pagination: { page: 1, limit: getCurrentLimit() }
		};
		requestOptions = nextOptions;
		refreshVolumes(nextOptions);
	}

	const backupVolumeName = $derived.by(() => $settingsStore?.backupVolumeName || 'arcane-backups');
	const isBackupVolumeName = (name?: string) => (name ?? '') === backupVolumeName;
	const isBackupVolume = (item: VolumeSummaryDto) => isBackupVolumeName(item.name);

	// Lazy load volume sizes - this is a slow operation
	let sizesMap = new SvelteMap<string, VolumeSizeInfo>();
	let sizeLoadPromises = new SvelteMap<string, Promise<void>>();
	let sizesLoading = $state(false);
	let columnVisibility = $state<ColumnVisibilityState>({});
	let tablePreferencesReady = $state(false);

	function loadVolumeSizes(environmentId: string): Promise<void> {
		const existing = sizeLoadPromises.get(environmentId);
		if (existing) return existing;

		const request = (async () => {
			if (currentEnvId === environmentId) {
				sizesLoading = true;
			}
			try {
				const sizes = await volumeService.getVolumeSizes(environmentId);
				if (currentEnvId !== environmentId || columnVisibility['size'] === false) return;

				sizesMap.clear();
				for (const s of sizes) {
					sizesMap.set(s.name, s);
				}

				if (requestOptions?.sort?.column === 'size') {
					await refreshVolumes(requestOptions, false);
				}
			} catch (error) {
				console.error('Failed to load volume sizes:', error);
			} finally {
				sizeLoadPromises.delete(environmentId);
				if (currentEnvId === environmentId) {
					sizesLoading = false;
				}
			}
		})();

		sizeLoadPromises.set(environmentId, request);
		return request;
	}

	async function handleRemoveVolumeConfirm(name: string) {
		const safeName = name?.trim() || m.common_unknown();
		if (isBackupVolumeName(safeName)) return;
		openConfirmDialog({
			title: m.common_remove_title({ resource: m.resource_volume() }),
			message: m.common_remove_confirm({ resource: `${m.resource_volume()} "${safeName}"` }),
			confirm: {
				label: m.common_remove(),
				destructive: true,
				action: async () => {
					isLoading.removing = true;
					handleApiResultWithCallbacks({
						result: await tryCatch(volumeService.deleteVolume(safeName)),
						message: m.common_remove_failed({ resource: `${m.resource_volume()} "${safeName}"` }),
						setLoadingState: (value) => (isLoading.removing = value),
						onSuccess: async (data) => {
							toast.success(
								m.common_remove_success({ resource: `${m.resource_volume()} "${safeName}"` }),
								activityToastOptions(extractActivityId(data))
							);
							await refreshVolumes();
						}
					});
				}
			}
		});
	}

	function handleDeleteSelected(ids: string[]) {
		const idToName = new Map(volumes.data.map((v) => [v.id, v.name] as const));
		const idsToDelete = ids.filter((id) => !isBackupVolumeName(idToName.get(id)));

		bulkConfirmAndRun({
			ids: idsToDelete,
			title: m.volumes_remove_selected_title({ count: idsToDelete.length }),
			message: m.volumes_remove_selected_message({ count: idsToDelete.length }),
			confirmLabel: m.common_remove(),
			destructive: true,
			run: (id) => volumeService.deleteVolume(idToName.get(id)?.trim() || m.common_unknown()),
			messages: {
				success: (count) => m.common_bulk_remove_success({ count, resource: m.resource_volumes_cap() }),
				partial: (success, total, failed) =>
					m.common_bulk_remove_partial({ success, total, failed, resource: m.resource_volumes_cap() }),
				failure: () => m.common_bulk_remove_failed({ count: idsToDelete.length, resource: m.resource_volumes_cap() })
			},
			setLoading: (loading) => (isLoading.removing = loading),
			onComplete: async (result) => {
				if (result.success > 0) await refreshVolumes();
			},
			clearSelection: () => (selectedIds = [])
		});
	}

	const columns = [
		{ accessorKey: 'id', title: m.common_id(), hidden: true },
		{ accessorKey: 'name', title: m.common_name(), sortable: true, cell: NameCell },
		{ accessorKey: 'inUse', title: m.common_status(), sortable: true, cell: StatusCell },
		{ accessorKey: 'size', title: m.common_size(), sortable: true, cell: SizeCell },
		{ accessorKey: 'createdAt', title: m.common_created(), sortable: true, cell: CreatedCell },
		{ accessorKey: 'driver', title: m.common_driver(), sortable: true }
	] satisfies ColumnSpec<VolumeSummaryDto>[];

	const mobileFields = [
		{ id: 'id', label: m.common_id(), defaultVisible: false },
		{ id: 'inUse', label: m.common_status(), defaultVisible: true },
		{ id: 'size', label: m.common_size(), defaultVisible: true },
		{ id: 'createdAt', label: m.common_created(), defaultVisible: true },
		{ id: 'driver', label: m.common_driver(), defaultVisible: true }
	];

	const deletableSelectedIds = $derived.by(() => {
		const idToName = new Map(volumes.data.map((v) => [v.id, v.name] as const));
		return (selectedIds ?? []).filter((id) => !isBackupVolumeName(idToName.get(id)));
	});

	const bulkActions = $derived.by<BulkAction[]>(() => [
		{
			id: 'remove',
			label: m.common_remove_selected_count({ count: deletableSelectedIds.length }),
			action: 'remove',
			onClick: () => handleDeleteSelected(deletableSelectedIds),
			loading: isLoading.removing,
			disabled: !canDeleteVolume || isLoading.removing || deletableSelectedIds.length === 0,
			icon: TrashIcon
		}
	]);

	let mobileFieldVisibility = $state<Record<string, boolean>>({});

	let persistedSettingsApplied = false;
	$effect(() => {
		if (!tablePreferencesReady) return;

		const environmentId = currentEnvId;
		const sizeVisible = columnVisibility['size'] !== false;
		if (!sizeVisible) {
			sizesMap.clear();
			sizesLoading = false;
		} else {
			untrack(() => {
				sizesMap.clear();
				void loadVolumeSizes(environmentId);
			});
		}

		if (!persistedSettingsApplied) {
			persistedSettingsApplied = true;
			const persistedInternal = (customSettings['showInternalVolumes'] as boolean) ?? false;
			const currentInternal = requestOptions?.includeInternal ?? false;
			if (persistedInternal !== currentInternal) {
				untrack(() => setShowInternal(persistedInternal));
			}
		}
	});
</script>

{#snippet NameCell({ item }: { item: VolumeSummaryDto })}
	<a class="font-medium hover:underline" href="/volumes/{item.id}" title={item.name}>
		{truncateString(item.name, 40)}
	</a>
{/snippet}

{#snippet StatusCell({ item }: { item: VolumeSummaryDto })}
	<InUseStatus inUse={item.inUse} />
{/snippet}

{#snippet SizeCell({ item }: { item: VolumeSummaryDto })}
	{@const sizeInfo = sizesMap.get(item.name)}
	{#if sizeInfo && sizeInfo.size >= 0}
		<span class="text-sm tabular-nums">{bytes.format(sizeInfo.size)}</span>
	{:else if sizesLoading && sizesMap.size === 0}
		{#if item.size > 0}
			<span class="text-sm tabular-nums">{bytes.format(item.size)}</span>
		{:else}
			<span class="flex items-center gap-1 text-sm text-muted-foreground">
				<Spinner class="size-4" />
			</span>
		{/if}
	{:else if item.size > 0}
		<span class="text-sm tabular-nums">{bytes.format(item.size)}</span>
	{:else}
		<span class="text-sm text-muted-foreground">-</span>
	{/if}
{/snippet}

{#snippet CreatedCell({ value }: { value: unknown })}
	{formatDateTimeShort(String(value))}
{/snippet}

{#snippet VolumeMobileCardSnippet({
	item,
	mobileFieldVisibility
}: {
	item: VolumeSummaryDto;
	mobileFieldVisibility: MobileFieldVisibility;
})}
	<UniversalMobileCard
		{item}
		icon={(item) => ({
			component: VolumesIcon,
			variant: item.inUse ? 'emerald' : 'amber'
		})}
		title={(item) => item.name}
		subtitle={(item) => ((mobileFieldVisibility['id'] ?? true) ? item.id : null)}
		badges={[inUseBadge(mobileFieldVisibility['inUse'] ?? true)]}
		fields={[
			{
				label: m.common_driver(),
				getValue: (item: VolumeSummaryDto) => item.driver,
				icon: VolumesIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['driver'] ?? true
			}
		]}
		footer={(mobileFieldVisibility['createdAt'] ?? true)
			? {
					label: m.common_created(),
					getValue: (item) => formatDateTimeShort(String(item.createdAt)),
					icon: CalendarIcon
				}
			: undefined}
		rowActions={RowActions}
		onclick={() => goto(`/volumes/${item.id}`)}
	/>
{/snippet}

{#snippet RowActions({ item }: { item: VolumeSummaryDto })}
	<RowActionsMenu>
		<DropdownMenu.Item onclick={() => goto(`/volumes/${item.id}`)}>
			<InspectIcon class="size-4" />
			{m.common_inspect()}
		</DropdownMenu.Item>

		{#if canDeleteVolume}
			<DropdownMenu.Separator />

			<DropdownMenu.Item
				variant="destructive"
				onclick={() => handleRemoveVolumeConfirm(item.name)}
				disabled={item.inUse || isBackupVolume(item)}
			>
				<TrashIcon class="size-4" />
				{m.common_remove()}
			</DropdownMenu.Item>
		{/if}
	</RowActionsMenu>
{/snippet}

<ArcaneTable
	persistKey="arcane-volumes-table"
	items={volumes}
	bind:requestOptions
	bind:selectedIds
	bind:mobileFieldVisibility
	bind:customSettings
	bind:columnVisibility
	bind:preferencesReady={tablePreferencesReady}
	hiddenSortFallback={{ column: 'name', direction: 'asc' }}
	{bulkActions}
	onRefresh={async (options) => {
		requestOptions = options;
		await refreshVolumes(options);
		return volumes;
	}}
	{columns}
	{mobileFields}
	rowActions={RowActions}
	mobileCard={VolumeMobileCardSnippet}
	customViewOptions={CustomViewOptions}
/>

{#snippet CustomViewOptions()}
	<DropdownMenu.CheckboxItem checked={showInternal} onCheckedChange={(v) => setShowInternal(!!v)}>
		{`${m.common_show()} ${m.internal()} ${m.resource_volumes_cap()}`}
	</DropdownMenu.CheckboxItem>
{/snippet}
