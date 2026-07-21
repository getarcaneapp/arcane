export type SystemBackupDestination = 'local' | 's3' | 'local_s3';

export type SystemBackupRun = {
	id: string;
	size: number;
	createdAt: string;
	status: 'running' | 'succeeded' | 'failed';
	trigger: 'manual' | 'scheduled' | 'safety';
	destination: SystemBackupDestination;
	localSnapshotId?: string;
	remoteSnapshotId?: string;
	s3DestinationId?: string;
	s3DestinationName?: string;
	policyId?: string;
	error?: string;
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

export type CreateSystemBackup = {
	destination?: SystemBackupDestination;
	s3DestinationId?: string;
	recoveryKey?: string;
	policyId?: string;
};
