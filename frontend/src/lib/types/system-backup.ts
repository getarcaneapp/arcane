import type { BackupDestination, BackupManagementType, BackupPolicy, BackupRun } from './backup';

export type SystemBackupDestination = BackupDestination;

export type SystemBackupRun = BackupRun;

export type BackupHistoryEntry = BackupRun & {
	type: BackupManagementType;
	resourceType: 'system' | 'volume';
	resourceName: string;
};

export type SystemVolumeBackupSelectionMode = 'all' | 'allowlist' | 'blocklist';

export type SystemVolumeBackupPolicy = BackupPolicy & {
	s3DestinationName?: string;
	selectionMode: SystemVolumeBackupSelectionMode;
	volumeNames: string[];
	ignoreAnonymous: boolean;
	lastRun?: SystemBackupRun;
};

export type UpdateSystemVolumeBackupPolicy = Omit<SystemVolumeBackupPolicy, 's3DestinationName' | 'lastRun'> & {
	s3DestinationId: string;
};

export type SystemVolumeBackupPolicyCollection = {
	policies: SystemVolumeBackupPolicy[];
};

export type SystemVolumeBackupOption = {
	name: string;
	anonymous: boolean;
	available: boolean;
};

export type SystemVolumeBackupRunResult = {
	activityId: string;
	status: 'running';
};

export type SystemVolumeBackupCustomRun = {
	destination: BackupDestination;
	s3DestinationId?: string;
	stopContainers: boolean;
	selectionMode: SystemVolumeBackupSelectionMode;
	volumeNames: string[];
	ignoreAnonymous: boolean;
};

export type RunSystemVolumeBackups = {
	policyId?: string;
	custom?: SystemVolumeBackupCustomRun;
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
