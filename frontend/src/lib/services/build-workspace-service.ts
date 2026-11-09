import type { Workspace, WorkspaceFileContent, WorkspaceUpdateManifest } from '#lib/types/workspace.js';
import { downloadBlob, filenameFromPath } from '#lib/utils/browser-download.js';
import { createWorkspaceFormData } from '#lib/utils/workspace-files.js';

import BaseAPIService from './api-service';
import { uploadService } from './upload-service';

class BuildWorkspaceService extends BaseAPIService {
	async getWorkspace(environmentId: string): Promise<Workspace> {
		return this.handleResponse(this.api.get(`/environments/${environmentId}/builds/workspace`, { cache: 'no-store' }));
	}

	async getWorkspaceFile(relativePath: string, environmentId: string): Promise<WorkspaceFileContent> {
		return this.handleResponse(
			this.api.get(`/environments/${environmentId}/builds/workspace/file`, {
				cache: 'no-store',
				params: { relativePath }
			})
		);
	}

	async updateWorkspace(manifest: WorkspaceUpdateManifest, files: File[], environmentId: string): Promise<Workspace> {
		const form = createWorkspaceFormData(manifest, files);
		return this.handleResponse(this.api.patch(`/environments/${environmentId}/builds/workspace`, form));
	}

	async uploadFile(parentPath: string, file: File, environmentId: string): Promise<Workspace> {
		const uploadId = await uploadService.uploadFile(environmentId, 'build-workspace', file);
		return this.handleResponse(this.api.post(`/environments/${environmentId}/builds/workspace/upload`, { uploadId, parentPath }));
	}

	async downloadWorkspaceFile(relativePath: string, environmentId: string): Promise<void> {
		const response = await this.api.get(`/environments/${environmentId}/builds/workspace/file/download`, {
			params: { relativePath },
			responseType: 'blob'
		});
		downloadBlob(response.data, filenameFromPath(relativePath));
	}
}

export const buildWorkspaceService = new BuildWorkspaceService();
