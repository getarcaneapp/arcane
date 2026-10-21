import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import type { BackupFileBrowseRequest, BackupFileEntry, BackupRestoreSelection } from '#lib/types/backup.js';
import type {
	BackupEntry,
	CreateVolumeBackupRequest,
	DiscoverVolumeBackupsResponse,
	UpdateVolumeBackupPolicy,
	VolumeBackupPolicyCollection
} from '#lib/types/shared.js';
import type { SearchPaginationSortRequest, Paginated } from '#lib/types/shared.js';
import { transformPaginationParams } from '#lib/utils/tables.js';

import BaseAPIService from './api-service';
import { uploadService, type UploadProgressCallback } from './upload-service';

export type VolumeBackupListResponse = Paginated<BackupEntry> & { warnings?: string[] };

class VolumeBackupService extends BaseAPIService {
	private async resolveEnvironmentId(environmentId?: string): Promise<string> {
		return environmentId ?? (await environmentStore.getCurrentEnvironmentId());
	}

	async getPolicies(volumeName: string, environmentId?: string): Promise<VolumeBackupPolicyCollection> {
		const envId = await this.resolveEnvironmentId(environmentId);
		return this.handleResponse(this.api.get(`/environments/${envId}/volumes/${volumeName}/backups/policy`));
	}

	async updatePolicies(
		volumeName: string,
		policies: UpdateVolumeBackupPolicy[],
		environmentId?: string
	): Promise<VolumeBackupPolicyCollection> {
		const envId = await this.resolveEnvironmentId(environmentId);
		return this.handleResponse(this.api.put(`/environments/${envId}/volumes/${volumeName}/backups/policy`, { policies }));
	}

	async createBackup(volumeName: string, request?: CreateVolumeBackupRequest, environmentId?: string): Promise<BackupEntry> {
		const envId = await this.resolveEnvironmentId(environmentId);
		const res = await this.api.post(`/environments/${envId}/volumes/${volumeName}/backups`, request);
		return res.data.data;
	}

	async listBackups(
		volumeName: string,
		options?: SearchPaginationSortRequest,
		environmentId?: string
	): Promise<VolumeBackupListResponse> {
		const envId = await this.resolveEnvironmentId(environmentId);
		const params = transformPaginationParams(options);
		const res = await this.api.get(`/environments/${envId}/volumes/${volumeName}/backups`, { params });
		return res.data;
	}

	async discoverBackups(s3DestinationId: string): Promise<DiscoverVolumeBackupsResponse> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.post(`/environments/${envId}/volumes/backups/discover`, { s3DestinationId }));
	}

	async restoreBackup(volumeName: string, backupId: string, environmentId?: string): Promise<unknown> {
		const envId = await this.resolveEnvironmentId(environmentId);
		return this.handleResponse(
			this.api.post(`/environments/${envId}/volumes/${volumeName}/backups/${backupId}`, undefined, {
				params: { action: 'restore' }
			})
		);
	}

	async restoreBackupFiles(
		volumeName: string,
		backupId: string,
		selection: BackupRestoreSelection,
		environmentId?: string
	): Promise<unknown> {
		const envId = await this.resolveEnvironmentId(environmentId);
		return this.handleResponse(
			this.api.post(
				`/environments/${envId}/volumes/${volumeName}/backups/${backupId}`,
				{ ...selection },
				{ params: { action: 'restorefiles' } }
			)
		);
	}

	async browseBackupFiles(
		backupId: string,
		request: BackupFileBrowseRequest,
		environmentId?: string
	): Promise<Paginated<BackupFileEntry>> {
		const envId = await this.resolveEnvironmentId(environmentId);
		const response = await this.api.get(`/environments/${envId}/volumes/backups/${backupId}/files/browse`, {
			params: request
		});
		return response.data;
	}

	async backupHasPath(backupId: string, filePath: string, environmentId?: string): Promise<boolean> {
		const envId = await this.resolveEnvironmentId(environmentId);
		const res = await this.api.get(`/environments/${envId}/volumes/backups/${backupId}/files/exists`, {
			params: { path: filePath }
		});
		return !!res.data.data?.exists;
	}

	async listBackupFiles(backupId: string): Promise<string[]> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const res = await this.api.get(`/environments/${envId}/volumes/backups/${backupId}/files`);
		return res.data.data ?? [];
	}

	async downloadBackup(backupId: string, environmentId?: string): Promise<void> {
		const envId = await this.resolveEnvironmentId(environmentId);
		const res = await this.api.get(`/environments/${envId}/volumes/backups/${backupId}/download`, {
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

	async deleteBackup(backupId: string, environmentId?: string): Promise<unknown> {
		const envId = await this.resolveEnvironmentId(environmentId);
		return this.handleResponse(this.api.delete(`/environments/${envId}/volumes/backups/${backupId}`));
	}

	async uploadBackup(backupId: string, s3DestinationId: string, environmentId?: string): Promise<BackupEntry> {
		const envId = await this.resolveEnvironmentId(environmentId);
		return this.handleResponse(this.api.post(`/environments/${envId}/volumes/backups/${backupId}/upload`, { s3DestinationId }));
	}

	async uploadAndRestore(volumeName: string, file: File, onProgress?: UploadProgressCallback): Promise<unknown> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const uploadId = await uploadService.uploadFile(envId, 'volume-backup', file, onProgress);
		return this.handleResponse(this.api.post(`/environments/${envId}/volumes/${volumeName}/backups/upload`, { uploadId }));
	}
}

export const volumeBackupService = new VolumeBackupService();
