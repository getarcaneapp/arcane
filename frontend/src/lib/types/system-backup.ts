import type { BackupDestination, BackupRun } from './backup';

export type SystemBackupDestination = BackupDestination;

export type SystemBackupRun = BackupRun;

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
