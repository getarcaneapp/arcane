import type { BackupManagementType, BackupStatus, BackupTrigger } from './backup';
import type { WorkspaceFileChange } from './workspace';

export type VolumeWorkspaceFileChangeOperation =
	| 'create_file'
	| 'create_folder'
	| 'update_file'
	| 'rename'
	| 'move'
	| 'delete'
	| 'restore_file';

export interface VolumeWorkspaceFileChange extends Omit<WorkspaceFileChange, 'operation'> {
	operation: VolumeWorkspaceFileChangeOperation;
	backupId?: string;
}

export interface VolumeWorkspaceUpdateManifest {
	fileChanges: VolumeWorkspaceFileChange[];
}

export type VolumeBackupDestination = 'local' | 's3' | 'local_s3';

export type VolumeBackupFormat = 'archive' | 'rustic';

export interface CreateVolumeBackupRequest {
	destination?: VolumeBackupDestination;
	policyId?: string;
	s3DestinationId?: string;
}

export interface DiscoverVolumeBackupsResponse {
	errors?: string[];
	count: number;
}

export interface VolumeBackupEntry {
	remoteAvailable?: boolean;
	activityId?: string;
	id: string;
	volumeName: string;
	createdAt: string;
	status: BackupStatus;
	trigger: BackupTrigger;
	destination: VolumeBackupDestination;
	format: VolumeBackupFormat;
	localSnapshotId?: string;
	remoteSnapshotId?: string;
	s3DestinationId?: string;
	s3DestinationName?: string;
	policyId?: string;
	error?: string;
	type: BackupManagementType;
	size: number;
}

export interface VolumeBackupPolicy {
	lastRun?: VolumeBackupEntry;
	id: string;
	volumeName: string;
	schedule: string;
	s3DestinationId?: string;
	s3DestinationName?: string;
	s3Bucket?: string;
	retentionCount: number;
	enabled: boolean;
	stopContainers: boolean;
	localEnabled: boolean;
	s3Enabled: boolean;
	s3Available: boolean;
}

export interface VolumeBackupPolicyCollection {
	policies: VolumeBackupPolicy[];
	s3Available: boolean;
}

export interface Volume {
	options: Record<string, string>;
	labels: Record<string, string>;
	activityId?: string;
	id: string;
	name: string;
	driver: string;
	mountpoint: string;
	scope: string;
	createdAt: string;
	containers: string[];
	size: number;
	inUse: boolean;
}

export interface VolumeUsageCounts {
	inuse: number;
	unused: number;
	total: number;
}

export interface VolumeRename {
	name: string;
}

export interface VolumeUsageResponse {
	containers: string[];
	inUse: boolean;
}

export interface VolumeSizeInfo {
	name: string;
	size: number;
	refCount: number;
}
