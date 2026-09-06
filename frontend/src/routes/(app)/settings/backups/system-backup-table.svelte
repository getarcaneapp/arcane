<script lang="ts">
	import RemoveMenuItem from '#lib/components/arcane-table/cells/remove-menu-item.svelte';
	import { backupRunColumns, backupRunMobileFields } from '#lib/components/arcane-table/backup-columns.js';
	import ArcaneTable from '#lib/components/arcane-table/arcane-table.svelte';
	import RowActionsMenu from '#lib/components/arcane-table/row-actions-menu.svelte';
	import { UniversalMobileCard, type ColumnSpec, type MobileFieldVisibility } from '#lib/components/arcane-table/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import BackupStatusCell from '#lib/components/arcane-table/cells/backup-status-cell.svelte';
	import BackupTriggerCell from '#lib/components/arcane-table/cells/backup-trigger-cell.svelte';
	import BackupDestinationCell from '#lib/components/arcane-table/cells/backup-destination-cell.svelte';
	import BackupSizeCell from '#lib/components/arcane-table/cells/backup-size-cell.svelte';
	import CreatedAtCell from '#lib/components/arcane-table/cells/created-at-cell.svelte';
	import BackupManagementCell from '#lib/components/arcane-table/cells/backup-management-cell.svelte';
	import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared.js';
	import type { BackupHistoryEntry } from '#lib/types/system-backup.js';
	import { BackupIcon, RestartIcon, UploadIcon, ClockIcon, VolumesIcon, FileTextIcon } from '#lib/icons/index.js';
	import { bytes, formatDateTimeShort } from '#lib/utils/formatting.js';
	import {
		backupManagementFilterOptions,
		backupManagementLabel,
		backupStatusLabel,
		backupStatusVariant,
		backupTriggerLabel
	} from '#lib/utils/backups.js';
	import * as m from '#lib/paraglide/messages.js';

	let {
		backups = $bindable(),
		requestOptions = $bindable(),
		onChanged,
		onRestore,
		onRestoreFiles,
		onUpload,
		onDelete,
		onOpenVolume
	}: {
		backups: Paginated<BackupHistoryEntry>;
		requestOptions: SearchPaginationSortRequest;
		onChanged: (options: SearchPaginationSortRequest) => Promise<Paginated<BackupHistoryEntry>>;
		onRestore: (backup: BackupHistoryEntry) => void;
		onRestoreFiles: (backup: BackupHistoryEntry) => void;
		onUpload: (backup: BackupHistoryEntry) => void;
		onDelete: (backup: BackupHistoryEntry) => void;
		onOpenVolume: (backup: BackupHistoryEntry) => void;
	} = $props();

	let mobileFieldVisibility = $state<Record<string, boolean>>({});
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
	] satisfies ColumnSpec<BackupHistoryEntry>[];
	const mobileFields = [{ id: 'resourceName', label: m.backups_resource(), defaultVisible: true }, ...backupRunMobileFields()];
</script>

{#snippet IdCell({ item }: { item: BackupHistoryEntry })}<code class="text-xs">{item.id.slice(0, 18)}…</code>{/snippet}
{#snippet ResourceCell({ item }: { item: BackupHistoryEntry })}<span class="font-medium">{item.resourceName}</span>{/snippet}
{#snippet TypeCell({ item }: { item: BackupHistoryEntry })}
	<BackupManagementCell type={item.type} />
{/snippet}
{#snippet StatusCell({ item }: { item: BackupHistoryEntry })}<BackupStatusCell status={item.status} />{/snippet}
{#snippet TriggerCell({ item }: { item: BackupHistoryEntry })}<BackupTriggerCell trigger={item.trigger} />{/snippet}
{#snippet DestinationCell({ item }: { item: BackupHistoryEntry })}<BackupDestinationCell {item} />{/snippet}
{#snippet SizeCell({ item }: { item: BackupHistoryEntry })}<BackupSizeCell size={item.size} />{/snippet}
{#snippet ErrorCell({ item }: { item: BackupHistoryEntry })}<span class="max-w-72 truncate text-red-500">{item.error || '-'}</span
	>{/snippet}

{#snippet RowActions({ item }: { item: BackupHistoryEntry })}
	<RowActionsMenu>
		{#if item.resourceType === 'system'}
			<DropdownMenu.Item onclick={() => onRestore(item)} disabled={item.status !== 'succeeded'}
				><RestartIcon class="size-4" />{m.volumes_backups_restore()}</DropdownMenu.Item
			>
			<DropdownMenu.Item onclick={() => onRestoreFiles(item)} disabled={item.status !== 'succeeded'}
				><FileTextIcon class="size-4" />{m.volume_restore_files()}</DropdownMenu.Item
			>
			{#if item.localSnapshotId && !item.remoteSnapshotId}
				<DropdownMenu.Item onclick={() => onUpload(item)}><UploadIcon class="size-4" />{m.backups_upload_s3()}</DropdownMenu.Item>
			{/if}
			<RemoveMenuItem onclick={() => onDelete(item)} label={m.common_delete()} />
		{:else}
			<DropdownMenu.Item onclick={() => onOpenVolume(item)}
				><VolumesIcon class="size-4" />{m.backups_open_volume()}</DropdownMenu.Item
			>
		{/if}
	</RowActionsMenu>
{/snippet}

{#snippet MobileCard({ item, mobileFieldVisibility }: { item: BackupHistoryEntry; mobileFieldVisibility: MobileFieldVisibility })}
	<UniversalMobileCard
		{item}
		icon={{ component: BackupIcon, variant: 'blue' }}
		title={(item) => item.resourceName}
		badges={[
			(item) => ({
				variant: backupStatusVariant(item.status),
				text: backupStatusLabel(item.status)
			}),
			(item) => ({
				variant: 'purple',
				text: backupManagementLabel(item.type)
			})
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
	persistKey="arcane-system-backups-table"
	items={backups}
	bind:requestOptions
	bind:mobileFieldVisibility
	onRefresh={async (options) => {
		requestOptions = options;
		backups = await onChanged(options);
		return backups;
	}}
	{columns}
	{mobileFields}
	rowActions={RowActions}
	mobileCard={MobileCard}
/>
