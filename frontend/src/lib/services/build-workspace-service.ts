import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import type { FileEntry, FileContentResponse } from '#lib/types/shared.js';
import { downloadBlob, filenameFromPath } from '#lib/utils/browser-download.js';

import BaseAPIService from './api-service';
import { uploadService, type UploadProgressCallback } from './upload-service';

class BuildWorkspaceService extends BaseAPIService {
	private async resolveEnvironmentId(environmentId?: string): Promise<string> {
		return environmentId ?? (await environmentStore.getCurrentEnvironmentId());
	}

	async listDirectory(path: string = '/', environmentId?: string): Promise<FileEntry[]> {
		const envId = await this.resolveEnvironmentId(environmentId);
		const res = await this.api.get(`/environments/${envId}/builds/browse`, {
			params: { path }
		});
		return res.data.data;
	}

	async getFileContent(path: string, environmentId?: string): Promise<FileContentResponse> {
		const envId = await this.resolveEnvironmentId(environmentId);
		const res = await this.api.get(`/environments/${envId}/builds/browse/content`, {
			params: { path }
		});
		return res.data.data;
	}

	async downloadFile(path: string, environmentId?: string): Promise<void> {
		const envId = await this.resolveEnvironmentId(environmentId);
		const res = await this.api.get(`/environments/${envId}/builds/browse/download`, {
			params: { path },
			responseType: 'blob'
		});

		downloadBlob(res.data, filenameFromPath(path));
	}

	async uploadFile(path: string, file: File, onProgress?: UploadProgressCallback, environmentId?: string): Promise<void> {
		const envId = await this.resolveEnvironmentId(environmentId);
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

	async createDirectory(path: string, environmentId?: string): Promise<void> {
		const envId = await this.resolveEnvironmentId(environmentId);
		return this.handleResponse(
			this.api.post(`/environments/${envId}/builds/browse/mkdir`, null, {
				params: { path }
			})
		);
	}

	async deleteFile(path: string, environmentId?: string): Promise<void> {
		const envId = await this.resolveEnvironmentId(environmentId);
		return this.handleResponse(
			this.api.delete(`/environments/${envId}/builds/browse`, {
				params: { path }
			})
		);
	}
}

export const buildWorkspaceService = new BuildWorkspaceService();
