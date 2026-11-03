<script lang="ts">
	import type { Snippet } from 'svelte';

	import ArcaneTable from '#lib/components/arcane-table/arcane-table.svelte';
	import { backupRunColumns, backupRunMobileFields, backupManagementBadge } from '#lib/components/arcane-table/backup-columns.js';
	import BackupDestinationCell from '#lib/components/arcane-table/cells/backup-destination-cell.svelte';
	import BackupManagementCell from '#lib/components/arcane-table/cells/backup-management-cell.svelte';
	import BackupSizeCell from '#lib/components/arcane-table/cells/backup-size-cell.svelte';
	import BackupStatusCell from '#lib/components/arcane-table/cells/backup-status-cell.svelte';
	import BackupTriggerCell from '#lib/components/arcane-table/cells/backup-trigger-cell.svelte';
	import CreatedAtCell from '#lib/components/arcane-table/cells/created-at-cell.svelte';
	import RemoveMenuItem from '#lib/components/arcane-table/cells/remove-menu-item.svelte';
	import { UniversalMobileCard, type ColumnSpec, type MobileFieldVisibility } from '#lib/components/arcane-table/index.js';
	import RowActionsMenu from '#lib/components/arcane-table/row-actions-menu.svelte';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { BackupIcon, RestartIcon, UploadIcon, ClockIcon, VolumesIcon, FileTextIcon } from '#lib/icons/index.js';
	import * as m from '#lib/paraglide/messages.js';
	import { LOCAL_DOCKER_ENVIRONMENT_ID } from '#lib/stores/environment.store.svelte.js';
	import type { SystemVolumeBackupHistoryEntry } from '#lib/types/backup.js';
	import type { Paginated } from '#lib/types/base.js';
	import { GLOBAL_SCOPE } from '#lib/types/role.js';
	import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
	import { hasPermission, isGlobalAdmin } from '#lib/utils/auth.js';
	import {
		backupManagementFilterOptions,
		backupStatusLabel,
		backupStatusVariant,
		backupTriggerLabel
	} from '#lib/utils/backups.js';
	import { bytes, formatDateTimeShort } from '#lib/utils/formatting.js';

	let {
		backups,
		requestOptions = $bindable(),
		onRetry,
		onRestore,
		onRestoreFiles,
		onRestoreVolume,
		onUpload,
		onDelete,
		onOpenVolume,
		emptyState
	}: {
		backups: Paginated<SystemVolumeBackupHistoryEntry>;
		requestOptions: SearchPaginationSortRequest;
		onRetry: () => Promise<unknown>;
		onRestore: (backup: SystemVolumeBackupHistoryEntry) => void;
		onRestoreFiles: (backup: SystemVolumeBackupHistoryEntry) => void;
		onRestoreVolume?: (backup: SystemVolumeBackupHistoryEntry) => void;
		onUpload: (backup: SystemVolumeBackupHistoryEntry) => void;
		onDelete: (backup: SystemVolumeBackupHistoryEntry) => void;
		onOpenVolume: (backup: SystemVolumeBackupHistoryEntry) => void;
		emptyState?: Snippet;
	} = $props();

	let mobileFieldVisibility = $state<Record<string, boolean>>({});
	const canUploadBackups = $derived(hasPermission('system-backups:upload', GLOBAL_SCOPE));
	const canDeleteBackups = $derived(hasPermission('system-backups:delete', GLOBAL_SCOPE));
	const canRestoreBackups = $derived(isGlobalAdmin());
	const canRestoreVolume = $derived(hasPermission('volumes:backup', LOCAL_DOCKER_ENVIRONMENT_ID));
	const columns = [
		{ accessorKey: 'id', title: m.system_backups_id(), sortable: false, cell: IdCell },
		{ accessorKey: 'resourceName', title: m.backups_resource(), sortable: true, cell: ResourceCell },
		{
			accessorKey: 'type',
			title: m.common_type(),
			sortable: true,
			cell: TypeCell,
			filterOptions: backupManagementFilterOptions()
		},
		...backupRunColumns({ status: StatusCell, trigger: TriggerCell, destination: DestinationCell, size: SizeCell }),
		{ accessorKey: 'createdAt', title: m.common_created(), sortable: true, cellComponent: CreatedAtCell },
		{ accessorKey: 'error', title: m.common_error(), sortable: false, cell: ErrorCell }
	] satisfies ColumnSpec<SystemVolumeBackupHistoryEntry>[];
	const mobileFields = [{ id: 'resourceName', label: m.backups_resource(), defaultVisible: true }, ...backupRunMobileFields()];
</script>

{#snippet IdCell({ item }: { item: SystemVolumeBackupHistoryEntry })}<code class="text-xs">{item.id.slice(0, 18)}…</code
	>{/snippet}
{#snippet ResourceCell({ item }: { item: SystemVolumeBackupHistoryEntry })}<span class="font-medium">{item.resourceName}</span
	>{/snippet}
{#snippet TypeCell({ item }: { item: SystemVolumeBackupHistoryEntry })}
	<BackupManagementCell type={item.type} />
{/snippet}
{#snippet StatusCell({ item }: { item: SystemVolumeBackupHistoryEntry })}<BackupStatusCell status={item.status} />{/snippet}
{#snippet TriggerCell({ item }: { item: SystemVolumeBackupHistoryEntry })}<BackupTriggerCell trigger={item.trigger} />{/snippet}
{#snippet DestinationCell({ item }: { item: SystemVolumeBackupHistoryEntry })}<BackupDestinationCell {item} />{/snippet}
{#snippet SizeCell({ item }: { item: SystemVolumeBackupHistoryEntry })}<BackupSizeCell size={item.size} />{/snippet}
{#snippet ErrorCell({ item }: { item: SystemVolumeBackupHistoryEntry })}<span class="max-w-72 truncate text-destructive"
		>{item.error || '-'}</span
	>{/snippet}

{#snippet RowActions({ item }: { item: SystemVolumeBackupHistoryEntry })}
	<RowActionsMenu>
		{#if item.resourceType === 'system'}
			{#if canRestoreBackups}
				<DropdownMenu.Item onclick={() => onRestore(item)} disabled={item.status !== 'succeeded'}
					><RestartIcon class="size-4" />{m.volumes_backups_restore()}</DropdownMenu.Item
				>
				<DropdownMenu.Item onclick={() => onRestoreFiles(item)} disabled={item.status !== 'succeeded'}
					><FileTextIcon class="size-4" />{m.volume_restore_files()}</DropdownMenu.Item
				>
			{/if}
			{#if canUploadBackups && item.localSnapshotId && !item.remoteSnapshotId}
				<DropdownMenu.Item onclick={() => onUpload(item)}><UploadIcon class="size-4" />{m.backups_upload_s3()}</DropdownMenu.Item>
			{/if}
			{#if canDeleteBackups}
				<RemoveMenuItem onclick={() => onDelete(item)} label={m.common_delete()} />
			{/if}
		{:else}
			{#if onRestoreVolume && canRestoreVolume}
				<DropdownMenu.Item onclick={() => onRestoreVolume(item)} disabled={item.status !== 'succeeded'}
					><RestartIcon class="size-4" />{m.volumes_backups_restore()}</DropdownMenu.Item
				>
			{/if}
			<DropdownMenu.Item onclick={() => onOpenVolume(item)}
				><VolumesIcon class="size-4" />{m.backups_open_volume()}</DropdownMenu.Item
			>
		{/if}
	</RowActionsMenu>
{/snippet}

{#snippet MobileCard({
	item,
	mobileFieldVisibility
}: {
	item: SystemVolumeBackupHistoryEntry;
	mobileFieldVisibility: MobileFieldVisibility;
})}
	<UniversalMobileCard
		{item}
		icon={{ component: BackupIcon, variant: 'blue' }}
		title={(item) => item.resourceName}
		badges={[
			(item) => ({
				variant: backupStatusVariant(item.status),
				text: backupStatusLabel(item.status)
			}),
			backupManagementBadge
		]}
		fields={[
			{
				label: m.volume_backup_trigger(),
				getValue: (item) => backupTriggerLabel(item.trigger),
				icon: BackupIcon,
				iconVariant: 'gray',
				show: mobileFieldVisibility['trigger'] ?? true
			},
			{
				label: m.common_size(),
				getValue: (item) => bytes(item.size),
				icon: BackupIcon,
				iconVariant: 'gray',
				show: mobileFieldVisibility['size'] ?? true
			}
		]}
		footer={{ label: m.common_created(), getValue: (item) => formatDateTimeShort(item.createdAt), icon: ClockIcon }}
		rowActions={RowActions}
	/>
{/snippet}

<ArcaneTable
	{emptyState}
	persistKey="arcane-system-backups-table"
	items={backups}
	bind:requestOptions
	bind:mobileFieldVisibility
	{onRetry}
	{columns}
	{mobileFields}
	rowActions={RowActions}
	mobileCard={MobileCard}
/>
