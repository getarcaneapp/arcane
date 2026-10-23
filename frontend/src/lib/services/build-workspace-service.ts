import type { FileEntry, FileContentResponse } from '#lib/types/shared.js';
import { downloadBlob, filenameFromPath } from '#lib/utils/browser-download.js';

import BaseAPIService from './api-service';
import { uploadService, type UploadProgressCallback } from './upload-service';

class BuildWorkspaceService extends BaseAPIService {
	async listDirectory(path: string = '/', environmentId: string): Promise<FileEntry[]> {
		const res = await this.api.get(`/environments/${environmentId}/builds/browse`, {
			params: { path }
		});
		return res.data.data;
	}

	async getFileContent(path: string, environmentId: string): Promise<FileContentResponse> {
		const res = await this.api.get(`/environments/${environmentId}/builds/browse/content`, {
			params: { path }
		});
		return res.data.data;
	}

	async downloadFile(path: string, environmentId: string): Promise<void> {
		const res = await this.api.get(`/environments/${environmentId}/builds/browse/download`, {
			params: { path },
			responseType: 'blob'
		});

		downloadBlob(res.data, filenameFromPath(path));
	}

	async uploadFile(
		path: string,
		file: File,
		onProgress: UploadProgressCallback | undefined,
		environmentId: string
	): Promise<void> {
		const uploadId = await uploadService.uploadFile(environmentId, 'build-workspace', file, onProgress);
		return this.handleResponse(
			this.api.post(
				`/environments/${environmentId}/builds/browse/upload`,
				{ uploadId },
				{
					params: { path }
				}
			)
		);
	}

	async createDirectory(path: string, environmentId: string): Promise<void> {
		return this.handleResponse(
			this.api.post(`/environments/${environmentId}/builds/browse/mkdir`, null, {
				params: { path }
			})
		);
	}

	async deleteFile(path: string, environmentId: string): Promise<void> {
		return this.handleResponse(
			this.api.delete(`/environments/${environmentId}/builds/browse`, {
				params: { path }
			})
		);
	}
}

export const buildWorkspaceService = new BuildWorkspaceService();
