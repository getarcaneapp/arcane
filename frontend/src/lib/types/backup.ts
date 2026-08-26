import type { Paginated } from './shared';

export type BackupDestination = 'local' | 's3' | 'local_s3';
export type BackupStatus = 'running' | 'succeeded' | 'failed';
export type BackupTrigger = 'manual' | 'scheduled' | 'safety';
export type BackupManagementType = 'system' | 'volume';

export type BackupRun = {
	id: string;
	size: number;
	createdAt: string;
	status: BackupStatus;
	trigger: BackupTrigger;
	destination: BackupDestination;
	format?: 'archive' | 'rustic';
	localSnapshotId?: string;
	remoteSnapshotId?: string;
	s3DestinationId?: string;
	s3DestinationName?: string;
	policyId?: string;
	error?: string;
	type?: BackupManagementType;
};

export type BackupPolicyForm = {
	enabled: boolean;
	schedule: string;
	retentionCount: number;
	destination: BackupDestination;
	s3DestinationId: string;
	stopContainers?: boolean;
};

export type BackupPolicy = {
	id: string;
	enabled: boolean;
	schedule: string;
	retentionCount: number;
	stopContainers?: boolean;
	localEnabled: boolean;
	s3Enabled: boolean;
	s3DestinationId?: string;
};

export type BackupPolicyUpdate = {
	id: string;
	enabled: boolean;
	schedule: string;
	retentionCount: number;
	localEnabled: boolean;
	s3Enabled: boolean;
	s3DestinationId: string;
	stopContainers?: boolean;
};

export type BackupFileEntry = {
	path: string;
	name: string;
	isDirectory: boolean;
};

export type BackupFileBrowseRequest = {
	path?: string;
	search?: string;
	start?: number;
	limit?: number;
};

export type BackupRestoreSelection = {
	paths: string[];
	selectAll: boolean;
	search?: string;
};

export type BackupFileProvider = {
	browse(request: BackupFileBrowseRequest): Promise<Paginated<BackupFileEntry>>;
};
