import type { BackupDestination, BackupManagementType, BackupRun } from './backup';

export type SystemBackupDestination = BackupDestination;

export type SystemBackupRun = BackupRun;

export type BackupHistoryEntry = BackupRun & {
	type: BackupManagementType;
	resourceType: 'system' | 'volume';
	resourceName: string;
};

export type SystemVolumeBackupSelectionMode = 'all' | 'allowlist' | 'blocklist';

export type SystemVolumeBackupConfig = {
	enabled: boolean;
	schedule: string;
	retentionCount: number;
	stopContainers: boolean;
	localEnabled: boolean;
	s3Enabled: boolean;
	s3DestinationId?: string;
	s3DestinationName?: string;
	selectionMode: SystemVolumeBackupSelectionMode;
	volumeNames: string[];
	ignoreAnonymous: boolean;
};

export type SystemVolumeBackupOption = {
	name: string;
	anonymous: boolean;
	available: boolean;
};

export type SystemVolumeBackupRunResult = {
	matched: number;
	succeeded: number;
	failed: number;
	skipped: number;
	failures: { volumeName: string; error: string }[];
};

export type SystemBackupPolicy = {
	id: string;
	enabled: boolean;
	schedule: string;
	retentionCount: number;
	localEnabled: boolean;
	s3Enabled: boolean;
	s3DestinationId?: string;
	s3DestinationName?: string;
	lastRun?: SystemBackupRun;
};

export type UpdateSystemBackupPolicy = Omit<SystemBackupPolicy, 's3DestinationName' | 'lastRun'>;

export type SystemBackupPolicyCollection = {
	policies: SystemBackupPolicy[];
	recoveryKeyStored: boolean;
};

export type SystemBackupRecoveryKey = {
	recoveryKey: string;
};

export type CreateSystemBackup = {
	destination?: SystemBackupDestination;
	s3DestinationId?: string;
	recoveryKey?: string;
	policyId?: string;
};
