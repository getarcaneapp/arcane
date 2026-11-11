import type { MessageResponse } from '#lib/types/base.js';
import type { ProjectWorkspaceUpdateManifest } from '#lib/types/project.js';
import type { Workspace, WorkspaceFileContent } from '#lib/types/workspace.js';
import { downloadBlob, filenameFromPath } from '#lib/utils/browser-download.js';
import { createWorkspaceFormData } from '#lib/utils/workspace-files.js';

import BaseAPIService from './api-service';

class ProjectWorkspaceService extends BaseAPIService {
	async getWorkspace(projectId: string, path: string, environmentId: string): Promise<Workspace> {
		return this.handleResponse(
			this.api.get(`/agents/${environmentId}/projects/${projectId}/workspace`, {
				cache: 'no-store',
				params: { path }
			})
		);
	}

	async getWorkspaceFile(projectId: string, relativePath: string, environmentId: string): Promise<WorkspaceFileContent> {
		return this.handleResponse(
			this.api.get(`/agents/${environmentId}/projects/${projectId}/workspace/file`, {
				cache: 'no-store',
				params: { relativePath }
			})
		);
	}

	async updateWorkspace(
		projectId: string,
		manifest: ProjectWorkspaceUpdateManifest,
		files: File[],
		environmentId: string
	): Promise<MessageResponse> {
		const form = createWorkspaceFormData(manifest, files);
		return this.handleResponse(this.api.patch(`/agents/${environmentId}/projects/${projectId}/workspace`, form));
	}

	async downloadWorkspaceFile(projectId: string, relativePath: string, environmentId: string): Promise<void> {
		const response = await this.api.get(`/agents/${environmentId}/projects/${projectId}/workspace/file/download`, {
			params: { relativePath },
			responseType: 'blob'
		});
		downloadBlob(response.data, filenameFromPath(relativePath));
	}
}

export const projectWorkspaceService = new ProjectWorkspaceService();
