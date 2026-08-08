import BaseAPIService from './api-service';
import { environmentStore } from '#lib/stores/environment.store.svelte';
import type {
	BackupEntry,
	CreateVolumeBackupRequest,
	UpdateVolumeBackupPolicy,
	VolumeBackupPolicyCollection
} from '#lib/types/shared';
import type { SearchPaginationSortRequest, Paginated } from '#lib/types/shared';
import { transformPaginationParams } from '#lib/utils/tables';

export type VolumeBackupListResponse = Paginated<BackupEntry> & { warnings?: string[] };

class VolumeBackupService extends BaseAPIService {
	async getPolicies(volumeName: string): Promise<VolumeBackupPolicyCollection> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.get(`/environments/${envId}/volumes/${volumeName}/backup-policy`));
	}

	async updatePolicies(volumeName: string, policies: UpdateVolumeBackupPolicy[]): Promise<VolumeBackupPolicyCollection> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.put(`/environments/${envId}/volumes/${volumeName}/backup-policy`, { policies }));
	}

	async createBackup(volumeName: string, request?: CreateVolumeBackupRequest): Promise<BackupEntry> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const res = await this.api.post(`/environments/${envId}/volumes/${volumeName}/backups`, request);
		return res.data.data;
	}

	async listBackups(volumeName: string, options?: SearchPaginationSortRequest): Promise<VolumeBackupListResponse> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const params = transformPaginationParams(options);
		const res = await this.api.get(`/environments/${envId}/volumes/${volumeName}/backups`, { params });
		return res.data;
	}

	async restoreBackup(volumeName: string, backupId: string): Promise<any> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.post(`/environments/${envId}/volumes/${volumeName}/backups/${backupId}/restore`));
	}

	async restoreBackupFiles(volumeName: string, backupId: string, paths: string[]): Promise<any> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(
			this.api.post(`/environments/${envId}/volumes/${volumeName}/backups/${backupId}/restore-files`, {
				paths
			})
		);
	}

	async backupHasPath(backupId: string, filePath: string): Promise<boolean> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const res = await this.api.get(`/environments/${envId}/volumes/backups/${backupId}/has-path`, {
			params: { path: filePath }
		});
		return !!res.data.data?.exists;
	}

	async listBackupFiles(backupId: string): Promise<string[]> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const res = await this.api.get(`/environments/${envId}/volumes/backups/${backupId}/files`);
		return res.data.data ?? [];
	}

	async downloadBackup(backupId: string): Promise<void> {
		const envId = await environmentStore.getCurrentEnvironmentId();
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

	async deleteBackup(backupId: string): Promise<any> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.delete(`/environments/${envId}/volumes/backups/${backupId}`));
	}

	async uploadBackup(backupId: string, s3DestinationId: string): Promise<BackupEntry> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.post(`/environments/${envId}/volumes/backups/${backupId}/upload`, { s3DestinationId }));
	}
}

export const volumeBackupService = new VolumeBackupService();
