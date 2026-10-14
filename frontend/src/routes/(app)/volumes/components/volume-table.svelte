<script lang="ts">
	import { goto } from '$app/navigation';
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import type { ColumnVisibilityState } from '@tanstack/table-core';
	import { onMount } from 'svelte';
	import { toast } from 'svelte-sonner';

	import ArcaneTable from '#lib/components/arcane-table/arcane-table.svelte';
	import InUseStatus from '#lib/components/arcane-table/cells/in-use-status.svelte';
	import { environmentColumns } from '#lib/components/arcane-table/environment-column.js';
	import type { ColumnSpec, MobileFieldVisibility, BulkAction } from '#lib/components/arcane-table/index.js';
	import { UniversalMobileCard } from '#lib/components/arcane-table/index.js';
	import RowActionsMenu from '#lib/components/arcane-table/row-actions-menu.svelte';
	import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { TrashIcon, InspectIcon, VolumesIcon, CalendarIcon, EditIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { settingsService } from '#lib/services/settings-service.js';
	import { volumeService, type VolumesPaginatedResponse } from '#lib/services/volume-service.js';
	import settingsStore from '#lib/stores/config-store.svelte.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import userStore from '#lib/stores/user-store.svelte.js';
	import type { VolumeSizeInfo, VolumeSummaryDto } from '#lib/types/docker.js';
	import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
	import { activityToastOptions, extractActivityId } from '#lib/utils/activity-toast.js';
	import { handleApiResultWithCallbacks, extractApiErrorMessage } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { bulkConfirmAndRun } from '#lib/utils/bulk-actions.js';
	import {
		collectAcrossEnvironments,
		environmentHref,
		environmentRows,
		fetchAcrossEnvironments,
		hasShownPermission,
		parseRowKey,
		readAcrossEnvironments,
		rowKey,
		withFailedEnvironments,
		type EnvironmentPage,
		type EnvironmentRow
	} from '#lib/utils/environments.js';
	import { formatDateTimeShort, truncateString } from '#lib/utils/formatting.js';
	import { bytes } from '#lib/utils/formatting.js';
	import { inUseBadge } from '#lib/utils/mobile-card-badges.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import RenameVolumeDialog from './rename-volume-dialog.svelte';

	type VolumeRow = EnvironmentRow<VolumeSummaryDto>;

	let {
		volumes = $bindable(),
		selectedIds = $bindable(),
		requestOptions = $bindable(),
		onRefreshData
	}: {
		volumes: EnvironmentPage<VolumesPaginatedResponse>;
		selectedIds: string[];
		requestOptions: SearchPaginationSortRequest;
		onRefreshData?: (options: SearchPaginationSortRequest) => Promise<void>;
	} = $props();

	let isLoading = $state({
		removing: false,
		renaming: false
	});
	let volumeToRename = $state<VolumeRow | null>(null);
	let renameDialogOpen = $state(false);
	let renameSession = $state(0);
	let renameEnvironmentId = $state('0');

	const scope = $derived(environmentStore.shownKey);
	// Track the user store: hasPermission reads it non-reactively, so without
	// this the derived would cache a pre-hydration false forever.
	const canDeleteVolume = $derived.by(() => {
		userStore.current;
		return hasShownPermission('volumes:delete');
	});
	const canRenameVolume = $derived.by(() => {
		userStore.current;
		return hasShownPermission('volumes:rename');
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
			volumes = await fetchAcrossEnvironments(environmentStore.shown, options, (id, request, config) =>
				volumeService.getVolumesForEnvironment(id, request, config)
			);
		}
		if (refreshSizes && sizesEnabled) {
			await sizesQuery.refetch({ cancelRefetch: false });
		}
		// A retry also reloads backup volume names an environment failed to return.
		if (backupVolumeNamesQuery.isError || backupVolumeNamesQuery.data?.failures.length) {
			await backupVolumeNamesQuery.refetch();
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

	let columnVisibility = $state<ColumnVisibilityState>({});
	let tablePreferencesReady = $state(false);
	const queryClient = useQueryClient();

	// Each environment names its own backup volume; other shown environments' names come from their settings.
	const backupVolumeNamesQuery = createQuery(() => {
		const selectedId = environmentStore.selected?.id;
		const others = environmentStore.shown.filter((env) => env.id !== selectedId);
		return {
			// The selected environment is left out, so it is part of the key.
			queryKey: queryKeys.volumes.backupVolumeNames(`${scope}|${selectedId}`),
			queryFn: async () => {
				const { values, failures } = await collectAcrossEnvironments(others, (id) =>
					queryClient.ensureQueryData({
						queryKey: queryKeys.settings.byEnvironment(id),
						queryFn: () => settingsService.getReadableSettings(id)
					})
				);
				const names = new Map(
					values.map(({ environment, value }) => [environment.id, value.backupVolumeName || 'arcane-backups'])
				);
				return { names, failures };
			},
			enabled: others.length > 0,
			refetchOnWindowFocus: false
		};
	});
	const isBackupVolumeName = (name: string, environmentId = environmentStore.selected?.id) => {
		if (environmentId === environmentStore.selected?.id) {
			return name === (settingsStore.current?.backupVolumeName || 'arcane-backups');
		}
		// Until another environment's backup volume name is known, none of its volumes can be removed or renamed.
		const configured = backupVolumeNamesQuery.data?.names.get(environmentId ?? '');
		return configured === undefined || name === configured;
	};
	const isBackupVolume = (item: VolumeRow) => isBackupVolumeName(item.name, item.environmentId);
	const sizesEnabled = $derived.by(() => {
		userStore.current;
		return (
			tablePreferencesReady &&
			!!environmentStore.selected?.id &&
			columnVisibility['size'] !== false &&
			hasShownPermission('volumes:read')
		);
	});
	const sizesQuery = createQuery(() => {
		const queryScope = scope;
		const environments = environmentStore.shown;
		return {
			queryKey: queryKeys.volumes.sizes(queryScope),
			queryFn: async () => {
				const readable = environments.filter((env) => hasPermission('volumes:read', env.id));
				if (readable.length === 0) return [];
				const { value } = await readAcrossEnvironments(readable, (scope, config) => volumeService.getVolumeSizes(scope, config));
				return environmentRows(value, readable);
			},
			enabled: sizesEnabled,
			refetchOnWindowFocus: false,
			retry: false
		};
	});
	const sizesMap = $derived.by(() => {
		const sizes = new Map<string, VolumeSizeInfo>();
		if (!sizesEnabled) return sizes;
		for (const size of sizesQuery.data ?? []) sizes.set(rowKey({ ...size, id: size.name }), size);
		return sizes;
	});
	const sizesLoading = $derived(sizesEnabled && sizesQuery.isFetching);

	onMount(() => {
		const cache = queryClient.getQueryCache();
		return cache.subscribe((event) => {
			if (event.type !== 'updated') return;
			if (event.query !== cache.find({ queryKey: queryKeys.volumes.sizes(scope), exact: true })) return;
			if (event.action.type === 'success' && sizesEnabled && requestOptions?.sort?.column === 'size') {
				void tryCatch(refreshVolumes(requestOptions, false)).then((result) =>
					handleApiResultWithCallbacks({ result, message: m.common_refresh_failed({ resource: m.resource_volumes_cap() }) })
				);
			}
		});
	});

	async function handleRemoveVolumeConfirm(name: string, environmentId: string) {
		const safeName = name?.trim() || m.common_unknown();
		if (isBackupVolumeName(safeName, environmentId)) return;
		openConfirmDialog({
			title: m.common_remove_title({ resource: m.resource_volume() }),
			message: m.common_remove_confirm({ resource: `${m.resource_volume()} "${safeName}"` }),
			confirm: {
				label: m.common_remove(),
				destructive: true,
				action: async () => {
					isLoading.removing = true;
					await handleApiResultWithCallbacks({
						result: await tryCatch(volumeService.deleteVolume(safeName, environmentId)),
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

	function handleRenameVolume(item: VolumeRow) {
		volumeToRename = item;
		renameEnvironmentId = item.environmentId;
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
				if (environmentStore.shown.some((env) => env.id === renameEnvironmentId)) await refreshVolumes();
			}
		});
	}

	function handleDeleteSelected(ids: string[]) {
		// A volume's id IS its name (backend sets ID: v.Name), so ids can be
		// passed to the delete endpoint directly — no per-page name lookup, which
		// would break for selections carried across pages.
		const idsToDelete = ids.filter((key) => {
			const { environmentId, id } = parseRowKey(key);
			return !isBackupVolumeName(id, environmentId);
		});

		bulkConfirmAndRun({
			ids: idsToDelete,
			title: m.volumes_remove_selected_title({ count: idsToDelete.length }),
			message: m.volumes_remove_selected_message({ count: idsToDelete.length }),
			confirmLabel: m.common_remove(),
			destructive: true,
			run: (id, environmentId) => volumeService.deleteVolume(id, environmentId),
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

	const columns = $derived([
		...environmentColumns<VolumeRow>(),
		{ accessorKey: 'id', title: m.common_id(), hidden: true },
		{ accessorKey: 'name', title: m.common_name(), sortable: true, cell: NameCell },
		{ accessorKey: 'inUse', title: m.common_status(), sortable: true, cell: StatusCell },
		{ accessorKey: 'size', title: m.common_size(), sortable: true, cell: SizeCell },
		{ accessorKey: 'createdAt', title: m.common_created(), sortable: true, cell: CreatedCell },
		{ accessorKey: 'driver', title: m.common_driver(), sortable: true }
	] satisfies ColumnSpec<VolumeRow>[]);

	const mobileFields = [
		{ id: 'id', label: m.common_id(), defaultVisible: false },
		{ id: 'inUse', label: m.common_status(), defaultVisible: true },
		{ id: 'size', label: m.common_size(), defaultVisible: true },
		{ id: 'createdAt', label: m.common_created(), defaultVisible: true },
		{ id: 'driver', label: m.common_driver(), defaultVisible: true }
	];

	// Volume id === name, so the backup-volume filter works on the id directly.
	const deletableSelectedIds = $derived(
		(selectedIds ?? []).filter((key) => {
			const { environmentId, id } = parseRowKey(key);
			return !isBackupVolumeName(id, environmentId);
		})
	);

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

{#snippet NameCell({ item }: { item: VolumeRow })}
	<a class="font-medium hover:underline" href={environmentHref(`/volumes/${item.id}`, item.environmentId)} title={item.name}>
		{truncateString(item.name, 40)}
	</a>
{/snippet}

{#snippet StatusCell({ item }: { item: VolumeRow })}
	<InUseStatus inUse={item.inUse} />
{/snippet}

{#snippet SizeCell({ item }: { item: VolumeRow })}
	{@const sizeInfo = sizesMap.get(rowKey(item))}
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
	{:else if sizesEnabled && sizesQuery.isError}
		<span
			class="text-sm text-muted-foreground"
			title={m.volumes_size_calculation_failed({ error: extractApiErrorMessage(sizesQuery.error) })}
			>{m.common_unavailable()}</span
		>
	{:else}
		<span class="text-sm text-muted-foreground" title={sizesEnabled ? m.volumes_size_not_reported() : undefined}>-</span>
	{/if}
{/snippet}

{#snippet CreatedCell({ value }: { value: unknown })}
	{formatDateTimeShort(String(value))}
{/snippet}

{#snippet VolumeMobileCardSnippet({
	item,
	mobileFieldVisibility
}: {
	item: VolumeRow;
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
				getValue: (item: VolumeRow) => item.driver,
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
		onclick={() => goto(environmentHref(`/volumes/${item.id}`, item.environmentId))}
	/>
{/snippet}

{#snippet RowActions({ item }: { item: VolumeRow })}
	<RowActionsMenu>
		<DropdownMenu.Item onclick={() => goto(environmentHref(`/volumes/${item.id}`, item.environmentId))}>
			<InspectIcon class="size-4" />
			{m.common_inspect()}
		</DropdownMenu.Item>

		{#if canRenameVolume || canDeleteVolume}
			<DropdownMenu.Separator />
		{/if}

		{#if canRenameVolume && hasPermission('volumes:rename', item.environmentId)}
			<DropdownMenu.Item
				onclick={() => handleRenameVolume(item)}
				disabled={item.inUse || isBackupVolume(item) || isLoading.renaming}
			>
				<EditIcon class="size-4" />
				{m.rename()}
			</DropdownMenu.Item>
		{/if}

		{#if canDeleteVolume && hasPermission('volumes:delete', item.environmentId)}
			<DropdownMenu.Item
				variant="destructive"
				onclick={() => handleRemoveVolumeConfirm(item.name, item.environmentId)}
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
	items={withFailedEnvironments(volumes, backupVolumeNamesQuery.data?.failures)}
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
