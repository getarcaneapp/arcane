import type { PaginatedWithCounts, PaginatedWithWarnings } from '../base';
import type { Volume, VolumeBackupEntry, VolumeUsageCounts } from '../volume';
import type { WorkspaceFileContent, WorkspaceReadOnlyReason } from '../workspace';
import type { CreateRequest } from './pagination';

export interface VolumeWorkspaceFileContent extends Omit<WorkspaceFileContent, 'readOnlyReason'> {
	readOnlyReason?: WorkspaceReadOnlyReason | 'restore_pending';
}

export type VolumeWritable = 'name' | 'driver' | 'options' | 'labels';

export type VolumeCreate = CreateRequest<Volume, VolumeWritable, 'name'>;

export type VolumesPaginatedResponse = PaginatedWithCounts<Volume, VolumeUsageCounts>;

export type VolumeBackupListResponse = PaginatedWithWarnings<VolumeBackupEntry>;
