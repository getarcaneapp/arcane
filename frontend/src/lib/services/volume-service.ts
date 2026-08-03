import BaseAPIService from './api-service';
import { environmentStore, LOCAL_DOCKER_ENVIRONMENT_ID } from '#lib/stores/environment.store.svelte';
import type {
	VolumeSummaryDto,
	VolumeDetailDto,
	VolumeUsageDto,
	VolumeUsageCounts,
	VolumeCreateRequest,
	VolumeSizeInfo
} from '#lib/types/docker';
import type { SearchPaginationSortRequest, Paginated } from '#lib/types/shared';
import { transformPaginationParams } from '#lib/utils/tables';
import type { VolumeWorkspace, VolumeWorkspaceFileContent, VolumeWorkspaceUpdateManifest } from '#lib/types/volume-workspace';
import { downloadBlob, filenameFromPath } from '#lib/utils/browser-download';

export type VolumesPaginatedResponse = Paginated<VolumeSummaryDto, VolumeUsageCounts>;

class VolumeService extends BaseAPIService {
	private async resolveEnvironmentId(environmentId?: string): Promise<string> {
		return environmentId ?? (await environmentStore.getCurrentEnvironmentId());
	}

	async getVolumes(options?: SearchPaginationSortRequest): Promise<VolumesPaginatedResponse> {
		const envId = await this.resolveEnvironmentId();
		return this.getVolumesForEnvironment(envId, options);
	}

	async getVolumesForEnvironment(
		environmentId: string,
		options?: SearchPaginationSortRequest
	): Promise<VolumesPaginatedResponse> {
		const params = transformPaginationParams(options);
		const res = await this.api.get(`/environments/${environmentId}/volumes`, { params });
		return res.data;
	}

	async getVolume(volumeName: string): Promise<VolumeDetailDto> {
		const envId = await this.resolveEnvironmentId();
		return this.getVolumeForEnvironment(envId, volumeName);
	}

	async getVolumeForEnvironment(environmentId: string, volumeName: string): Promise<VolumeDetailDto> {
		return this.handleResponse(this.api.get(`/environments/${environmentId}/volumes/${volumeName}`)) as Promise<VolumeDetailDto>;
	}

	async getWorkspace(volumeName: string): Promise<VolumeWorkspace> {
		const envId = await this.resolveEnvironmentId();
		return this.getWorkspaceForEnvironment(envId, volumeName);
	}

	async getWorkspaceForEnvironment(environmentId: string, volumeName: string): Promise<VolumeWorkspace> {
		return this.handleResponse(this.api.get(`/environments/${environmentId}/volumes/${volumeName}/files`));
	}

	async getWorkspaceFile(volumeName: string, relativePath: string): Promise<VolumeWorkspaceFileContent> {
		const envId = await this.resolveEnvironmentId();
		return this.getWorkspaceFileForEnvironment(envId, volumeName, relativePath);
	}

	async getWorkspaceFileForEnvironment(
		environmentId: string,
		volumeName: string,
		relativePath: string
	): Promise<VolumeWorkspaceFileContent> {
		return this.handleResponse(
			this.api.get(`/environments/${environmentId}/volumes/${volumeName}/file`, { params: { relativePath } })
		);
	}

	async updateWorkspace(volumeName: string, manifest: VolumeWorkspaceUpdateManifest, files: File[]): Promise<VolumeWorkspace> {
		const envId = await this.resolveEnvironmentId();
		return this.updateWorkspaceForEnvironment(envId, volumeName, manifest, files);
	}

	async updateWorkspaceForEnvironment(
		environmentId: string,
		volumeName: string,
		manifest: VolumeWorkspaceUpdateManifest,
		files: File[]
	): Promise<VolumeWorkspace> {
		const form = new FormData();
		form.append('manifest', JSON.stringify(manifest));
		for (const file of files) form.append('files', file, file.name);
		return this.handleResponse(this.api.put(`/environments/${environmentId}/volumes/${volumeName}/files`, form));
	}

	async downloadWorkspaceFile(volumeName: string, relativePath: string): Promise<void> {
		const envId = await this.resolveEnvironmentId();
		return this.downloadWorkspaceFileForEnvironment(envId, volumeName, relativePath);
	}

	async downloadWorkspaceFileForEnvironment(environmentId: string, volumeName: string, relativePath: string): Promise<void> {
		const path = `/${relativePath}`;
		const res = await this.api.get(`/environments/${environmentId}/volumes/${volumeName}/browse/download`, {
			params: { path },
			responseType: 'blob'
		});
		downloadBlob(res.data, filenameFromPath(path));
	}

	async getVolumeUsage(volumeName: string): Promise<VolumeUsageDto> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.get(`/environments/${envId}/volumes/${volumeName}/usage`)) as Promise<VolumeUsageDto>;
	}

	async getVolumeUsageCounts(): Promise<VolumeUsageCounts> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const res = await this.api.get(`/environments/${envId}/volumes/counts`);
		return res.data.data;
	}

	async getVolumeSizes(environmentId: string): Promise<VolumeSizeInfo[]> {
		const res = await this.api.get(`/environments/${environmentId}/volumes/sizes`);
		return res.data.data;
	}

	async createVolume(options: VolumeCreateRequest, environmentId?: string): Promise<any> {
		const envId = await this.resolveEnvironmentId(environmentId);
		return this.handleResponse(this.api.post(`/environments/${envId}/volumes`, options));
	}

	async deleteVolume(volumeName: string): Promise<any> {
		// Resolve the env id synchronously when possible so bulk deletes start
		// their request inside the runWithActivityBatchId scope (see api-service).
		const envId = environmentStore.isInitialized()
			? (environmentStore.selected?.id ?? LOCAL_DOCKER_ENVIRONMENT_ID)
			: await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.delete(`/environments/${envId}/volumes/${volumeName}`));
	}
}

export const volumeService = new VolumeService();
