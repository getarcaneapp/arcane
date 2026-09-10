<script lang="ts">
	import ArcaneTable from '#lib/components/arcane-table/arcane-table.svelte';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import RowActionsMenu from '#lib/components/arcane-table/row-actions-menu.svelte';
	import { goto } from '$app/navigation';
	import { toast } from 'svelte-sonner';
	import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
	import InUseStatus from '#lib/components/arcane-table/cells/in-use-status.svelte';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { tryCatch } from '#lib/utils/try-catch.js';
	import { formatDateTimeShort, truncateString } from '#lib/utils/formatting.js';
	import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared.js';
	import type { VolumeSummaryDto, VolumeSizeInfo } from '#lib/types/docker.js';
	import type { ColumnSpec, MobileFieldVisibility, BulkAction } from '#lib/components/arcane-table/index.js';
	import { UniversalMobileCard } from '#lib/components/arcane-table/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { volumeService } from '#lib/services/volume-service.js';
	import { bytes } from '#lib/utils/formatting.js';
	import { inUseBadge } from '#lib/utils/mobile-card-badges.js';
	import { TrashIcon, InspectIcon, VolumesIcon, CalendarIcon, EditIcon } from '#lib/icons/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import settingsStore from '#lib/stores/config-store.js';
	import { onMount } from 'svelte';
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { queryKeys } from '#lib/query/query-keys.js';
	import type { ColumnVisibilityState } from '@tanstack/table-core';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import userStore from '#lib/stores/user-store.js';
	import { activityToastOptions, extractActivityId } from '#lib/utils/activity-toast.js';
	import { bulkConfirmAndRun } from '#lib/utils/bulk-actions.js';
	import RenameVolumeDialog from './components/rename-volume-dialog.svelte';

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
		removing: false,
		renaming: false
	});
	let volumeToRename = $state<VolumeSummaryDto | null>(null);
	let renameDialogOpen = $state(false);
	let renameSession = $state(0);
	let renameEnvironmentId = $state('0');

	const currentEnvId = $derived(environmentStore.selected?.id || '0');
	// Track the user store: hasPermission reads it non-reactively, so without
	// this the derived would cache a pre-hydration false forever.
	const canDeleteVolume = $derived.by(() => {
		$userStore;
		return hasPermission('volumes:delete', currentEnvId);
	});
	const canRenameVolume = $derived.by(() => {
		$userStore;
		return hasPermission('volumes:rename', currentEnvId);
	});
	let tablePreferences = $state<{ persistCustomSettings(settings: Record<string, unknown>): void }>();
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
		if (refreshSizes && sizesEnabled) {
			await sizesQuery.refetch({ cancelRefetch: false });
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
		tablePreferences?.persistCustomSettings(customSettings);
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

	let columnVisibility = $state<ColumnVisibilityState>({});
	let tablePreferencesReady = $state(false);
	const queryClient = useQueryClient();
	const sizesEnabled = $derived.by(() => {
		$userStore;
		return (
			tablePreferencesReady &&
			!!environmentStore.selected?.id &&
			columnVisibility['size'] !== false &&
			hasPermission('volumes:read', currentEnvId)
		);
	});
	const sizesQuery = createQuery(() => {
		const environmentId = currentEnvId;
		return {
			queryKey: queryKeys.volumes.sizes(environmentId),
			queryFn: () => volumeService.getVolumeSizes(environmentId),
			enabled: sizesEnabled,
			refetchOnWindowFocus: false,
			retry: false
		};
	});
	const sizesMap = $derived.by(() => {
		const sizes = new Map<string, VolumeSizeInfo>();
		if (!sizesEnabled) return sizes;
		for (const size of sizesQuery.data ?? []) sizes.set(size.name, size);
		return sizes;
	});
	const sizesLoading = $derived(sizesEnabled && sizesQuery.isFetching);

	onMount(() => {
		const cache = queryClient.getQueryCache();
		return cache.subscribe((event) => {
			if (event.type !== 'updated') return;
			if (event.query !== cache.find({ queryKey: queryKeys.volumes.sizes(currentEnvId), exact: true })) return;
			if (event.action.type === 'error') {
				console.error('Failed to load volume sizes:', event.query.state.error);
				return;
			}
			if (event.action.type === 'success' && sizesEnabled && requestOptions?.sort?.column === 'size') {
				void tryCatch(refreshVolumes(requestOptions, false)).then((result) => {
					if (result.error) console.error('Failed to refresh volume size sorting:', result.error);
				});
			}
		});
	});

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
					await handleApiResultWithCallbacks({
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

	function handleRenameVolume(item: VolumeSummaryDto) {
		volumeToRename = item;
		renameEnvironmentId = currentEnvId;
		renameSession += 1;
		renameDialogOpen = true;
	}

	async function handleRenameSubmit(newName: string) {
		if (!volumeToRename) return;
		const oldName = volumeToRename.name;
		isLoading.renaming = true;
		await handleApiResultWithCallbacks({
			result: await tryCatch(volumeService.renameVolume(oldName, { name: newName }, renameEnvironmentId)),
			message: m.volumes_rename_failed({ name: oldName }),
			setLoadingState: (value) => (isLoading.renaming = value),
			onSuccess: async (data) => {
				toast.success(m.volumes_rename_success({ oldName, newName }), activityToastOptions(extractActivityId(data)));
				renameDialogOpen = false;
				if (renameEnvironmentId === currentEnvId) await refreshVolumes();
			}
		});
	}

	function handleDeleteSelected(ids: string[]) {
		// A volume's id IS its name (backend sets ID: v.Name), so ids can be
		// passed to the delete endpoint directly — no per-page name lookup, which
		// would break for selections carried across pages.
		const idsToDelete = ids.filter((id) => !isBackupVolumeName(id));

		bulkConfirmAndRun({
			ids: idsToDelete,
			title: m.volumes_remove_selected_title({ count: idsToDelete.length }),
			message: m.volumes_remove_selected_message({ count: idsToDelete.length }),
			confirmLabel: m.common_remove(),
			destructive: true,
			run: (id) => volumeService.deleteVolume(id),
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

	// Volume id === name, so the backup-volume filter works on the id directly.
	const deletableSelectedIds = $derived((selectedIds ?? []).filter((id) => !isBackupVolumeName(id)));

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

		{#if canRenameVolume || canDeleteVolume}
			<DropdownMenu.Separator />
		{/if}

		{#if canRenameVolume}
			<DropdownMenu.Item
				onclick={() => handleRenameVolume(item)}
				disabled={item.inUse || isBackupVolume(item) || isLoading.renaming}
			>
				<EditIcon class="size-4" />
				{m.rename()}
			</DropdownMenu.Item>
		{/if}

		{#if canDeleteVolume}
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
	bind:this={tablePreferences}
	persistKey="arcane-volumes-table"
	items={volumes}
	bind:requestOptions
	bind:selectedIds
	bind:mobileFieldVisibility
	bind:customSettings
	bind:columnVisibility
	bind:preferencesReady={
		() => tablePreferencesReady,
		(ready) => {
			const wasReady = tablePreferencesReady;
			tablePreferencesReady = ready;
			if (!ready || wasReady) return;
			const persistedInternal = (customSettings['showInternalVolumes'] as boolean) ?? false;
			if (persistedInternal !== (requestOptions?.includeInternal ?? false)) setShowInternal(persistedInternal);
		}
	}
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

{#if renameSession > 0}
	{#key renameSession}
		<RenameVolumeDialog
			bind:open={renameDialogOpen}
			volume={volumeToRename}
			isLoading={isLoading.renaming}
			onSubmit={handleRenameSubmit}
		/>
	{/key}
{/if}

{#snippet CustomViewOptions()}
	<DropdownMenu.CheckboxItem checked={showInternal} onCheckedChange={(v) => setShowInternal(!!v)}>
		{`${m.common_show()} ${m.internal()} ${m.resource_volumes_cap()}`}
	</DropdownMenu.CheckboxItem>
{/snippet}
