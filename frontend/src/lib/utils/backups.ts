import { toast } from 'svelte-sonner';

import { m } from '#lib/paraglide/messages.js';
import { systemBackupService } from '#lib/services/system-backup-service.js';
import { volumeBackupService } from '#lib/services/volume-backup-service.js';
import { LOCAL_DOCKER_ENVIRONMENT_ID } from '#lib/stores/environment.store.svelte.js';
import type {
	SystemBackupDestination,
	BackupManagementType,
	UpdateBackupPolicy,
	BackupStatus,
	BackupTrigger,
	S3Destination
} from '#lib/types/backup.js';
import type { BackupPolicy, BackupRun } from '#lib/types/ui/backup.js';
import { extractApiErrorMessage } from '#lib/utils/api.js';
import { tryCatch } from '#lib/utils/try-catch.js';

export function backupStatusLabel(status: BackupStatus): string {
	if (status === 'succeeded') return m.volume_backup_status_succeeded();
	if (status === 'failed') return m.common_failed();
	return m.common_running();
}

export function backupStatusVariant(status: BackupStatus): 'green' | 'red' | 'blue' {
	if (status === 'succeeded') return 'green';
	if (status === 'failed') return 'red';
	return 'blue';
}

export function backupTriggerLabel(trigger: BackupTrigger): string {
	if (trigger === 'scheduled') return m.backups_trigger_scheduled();
	if (trigger === 'safety') return m.backups_trigger_safety();
	if (trigger === 'manager') return m.manager();
	return m.backups_trigger_manual();
}

// System is only ever Arcane's own backups; the global project and volume policies' backups are projects' and volumes'.
export function backupManagementLabel(type?: BackupManagementType): string {
	if (type === 'system') return m.system();
	if (type === 'project') return m.project();
	return m.resource_volume_cap();
}

export function backupManagementFilterOptions() {
	return [
		{ label: m.system(), value: 'system' },
		{ label: m.project(), value: 'project' },
		{ label: m.resource_volume_cap(), value: 'volume' }
	];
}

export function backupDestinationLabel(destination: SystemBackupDestination): string {
	if (destination === 'local_s3') return m.backups_destination_local_s3();
	if (destination === 's3') return m.backups_destination_s3();
	return m.local();
}

export function backupDestinationFromFlags(localEnabled: boolean, s3Enabled: boolean): SystemBackupDestination {
	if (!s3Enabled) return 'local';
	return localEnabled ? 'local_s3' : 's3';
}

export function backupDestinationName(item: Pick<BackupRun, 's3DestinationId' | 's3DestinationName'>): string {
	return item.s3DestinationName || item.s3DestinationId || '';
}

export function backupDestinationDisplay(item: Pick<BackupRun, 'destination' | 's3DestinationId' | 's3DestinationName'>): string {
	const label = backupDestinationLabel(item.destination);
	const name = backupDestinationName(item);
	return name && item.destination !== 'local' ? `${label} · ${name}` : label;
}

export function backupPolicyDestinationDisplay(policy: {
	localEnabled: boolean;
	s3Enabled: boolean;
	s3DestinationName?: string;
}): string {
	const label = backupDestinationLabel(backupDestinationFromFlags(policy.localEnabled, policy.s3Enabled));
	return policy.s3DestinationName ? `${label} · ${policy.s3DestinationName}` : label;
}

export function backupDestinationOptions(hasS3Destinations: boolean, descriptions = false) {
	const local = {
		label: m.volume_backup_destination_local(),
		value: 'local',
		description: descriptions ? m.volume_backup_destination_local_description() : undefined
	};
	if (!hasS3Destinations) return [local];
	return [
		local,
		{
			label: m.volume_backup_destination_s3(),
			value: 's3',
			description: descriptions ? m.volume_backup_destination_s3_description() : undefined
		},
		{
			label: m.backups_destination_local_s3(),
			value: 'local_s3',
			description: descriptions ? m.volume_backup_destination_local_s3_description() : undefined
		}
	];
}

export function s3DestinationOptions(destinations: S3Destination[]) {
	return destinations.map((item) => ({ label: item.name, value: item.id, description: item.bucket }));
}

export function backupPolicyDestinationValues(destination: SystemBackupDestination, s3DestinationId: string) {
	return {
		localEnabled: destination !== 's3',
		s3Enabled: destination !== 'local',
		s3DestinationId: destination === 'local' ? '' : s3DestinationId
	};
}

export function backupPolicyUpdateFromPolicy(policy: BackupPolicy, includeStopContainers = false): UpdateBackupPolicy {
	return {
		id: policy.id,
		enabled: policy.enabled,
		schedule: policy.schedule,
		retentionCount: policy.retentionCount,
		localEnabled: policy.localEnabled,
		s3Enabled: policy.s3Enabled,
		s3DestinationId: policy.s3DestinationId ?? '',
		...(includeStopContainers ? { stopContainers: policy.stopContainers ?? false } : {})
	};
}

export async function discoverDestinationBackups(destinationId: string): Promise<void> {
	const policiesResult = await tryCatch(systemBackupService.getPolicies());
	if (policiesResult.error !== null || !policiesResult.data.recoveryKeyStored) return;
	const systemResult = await tryCatch(systemBackupService.discover(destinationId, ''));
	if (systemResult.error === null && systemResult.data > 0) {
		toast.success(m.system_backups_discovered({ count: systemResult.data }));
	}
	const projectResult = await tryCatch(systemBackupService.discoverProjectBackups(destinationId));
	if (projectResult.error !== null) {
		toast.error(m.failed_to_discover_project_backups(), { description: extractApiErrorMessage(projectResult.error) });
	} else if (projectResult.data > 0) {
		toast.success(m.found_project_backups({ count: projectResult.data }));
	}
	const volumeResult = await tryCatch(volumeBackupService.discoverBackups(destinationId, LOCAL_DOCKER_ENVIRONMENT_ID));
	if (volumeResult.error === null) {
		if (volumeResult.data.count > 0) {
			toast.success(m.volume_backups_discovered({ count: volumeResult.data.count }));
		}
		for (const failure of volumeResult.data.errors ?? []) {
			toast.warning(m.volume_backups_discover_failed(), { description: failure });
		}
	} else {
		toast.error(volumeResult.error instanceof Error ? volumeResult.error.message : m.volume_backups_discover_failed());
	}
}

// Finds backups whose rows are missing on each S3 destination, and in local storage when `local` is set, such as after
// a recovery key is imported following the loss of Arcane's data volume.
export async function runAutomaticBackupDiscovery(
	destinations: { id: string }[],
	{ local }: { local: boolean }
): Promise<boolean> {
	const discover = (found: Promise<number>, failed: () => string) =>
		found.catch((error: unknown) => {
			toast.error(failed(), { description: extractApiErrorMessage(error) });
			return 0;
		});
	const counts = await Promise.all([
		...(local ? [discover(systemBackupService.discoverProjectBackups('', 'local'), m.failed_to_discover_project_backups)] : []),
		...destinations.flatMap((item) => [
			discover(systemBackupService.discover(item.id, ''), m.system_backups_discover_failed),
			discover(systemBackupService.discoverProjectBackups(item.id), m.failed_to_discover_project_backups),
			discover(
				volumeBackupService.discoverBackups(item.id, LOCAL_DOCKER_ENVIRONMENT_ID).then((result) => result.count),
				m.volume_backups_discover_failed
			)
		])
	]);
	return counts.some((count) => count > 0);
}
