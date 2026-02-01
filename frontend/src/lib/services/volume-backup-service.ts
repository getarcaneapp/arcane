import BaseAPIService from './api-service';
import { environmentStore } from '$lib/stores/environment.store.svelte';
import type { BackupEntry } from '$lib/types/file-browser.type';
import type { SearchPaginationSortRequest, Paginated } from '$lib/types/pagination.type';
import { transformPaginationParams } from '$lib/utils/params.util';

export type VolumeBackupListResponse = Paginated<BackupEntry> & { warnings?: string[] };

export class VolumeBackupService extends BaseAPIService {
	async createBackup(volumeName: string): Promise<BackupEntry> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const res = await this.api.post(`/environments/${envId}/volumes/${volumeName}/backups`);
		return res.data.data;
	}

	async listBackups(volumeName: string, options?: SearchPaginationSortRequest): Promise<VolumeBackupListResponse> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const params = transformPaginationParams(options);
		const res = await this.api.get(`/environments/${envId}/volumes/${volumeName}/backups`, { params });
		return res.data;
	}

	async restoreBackup(volumeName: string, backupId: string): Promise<void> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.post(`/environments/${envId}/volumes/${volumeName}/backups/${backupId}/restore`));
	}

	async restoreBackupFiles(volumeName: string, backupId: string, paths: string[]): Promise<void> {
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

	async deleteBackup(backupId: string): Promise<void> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.delete(`/environments/${envId}/volumes/backups/${backupId}`));
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

	async uploadAndRestore(volumeName: string, file: File): Promise<void> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const formData = new FormData();
		formData.append('file', file);
		return this.handleResponse(
			this.api.post(`/environments/${envId}/volumes/${volumeName}/backups/upload`, formData, {
				headers: {
					'Content-Type': 'multipart/form-data'
				}
			})
		);
	}
}

export const volumeBackupService = new VolumeBackupService();
