import type {
	BackupFileEntry,
	BackupManagementType,
	BackupStatus,
	BackupTrigger,
	S3Destination,
	SystemBackupDestination
} from '../backup';
import type { Paginated } from '../base';
import type { CreateRequest, UpdateRequest } from './pagination';

export type BackupRun = {
	remoteAvailable?: boolean;
	activityId?: string;
	id: string;
	size: number;
	createdAt: string;
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
	type?: BackupManagementType;
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

export type BackupPolicyForm = {
	enabled: boolean;
	schedule: string;
	retentionCount: number;
	destination: SystemBackupDestination;
	s3DestinationId: string;
	stopContainers?: boolean;
};

export type BackupFileBrowseRequest = {
	path?: string;
	search?: string;
	start?: number;
	limit?: number;
};

export type BackupFileProvider = {
	browse(request: BackupFileBrowseRequest): Promise<Paginated<BackupFileEntry>>;
};

export type BackupFileRootLoadState = 'loading' | 'ready' | 'error';

export type S3DestinationWritable =
	| 'name'
	| 'endpoint'
	| 'bucket'
	| 'region'
	| 'accessKeyId'
	| 'secretAccessKey'
	| 'prefix'
	| 'useSsl'
	| 'forcePathStyle';

export type S3DestinationCreate = CreateRequest<S3Destination, S3DestinationWritable, 'name' | 'bucket' | 'accessKeyId'>;

export type S3DestinationUpdate = UpdateRequest<S3Destination, S3DestinationWritable>;
