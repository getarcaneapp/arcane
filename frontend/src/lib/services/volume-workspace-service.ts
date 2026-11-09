import type { MessageResponse } from '#lib/types/base.js';
import type { VolumeWorkspaceUpdateManifest } from '#lib/types/volume.js';
import type { Workspace, WorkspaceFileContent } from '#lib/types/workspace.js';
import { downloadBlob, filenameFromPath } from '#lib/utils/browser-download.js';
import { createWorkspaceFormData } from '#lib/utils/workspace-files.js';

import BaseAPIService from './api-service';

class VolumeWorkspaceService extends BaseAPIService {
	async getWorkspace(volumeName: string, path: string, environmentId: string): Promise<Workspace> {
		return this.handleResponse(
			this.api.get(`/environments/${environmentId}/volumes/${volumeName}/workspace`, {
				cache: 'no-store',
				params: { path }
			})
		);
	}

	async getWorkspaceFile(volumeName: string, relativePath: string, environmentId: string): Promise<WorkspaceFileContent> {
		return this.handleResponse(
			this.api.get(`/environments/${environmentId}/volumes/${volumeName}/workspace/file`, {
				cache: 'no-store',
				params: { relativePath }
			})
		);
	}

	async updateWorkspace(
		volumeName: string,
		manifest: VolumeWorkspaceUpdateManifest,
		files: File[],
		environmentId: string
	): Promise<MessageResponse> {
		const form = createWorkspaceFormData(manifest, files);
		return this.handleResponse(this.api.patch(`/environments/${environmentId}/volumes/${volumeName}/workspace`, form));
	}

	async downloadWorkspaceFile(volumeName: string, relativePath: string, environmentId: string): Promise<void> {
		const response = await this.api.get(`/environments/${environmentId}/volumes/${volumeName}/workspace/file/download`, {
			params: { relativePath },
			responseType: 'blob'
		});
		downloadBlob(response.data, filenameFromPath(relativePath));
	}
}

export const volumeWorkspaceService = new VolumeWorkspaceService();
