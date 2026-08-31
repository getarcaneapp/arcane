import { m } from '#lib/paraglide/messages';
import type {
	BackupDestination,
	BackupManagementType,
	BackupPolicy,
	BackupPolicyUpdate,
	BackupRun,
	BackupStatus,
	BackupTrigger
} from '#lib/types/backup';
import type { S3Destination } from '#lib/types/s3-destination';

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
	return m.backups_trigger_manual();
}

export function backupManagementLabel(type?: BackupManagementType): string {
	return type === 'system' ? m.backups_system_managed() : m.backups_volume_managed();
}

export function backupManagementFilterOptions() {
	return [
		{ label: m.backups_system_managed(), value: 'system' },
		{ label: m.backups_volume_managed(), value: 'volume' }
	];
}

export function backupDestinationLabel(destination: BackupDestination): string {
	if (destination === 'local_s3') return m.backups_destination_local_s3();
	if (destination === 's3') return m.backups_destination_s3();
	return m.local();
}

export function backupDestinationFromFlags(localEnabled: boolean, s3Enabled: boolean): BackupDestination {
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
	return [
		{
			label: m.volume_backup_destination_local(),
			value: 'local',
			description: descriptions ? m.volume_backup_destination_local_description() : undefined
		},
		...(hasS3Destinations
			? [
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
				]
			: [])
	];
}

export function s3DestinationOptions(destinations: S3Destination[]) {
	return destinations.map((item) => ({ label: item.name, value: item.id, description: item.bucket }));
}

export function backupPolicyDestinationValues(destination: BackupDestination, s3DestinationId: string) {
	return {
		localEnabled: destination !== 's3',
		s3Enabled: destination !== 'local',
		s3DestinationId: destination === 'local' ? '' : s3DestinationId
	};
}

export function backupPolicyUpdateFromPolicy(policy: BackupPolicy, includeStopContainers = false): BackupPolicyUpdate {
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
