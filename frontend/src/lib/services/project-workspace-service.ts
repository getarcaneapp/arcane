import type { Workspace, WorkspaceFileContent, WorkspaceUpdateManifest } from '#lib/types/workspace.js';
import { downloadBlob, filenameFromPath } from '#lib/utils/browser-download.js';
import { createWorkspaceFormData } from '#lib/utils/workspace-files.js';

import BaseAPIService from './api-service';

class ProjectWorkspaceService extends BaseAPIService {
	async getWorkspace(projectId: string, environmentId: string): Promise<Workspace> {
		return this.handleResponse(
			this.api.get(`/environments/${environmentId}/projects/${projectId}/workspace`, { cache: 'no-store' })
		);
	}

	async getWorkspaceFile(projectId: string, relativePath: string, environmentId: string): Promise<WorkspaceFileContent> {
		return this.handleResponse(
			this.api.get(`/environments/${environmentId}/projects/${projectId}/workspace/file`, {
				cache: 'no-store',
				params: { relativePath }
			})
		);
	}

	async updateWorkspace(
		projectId: string,
		manifest: WorkspaceUpdateManifest,
		files: File[],
		environmentId: string
	): Promise<Workspace> {
		const form = createWorkspaceFormData(manifest, files);
		return this.handleResponse(this.api.patch(`/environments/${environmentId}/projects/${projectId}/workspace`, form));
	}

	async downloadWorkspaceFile(projectId: string, relativePath: string, environmentId: string): Promise<void> {
		const response = await this.api.get(`/environments/${environmentId}/projects/${projectId}/workspace/file/download`, {
			params: { relativePath },
			responseType: 'blob'
		});
		downloadBlob(response.data, filenameFromPath(relativePath));
	}
}

export const projectWorkspaceService = new ProjectWorkspaceService();
