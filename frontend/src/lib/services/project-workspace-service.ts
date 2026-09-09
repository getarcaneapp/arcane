import type {
	ProjectWorkspace,
	ProjectWorkspaceFileContent,
	ProjectWorkspaceUpdateManifest
} from '#lib/types/project-workspace.js';
import { downloadBlob, filenameFromPath } from '#lib/utils/browser-download.js';
import BaseAPIService from './api-service';

class ProjectWorkspaceService extends BaseAPIService {
	async getWorkspace(projectId: string, envId: string): Promise<ProjectWorkspace> {
		return this.handleResponse(this.api.get(`/environments/${envId}/projects/${projectId}/workspace`, { cache: 'no-store' }));
	}

	async getWorkspaceFile(projectId: string, relativePath: string, envId: string): Promise<ProjectWorkspaceFileContent> {
		return this.handleResponse(
			this.api.get(`/environments/${envId}/projects/${projectId}/workspace/file`, {
				cache: 'no-store',
				params: { relativePath }
			})
		);
	}

	async updateWorkspace(
		projectId: string,
		manifest: ProjectWorkspaceUpdateManifest,
		files: File[],
		envId: string
	): Promise<ProjectWorkspace> {
		const form = new FormData();
		form.append('manifest', JSON.stringify(manifest));
		for (const file of files) form.append('files', file, file.name);
		return this.handleResponse(this.api.put(`/environments/${envId}/projects/${projectId}/workspace`, form));
	}

	async downloadWorkspaceFile(projectId: string, relativePath: string, envId: string): Promise<void> {
		const response = await this.api.get(`/environments/${envId}/projects/${projectId}/workspace/file/download`, {
			params: { relativePath },
			responseType: 'blob'
		});
		downloadBlob(response.data, filenameFromPath(relativePath));
	}
}

export const projectWorkspaceService = new ProjectWorkspaceService();
