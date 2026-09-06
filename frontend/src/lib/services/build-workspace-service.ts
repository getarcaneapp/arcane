import BaseAPIService from './api-service';
import { uploadService, type UploadProgressCallback } from './upload-service';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import type { FileEntry, FileContentResponse } from '#lib/types/shared.js';
import { downloadBlob, filenameFromPath } from '#lib/utils/browser-download.js';

class BuildWorkspaceService extends BaseAPIService {
	async listDirectory(path: string = '/'): Promise<FileEntry[]> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const res = await this.api.get(`/environments/${envId}/builds/browse`, {
			params: { path }
		});
		return res.data.data;
	}

	async getFileContent(path: string): Promise<FileContentResponse> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const res = await this.api.get(`/environments/${envId}/builds/browse/content`, {
			params: { path }
		});
		return res.data.data;
	}

	async downloadFile(path: string): Promise<void> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const res = await this.api.get(`/environments/${envId}/builds/browse/download`, {
			params: { path },
			responseType: 'blob'
		});

		downloadBlob(res.data, filenameFromPath(path));
	}

	async uploadFile(path: string, file: File, onProgress?: UploadProgressCallback): Promise<void> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const uploadId = await uploadService.uploadFile(envId, 'build-workspace', file, onProgress);
		return this.handleResponse(
			this.api.post(
				`/environments/${envId}/builds/browse/upload`,
				{ uploadId },
				{
					params: { path }
				}
			)
		);
	}

	async createDirectory(path: string): Promise<void> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(
			this.api.post(`/environments/${envId}/builds/browse/mkdir`, null, {
				params: { path }
			})
		);
	}

	async deleteFile(path: string): Promise<void> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(
			this.api.delete(`/environments/${envId}/builds/browse`, {
				params: { path }
			})
		);
	}
}

export const buildWorkspaceService = new BuildWorkspaceService();
