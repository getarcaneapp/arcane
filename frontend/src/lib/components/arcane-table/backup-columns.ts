import type { ColumnSpec } from './arcane-table.types.svelte';
import type { BackupRun } from '#lib/types/backup.js';
import { m } from '#lib/paraglide/messages.js';

export function backupRunColumns<T extends BackupRun>(cells: {
	status: ColumnSpec<T>['cell'];
	trigger: ColumnSpec<T>['cell'];
	destination: ColumnSpec<T>['cell'];
	size: ColumnSpec<T>['cell'];
}): ColumnSpec<T>[] {
	return [
		{ accessorKey: 'status', title: m.common_status(), sortable: true, cell: cells.status },
		{ accessorKey: 'trigger', title: m.volume_backup_trigger(), sortable: true, cell: cells.trigger },
		{ accessorKey: 'destination', title: m.backups_destination_label(), sortable: true, cell: cells.destination },
		{ accessorKey: 'size', title: m.common_size(), sortable: true, cell: cells.size }
	];
}

export function backupRunMobileFields() {
	return [
		{ id: 'type', label: m.common_type(), defaultVisible: true },
		{ id: 'status', label: m.common_status(), defaultVisible: true },
		{ id: 'trigger', label: m.volume_backup_trigger(), defaultVisible: true },
		{ id: 'destination', label: m.backups_destination_label(), defaultVisible: true },
		{ id: 'size', label: m.common_size(), defaultVisible: true }
	];
}
