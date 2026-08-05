import { environmentStore } from '#lib/stores/environment.store.svelte';
import type { VolumeWorkspace, VolumeWorkspaceFileContent, VolumeWorkspaceUpdateManifest } from '#lib/types/volume-workspace';
import { downloadBlob, filenameFromPath } from '#lib/utils/browser-download';
import BaseAPIService from './api-service';

class VolumeWorkspaceService extends BaseAPIService {
	private async resolveEnvironmentId(environmentId?: string): Promise<string> {
		return environmentId ?? (await environmentStore.getCurrentEnvironmentId());
	}

	async getWorkspace(volumeName: string, environmentId?: string): Promise<VolumeWorkspace> {
		const envId = await this.resolveEnvironmentId(environmentId);
		return this.handleResponse(this.api.get(`/environments/${envId}/volumes/${volumeName}/workspace`));
	}

	async getWorkspaceFile(volumeName: string, relativePath: string, environmentId?: string): Promise<VolumeWorkspaceFileContent> {
		const envId = await this.resolveEnvironmentId(environmentId);
		return this.handleResponse(
			this.api.get(`/environments/${envId}/volumes/${volumeName}/workspace/file`, { params: { relativePath } })
		);
	}

	async updateWorkspace(
		volumeName: string,
		manifest: VolumeWorkspaceUpdateManifest,
		files: File[],
		environmentId?: string
	): Promise<VolumeWorkspace> {
		const envId = await this.resolveEnvironmentId(environmentId);
		const form = new FormData();
		form.append('manifest', JSON.stringify(manifest));
		for (const file of files) form.append('files', file, file.name);
		return this.handleResponse(this.api.put(`/environments/${envId}/volumes/${volumeName}/workspace`, form));
	}

	async downloadWorkspaceFile(volumeName: string, relativePath: string, environmentId?: string): Promise<void> {
		const envId = await this.resolveEnvironmentId(environmentId);
		const response = await this.api.get(`/environments/${envId}/volumes/${volumeName}/workspace/file/download`, {
			params: { relativePath },
			responseType: 'blob'
		});
		downloadBlob(response.data, filenameFromPath(relativePath));
	}
}

export const volumeWorkspaceService = new VolumeWorkspaceService();
