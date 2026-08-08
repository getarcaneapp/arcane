<script lang="ts">
	import ArcaneTable from '#lib/components/arcane-table/arcane-table.svelte';
	import RowActionsMenu from '#lib/components/arcane-table/row-actions-menu.svelte';
	import { UniversalMobileCard, type ColumnSpec, type MobileFieldVisibility } from '#lib/components/arcane-table';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu';
	import { Badge } from '#lib/components/ui/badge';
	import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared';
	import type { SystemBackupRun } from '#lib/types/system-backup';
	import { BackupIcon, RestartIcon, TrashIcon, UploadIcon, ClockIcon } from '#lib/icons';
	import { bytes, formatDateTimeShort } from '#lib/utils/formatting';
	import { backupDestinationDisplay, backupStatusLabel, backupStatusVariant, backupTriggerLabel } from '#lib/utils/backups';
	import * as m from '#lib/paraglide/messages.js';

	let {
		backups = $bindable(),
		requestOptions = $bindable(),
		onChanged,
		onRestore,
		onUpload,
		onDelete
	}: {
		backups: Paginated<SystemBackupRun>;
		requestOptions: SearchPaginationSortRequest;
		onChanged: (options: SearchPaginationSortRequest) => Promise<Paginated<SystemBackupRun>>;
		onRestore: (backup: SystemBackupRun) => void;
		onUpload: (backup: SystemBackupRun) => void;
		onDelete: (backup: SystemBackupRun) => void;
	} = $props();

	let mobileFieldVisibility = $state<Record<string, boolean>>({});
	const columns = [
		{ accessorKey: 'id', title: m.system_backups_id(), sortable: false, cell: IdCell },
		{ accessorKey: 'status', title: m.common_status(), sortable: true, cell: StatusCell },
		{ accessorKey: 'trigger', title: m.volume_backup_trigger(), sortable: true, cell: TriggerCell },
		{ accessorKey: 'destination', title: m.backups_destination_label(), sortable: true, cell: DestinationCell },
		{ accessorKey: 'size', title: m.common_size(), sortable: true, cell: SizeCell },
		{ accessorKey: 'createdAt', title: m.common_created(), sortable: true, cell: CreatedCell },
		{ accessorKey: 'error', title: m.common_error(), sortable: false, cell: ErrorCell }
	] satisfies ColumnSpec<SystemBackupRun>[];
	const mobileFields = [
		{ id: 'status', label: m.common_status(), defaultVisible: true },
		{ id: 'trigger', label: m.volume_backup_trigger(), defaultVisible: true },
		{ id: 'destination', label: m.backups_destination_label(), defaultVisible: true },
		{ id: 'size', label: m.common_size(), defaultVisible: true }
	];
</script>

{#snippet IdCell({ item }: { item: SystemBackupRun })}<code class="text-xs">{item.id.slice(0, 18)}…</code>{/snippet}
{#snippet StatusCell({ item }: { item: SystemBackupRun })}<Badge variant={backupStatusVariant(item.status)}
		>{backupStatusLabel(item.status)}</Badge
	>{/snippet}
{#snippet TriggerCell({ item }: { item: SystemBackupRun })}{backupTriggerLabel(item.trigger)}{/snippet}
{#snippet DestinationCell({ item }: { item: SystemBackupRun })}{backupDestinationDisplay(item)}{/snippet}
{#snippet SizeCell({ item }: { item: SystemBackupRun })}{bytes(item.size)}{/snippet}
{#snippet CreatedCell({ item }: { item: SystemBackupRun })}{formatDateTimeShort(item.createdAt)}{/snippet}
{#snippet ErrorCell({ item }: { item: SystemBackupRun })}<span class="max-w-72 truncate text-red-500">{item.error || '-'}</span
	>{/snippet}

{#snippet RowActions({ item }: { item: SystemBackupRun })}
	<RowActionsMenu>
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
	</RowActionsMenu>
{/snippet}

{#snippet MobileCard({ item, mobileFieldVisibility }: { item: SystemBackupRun; mobileFieldVisibility: MobileFieldVisibility })}
	<UniversalMobileCard
		{item}
		icon={{ component: BackupIcon, variant: 'blue' }}
		title={(item) => backupDestinationDisplay(item)}
		badges={[
			(item) => ({
				variant: backupStatusVariant(item.status),
				text: backupStatusLabel(item.status)
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
