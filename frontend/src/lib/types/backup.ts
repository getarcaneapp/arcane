export type BackupDestination = 'local' | 's3' | 'local_s3';
export type BackupStatus = 'running' | 'succeeded' | 'failed';
export type BackupTrigger = 'manual' | 'scheduled' | 'safety';

export type BackupRun = {
	id: string;
	size: number;
	createdAt: string;
	status: BackupStatus;
	trigger: BackupTrigger;
	destination: BackupDestination;
	localSnapshotId?: string;
	remoteSnapshotId?: string;
	s3DestinationId?: string;
	s3DestinationName?: string;
	policyId?: string;
	error?: string;
};

export type BackupPolicyForm = {
	enabled: boolean;
	schedule: string;
	retentionCount: number;
	destination: BackupDestination;
	s3DestinationId: string;
	stopContainers?: boolean;
};
