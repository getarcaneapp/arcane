import { environmentStore, LOCAL_DOCKER_ENVIRONMENT_ID } from '#lib/stores/environment.store.svelte.js';
import type {
	Volume,
	VolumeUsageDto,
	VolumeUsageCounts,
	VolumeCreate,
	VolumeRenameRequest,
	VolumeSizeInfo
} from '#lib/types/docker.js';
import type { SearchPaginationSortRequest, Paginated } from '#lib/types/shared.js';
import { transformPaginationParams } from '#lib/utils/tables.js';

import BaseAPIService, { type APIRequestConfig } from './api-service';

export type VolumesPaginatedResponse = Paginated<Volume, VolumeUsageCounts>;

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
		options?: SearchPaginationSortRequest,
		config?: APIRequestConfig
	): Promise<VolumesPaginatedResponse> {
		const params = transformPaginationParams(options);
		const res = await this.api.get(`/environments/${environmentId}/volumes`, { ...config, params });
		return res.data;
	}

	async getVolume(volumeName: string): Promise<Volume> {
		const envId = await this.resolveEnvironmentId();
		return this.getVolumeForEnvironment(envId, volumeName);
	}

	async getVolumeForEnvironment(environmentId: string, volumeName: string): Promise<Volume> {
		return this.handleResponse(this.api.get(`/environments/${environmentId}/volumes/${volumeName}`)) as Promise<Volume>;
	}

	async getVolumeUsage(volumeName: string, environmentId?: string): Promise<VolumeUsageDto> {
		const envId = await this.resolveEnvironmentId(environmentId);
		return this.handleResponse(this.api.get(`/environments/${envId}/volumes/${volumeName}/usage`)) as Promise<VolumeUsageDto>;
	}

	async getVolumeUsageCounts(): Promise<VolumeUsageCounts> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const res = await this.api.get(`/environments/${envId}/volumes/counts`);
		return res.data.data;
	}

	async getVolumeSizes(environmentId: string, config?: APIRequestConfig): Promise<VolumeSizeInfo[]> {
		const res = await this.api.get(`/environments/${environmentId}/volumes/sizes`, config);
		return res.data.data;
	}

	async createVolume(options: VolumeCreate, environmentId?: string): Promise<Volume> {
		const envId = await this.resolveEnvironmentId(environmentId);
		return this.handleResponse(this.api.post(`/environments/${envId}/volumes`, options));
	}

	async renameVolume(sourceName: string, options: VolumeRenameRequest, environmentId?: string): Promise<Volume> {
		const envId = await this.resolveEnvironmentId(environmentId);
		return this.handleResponse(
			this.api.post(`/environments/${envId}/volumes/${encodeURIComponent(sourceName)}/rename`, options)
		) as Promise<Volume>;
	}

	async deleteVolume(volumeName: string, environmentId?: string): Promise<any> {
		// Resolve the env id synchronously when possible so bulk deletes start
		// their request inside the runWithActivityBatchId scope (see api-service).
		const envId =
			environmentId ??
			(environmentStore.isInitialized()
				? (environmentStore.selected?.id ?? LOCAL_DOCKER_ENVIRONMENT_ID)
				: await environmentStore.getCurrentEnvironmentId());
		return this.handleResponse(this.api.delete(`/environments/${envId}/volumes/${volumeName}`));
	}
}

export const volumeService = new VolumeService();
