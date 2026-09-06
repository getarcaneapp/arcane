import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import type {
	ProjectWorkspace,
	ProjectWorkspaceFileContent,
	ProjectWorkspaceUpdateManifest
} from '#lib/types/project-workspace.js';
import { downloadBlob, filenameFromPath } from '#lib/utils/browser-download.js';
import BaseAPIService from './api-service';

class ProjectWorkspaceService extends BaseAPIService {
	private async resolveEnvironmentId(environmentId?: string): Promise<string> {
		return environmentId ?? (await environmentStore.getCurrentEnvironmentId());
	}

	async getWorkspace(projectId: string, environmentId?: string): Promise<ProjectWorkspace> {
		const envId = await this.resolveEnvironmentId(environmentId);
		return this.handleResponse(this.api.get(`/environments/${envId}/projects/${projectId}/workspace`, { cache: 'no-store' }));
	}

	async getWorkspaceFile(projectId: string, relativePath: string, environmentId?: string): Promise<ProjectWorkspaceFileContent> {
		const envId = await this.resolveEnvironmentId(environmentId);
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
		environmentId?: string
	): Promise<ProjectWorkspace> {
		const envId = await this.resolveEnvironmentId(environmentId);
		const form = new FormData();
		form.append('manifest', JSON.stringify(manifest));
		for (const file of files) form.append('files', file, file.name);
		return this.handleResponse(this.api.put(`/environments/${envId}/projects/${projectId}/workspace`, form));
	}

	async downloadWorkspaceFile(projectId: string, relativePath: string, environmentId?: string): Promise<void> {
		const envId = await this.resolveEnvironmentId(environmentId);
		const response = await this.api.get(`/environments/${envId}/projects/${projectId}/workspace/file/download`, {
			params: { relativePath },
			responseType: 'blob'
		});
		downloadBlob(response.data, filenameFromPath(relativePath));
	}
}

export const projectWorkspaceService = new ProjectWorkspaceService();
