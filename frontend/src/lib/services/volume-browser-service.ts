import BaseAPIService from './api-service';
import { environmentStore } from '$lib/stores/environment.store.svelte';
import type { FileEntry, FileContentResponse } from '$lib/types/shared';
import { downloadBlob, filenameFromPath } from '$lib/utils/browser-download';

class VolumeBrowserService extends BaseAPIService {
	async listDirectory(volumeName: string, path: string = '/'): Promise<FileEntry[]> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const res = await this.api.get(`/environments/${envId}/volumes/${volumeName}/browse`, {
			params: { path }
		});
		return res.data.data;
	}

	async getFileContent(volumeName: string, path: string): Promise<FileContentResponse> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const res = await this.api.get(`/environments/${envId}/volumes/${volumeName}/browse/content`, {
			params: { path }
		});
		return res.data.data;
	}

	async downloadFile(volumeName: string, path: string): Promise<void> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const res = await this.api.get(`/environments/${envId}/volumes/${volumeName}/browse/download`, {
			params: { path },
			responseType: 'blob'
		});

		downloadBlob(res.data, filenameFromPath(path));
	}

	async uploadFile(volumeName: string, path: string, file: File): Promise<any> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const formData = new FormData();
		formData.append('file', file);
		return this.handleResponse(
			this.api.post(`/environments/${envId}/volumes/${volumeName}/browse/upload`, formData, {
				params: { path }
			})
		);
	}

	async createDirectory(volumeName: string, path: string): Promise<any> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(
			this.api.post(`/environments/${envId}/volumes/${volumeName}/browse/mkdir`, null, {
				params: { path }
			})
		);
	}

	async deleteFile(volumeName: string, path: string): Promise<any> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(
			this.api.delete(`/environments/${envId}/volumes/${volumeName}/browse`, {
				params: { path }
			})
		);
	}
}

export const volumeBrowserService = new VolumeBrowserService();
