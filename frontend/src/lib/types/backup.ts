export type BackupStatus = 'running' | 'succeeded' | 'failed';
export type BackupTrigger = 'manual' | 'scheduled' | 'safety' | 'manager';

export type BackupFileEntry = {
	path: string;
	name: string;
	isDirectory: boolean;
};

export type BackupRestoreSelection = {
	search?: string;
	paths?: string[];
	selectAll?: boolean;
};

export type S3Destination = {
	createdAt: string;
	updatedAt?: string;
	id: string;
	name: string;
	endpoint?: string;
	bucket: string;
	region: string;
	accessKeyId: string;
	secretAccessKey?: string;
	prefix?: string;
	useSsl: boolean;
	forcePathStyle: boolean;
	secretConfigured: boolean;
};

export type SystemBackupDestination = 'local' | 's3' | 'local_s3';

export type SystemBackupRun = {
	createdAt: string;
	remoteAvailable?: boolean;
	activityId?: string;
	id: string;
	status: BackupStatus;
	trigger: BackupTrigger;
	destination: SystemBackupDestination;
	localSnapshotId?: string;
	remoteSnapshotId?: string;
	s3DestinationId?: string;
	s3DestinationName?: string;
	policyId?: string;
	error?: string;
	size: number;
};

export type SystemBackupPolicy = {
	lastRun?: SystemBackupRun;
	id: string;
	schedule: string;
	s3DestinationId?: string;
	s3DestinationName?: string;
	retentionCount: number;
	enabled: boolean;
	localEnabled: boolean;
	s3Enabled: boolean;
};

export type UpdateBackupPolicy = {
	id: string;
	schedule: string;
	s3DestinationId?: string;
	retentionCount: number;
	enabled: boolean;
	stopContainers?: boolean;
	localEnabled: boolean;
	s3Enabled: boolean;
};

export type SystemBackupPolicyCollection = {
	policies: SystemBackupPolicy[];
	recoveryKeyStored: boolean;
};

export type SystemBackupRecoveryKey = {
	recoveryKey: string;
};

export type CreateSystemBackupRequest = {
	destination?: SystemBackupDestination;
	s3DestinationId?: string;
	recoveryKey?: string;
	policyId?: string;
};

export type BackupManagementType = 'system' | 'volume';

export type SystemVolumeSelectionMode = 'all' | 'allowlist' | 'blocklist';

export type SystemVolumeBackupPolicy = {
	lastRun?: SystemBackupRun;
	id: string;
	schedule: string;
	s3DestinationId?: string;
	s3DestinationName?: string;
	selectionMode: SystemVolumeSelectionMode;
	volumeNames: string[];
	retentionCount: number;
	enabled: boolean;
	stopContainers: boolean;
	localEnabled: boolean;
	s3Enabled: boolean;
	ignoreAnonymous: boolean;
};

export type UpdateSystemVolumeBackupPolicy = UpdateBackupPolicy & {
	selectionMode: SystemVolumeSelectionMode;
	volumeNames: string[];
	ignoreAnonymous: boolean;
};

export type SystemVolumeBackupPolicyCollection = {
	policies: SystemVolumeBackupPolicy[];
};

export type SystemVolumeBackupCustomRun = {
	destination: SystemBackupDestination;
	s3DestinationId?: string;
	selectionMode: SystemVolumeSelectionMode;
	volumeNames: string[];
	stopContainers: boolean;
	ignoreAnonymous: boolean;
};

export type RunSystemVolumeBackupsRequest = {
	custom?: SystemVolumeBackupCustomRun;
	policyId?: string;
};

export type SystemVolumeBackupOption = {
	name: string;
	anonymous: boolean;
	available: boolean;
};

export type SystemVolumeBackupHistoryEntry = {
	environmentId?: string;
	environmentName?: string;
	createdAt: string;
	remoteAvailable?: boolean;
	id: string;
	status: BackupStatus;
	trigger: BackupTrigger;
	destination: SystemBackupDestination;
	format?: 'archive' | 'rustic';
	localSnapshotId?: string;
	remoteSnapshotId?: string;
	s3DestinationId?: string;
	s3DestinationName?: string;
	policyId?: string;
	error?: string;
	type: BackupManagementType;
	resourceType: string;
	resourceName: string;
	size: number;
};

export type BackupRunAccepted = {
	activityId: string;
	status: string;
};
