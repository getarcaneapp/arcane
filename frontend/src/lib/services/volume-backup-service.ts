import type { BackupFileEntry, BackupRestoreSelection, UpdateBackupPolicy } from '#lib/types/backup.js';
import type { Paginated, MessageResponse } from '#lib/types/base.js';
import type { BackupFileBrowseRequest } from '#lib/types/ui/backup.js';
import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
import type { UploadProgressCallback } from '#lib/types/ui/upload.js';
import type { VolumeBackupListResponse } from '#lib/types/ui/volume.js';
import type {
	VolumeBackupEntry,
	CreateVolumeBackupRequest,
	DiscoverVolumeBackupsResponse,
	VolumeBackupPolicyCollection
} from '#lib/types/volume.js';
import { transformPaginationParams } from '#lib/utils/tables.js';

import BaseAPIService from './api-service';
import { uploadService } from './upload-service';

class VolumeBackupService extends BaseAPIService {
	async getPolicies(volumeName: string, environmentId: string): Promise<VolumeBackupPolicyCollection> {
		return this.handleResponse(this.api.get(`/environments/${environmentId}/volumes/${volumeName}/backups/policy`));
	}

	async updatePolicies(
		volumeName: string,
		policies: UpdateBackupPolicy[],
		environmentId: string
	): Promise<VolumeBackupPolicyCollection> {
		return this.handleResponse(this.api.put(`/environments/${environmentId}/volumes/${volumeName}/backups/policy`, { policies }));
	}

	async createBackup(
		volumeName: string,
		request: CreateVolumeBackupRequest | undefined,
		environmentId: string
	): Promise<VolumeBackupEntry> {
		const res = await this.api.post(`/environments/${environmentId}/volumes/${volumeName}/backups`, request);
		return res.data.data;
	}

	async listBackups(
		volumeName: string,
		options: SearchPaginationSortRequest | undefined,
		environmentId: string
	): Promise<VolumeBackupListResponse> {
		const params = transformPaginationParams(options);
		const res = await this.api.get(`/environments/${environmentId}/volumes/${volumeName}/backups`, { params });
		return res.data;
	}

	async discoverBackups(s3DestinationId: string, environmentId: string): Promise<DiscoverVolumeBackupsResponse> {
		return this.handleResponse(this.api.post(`/environments/${environmentId}/volumes/backups/discover`, { s3DestinationId }));
	}

	async restoreBackup(volumeName: string, backupId: string, environmentId: string): Promise<MessageResponse> {
		return this.handleResponse(
			this.api.post(`/environments/${environmentId}/volumes/${volumeName}/backups/${backupId}`, undefined, {
				params: { action: 'restore' }
			})
		);
	}

	async restoreBackupFiles(
		volumeName: string,
		backupId: string,
		selection: BackupRestoreSelection,
		environmentId: string
	): Promise<MessageResponse> {
		return this.handleResponse(
			this.api.post(
				`/environments/${environmentId}/volumes/${volumeName}/backups/${backupId}`,
				{ ...selection },
				{ params: { action: 'restorefiles' } }
			)
		);
	}

	async browseBackupFiles(
		backupId: string,
		request: BackupFileBrowseRequest,
		environmentId: string
	): Promise<Paginated<BackupFileEntry>> {
		const response = await this.api.get(`/environments/${environmentId}/volumes/backups/${backupId}/files/browse`, {
			params: request
		});
		return response.data;
	}

	async backupHasPath(backupId: string, filePath: string, environmentId: string): Promise<boolean> {
		const res = await this.api.get(`/environments/${environmentId}/volumes/backups/${backupId}/files/exists`, {
			params: { path: filePath }
		});
		return !!res.data.data?.exists;
	}

	async listBackupFiles(backupId: string, environmentId: string): Promise<string[]> {
		const res = await this.api.get(`/environments/${environmentId}/volumes/backups/${backupId}/files`);
		return res.data.data ?? [];
	}

	async downloadBackup(backupId: string, environmentId: string): Promise<void> {
		const res = await this.api.get(`/environments/${environmentId}/volumes/backups/${backupId}/download`, {
			responseType: 'blob'
		});

		const url = window.URL.createObjectURL(new Blob([res.data]));
		const link = document.createElement('a');
		link.href = url;
		link.setAttribute('download', `${backupId}.tar.gz`);
		document.body.appendChild(link);
		link.click();
		link.remove();
	}

	async deleteBackup(backupId: string, environmentId: string): Promise<MessageResponse> {
		return this.handleResponse(this.api.delete(`/environments/${environmentId}/volumes/backups/${backupId}`));
	}

	async uploadBackup(backupId: string, s3DestinationId: string, environmentId: string): Promise<VolumeBackupEntry> {
		return this.handleResponse(
			this.api.post(`/environments/${environmentId}/volumes/backups/${backupId}/upload`, { s3DestinationId })
		);
	}

	async uploadAndRestore(
		volumeName: string,
		file: File,
		environmentId: string,
		onProgress?: UploadProgressCallback
	): Promise<MessageResponse> {
		const uploadId = await uploadService.uploadFile(environmentId, 'volume-backup', file, onProgress);
		return this.handleResponse(
			this.api.post(`/environments/${environmentId}/volumes/${volumeName}/backups/upload`, { uploadId })
		);
	}
}

export const volumeBackupService = new VolumeBackupService();
