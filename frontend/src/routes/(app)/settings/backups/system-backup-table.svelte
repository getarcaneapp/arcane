<script lang="ts">
	import ArcaneTable from '#lib/components/arcane-table/arcane-table.svelte';
	import RowActionsMenu from '#lib/components/arcane-table/row-actions-menu.svelte';
	import { UniversalMobileCard, type ColumnSpec, type MobileFieldVisibility } from '#lib/components/arcane-table';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu';
	import BackupStatusCell from '#lib/components/arcane-table/cells/backup-status-cell.svelte';
	import BackupTriggerCell from '#lib/components/arcane-table/cells/backup-trigger-cell.svelte';
	import BackupDestinationCell from '#lib/components/arcane-table/cells/backup-destination-cell.svelte';
	import BackupSizeCell from '#lib/components/arcane-table/cells/backup-size-cell.svelte';
	import CreatedAtCell from '#lib/components/arcane-table/cells/created-at-cell.svelte';
	import { Badge } from '#lib/components/ui/badge';
	import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared';
	import type { BackupHistoryEntry } from '#lib/types/system-backup';
	import { BackupIcon, RestartIcon, TrashIcon, UploadIcon, ClockIcon, VolumesIcon } from '#lib/icons';
	import { bytes, formatDateTimeShort } from '#lib/utils/formatting';
	import { backupStatusLabel, backupStatusVariant, backupTriggerLabel } from '#lib/utils/backups';
	import * as m from '#lib/paraglide/messages.js';

	let {
		backups = $bindable(),
		requestOptions = $bindable(),
		onChanged,
		onRestore,
		onUpload,
		onDelete,
		onOpenVolume
	}: {
		backups: Paginated<BackupHistoryEntry>;
		requestOptions: SearchPaginationSortRequest;
		onChanged: (options: SearchPaginationSortRequest) => Promise<Paginated<BackupHistoryEntry>>;
		onRestore: (backup: BackupHistoryEntry) => void;
		onUpload: (backup: BackupHistoryEntry) => void;
		onDelete: (backup: BackupHistoryEntry) => void;
		onOpenVolume: (backup: BackupHistoryEntry) => void;
	} = $props();

	let mobileFieldVisibility = $state<Record<string, boolean>>({});
	const typeFilterOptions = [
		{ label: m.backups_system_managed(), value: 'system' },
		{ label: m.backups_volume_managed(), value: 'volume' }
	];
	const columns = [
		{ accessorKey: 'id', title: m.system_backups_id(), sortable: false, cell: IdCell },
		{ accessorKey: 'resourceName', title: m.backups_resource(), sortable: true, cell: ResourceCell },
		{
			accessorKey: 'type',
			title: m.common_type(),
			sortable: true,
			cell: TypeCell,
			filterOptions: typeFilterOptions
		},
		{ accessorKey: 'status', title: m.common_status(), sortable: true, cell: StatusCell },
		{ accessorKey: 'trigger', title: m.volume_backup_trigger(), sortable: true, cell: TriggerCell },
		{ accessorKey: 'destination', title: m.backups_destination_label(), sortable: true, cell: DestinationCell },
		{ accessorKey: 'size', title: m.common_size(), sortable: true, cell: SizeCell },
		{ accessorKey: 'createdAt', title: m.common_created(), sortable: true, cell: CreatedCell },
		{ accessorKey: 'error', title: m.common_error(), sortable: false, cell: ErrorCell }
	] satisfies ColumnSpec<BackupHistoryEntry>[];
	const mobileFields = [
		{ id: 'resourceName', label: m.backups_resource(), defaultVisible: true },
		{ id: 'type', label: m.common_type(), defaultVisible: true },
		{ id: 'status', label: m.common_status(), defaultVisible: true },
		{ id: 'trigger', label: m.volume_backup_trigger(), defaultVisible: true },
		{ id: 'destination', label: m.backups_destination_label(), defaultVisible: true },
		{ id: 'size', label: m.common_size(), defaultVisible: true }
	];
</script>

{#snippet IdCell({ item }: { item: BackupHistoryEntry })}<code class="text-xs">{item.id.slice(0, 18)}…</code>{/snippet}
{#snippet ResourceCell({ item }: { item: BackupHistoryEntry })}<span class="font-medium">{item.resourceName}</span>{/snippet}
{#snippet TypeCell({ item }: { item: BackupHistoryEntry })}
	<Badge variant="purple">{item.type === 'system' ? m.backups_system_managed() : m.backups_volume_managed()}</Badge>
{/snippet}
{#snippet StatusCell({ item }: { item: BackupHistoryEntry })}<BackupStatusCell status={item.status} />{/snippet}
{#snippet TriggerCell({ item }: { item: BackupHistoryEntry })}<BackupTriggerCell trigger={item.trigger} />{/snippet}
{#snippet DestinationCell({ item }: { item: BackupHistoryEntry })}<BackupDestinationCell {item} />{/snippet}
{#snippet SizeCell({ item }: { item: BackupHistoryEntry })}<BackupSizeCell size={item.size} />{/snippet}
{#snippet CreatedCell({ item }: { item: BackupHistoryEntry })}<CreatedAtCell value={item.createdAt} />{/snippet}
{#snippet ErrorCell({ item }: { item: BackupHistoryEntry })}<span class="max-w-72 truncate text-red-500">{item.error || '-'}</span
	>{/snippet}

{#snippet RowActions({ item }: { item: BackupHistoryEntry })}
	<RowActionsMenu>
		{#if item.resourceType === 'system'}
			<DropdownMenu.Item onclick={() => onRestore(item)} disabled={item.status !== 'succeeded'}
				><RestartIcon class="size-4" />{m.volumes_backups_restore()}</DropdownMenu.Item
			>
			{#if item.localSnapshotId && !item.remoteSnapshotId}
				<DropdownMenu.Item onclick={() => onUpload(item)}><UploadIcon class="size-4" />{m.backups_upload_s3()}</DropdownMenu.Item>
			{/if}
			<DropdownMenu.Separator />
			<DropdownMenu.Item variant="destructive" onclick={() => onDelete(item)}
				><TrashIcon class="size-4" />{m.common_delete()}</DropdownMenu.Item
			>
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
				text: item.type === 'system' ? m.backups_system_managed() : m.backups_volume_managed()
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
