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
	async getVolumesForEnvironment(
		environmentId: string,
		options?: SearchPaginationSortRequest,
		config?: APIRequestConfig
	): Promise<VolumesPaginatedResponse> {
		const params = transformPaginationParams(options);
		const res = await this.api.get(`/environments/${environmentId}/volumes`, { ...config, params });
		return res.data;
	}

	async getVolumeForEnvironment(environmentId: string, volumeName: string): Promise<Volume> {
		return this.handleResponse(this.api.get(`/environments/${environmentId}/volumes/${volumeName}`)) as Promise<Volume>;
	}

	async getVolumeUsage(volumeName: string, environmentId: string): Promise<VolumeUsageDto> {
		return this.handleResponse(
			this.api.get(`/environments/${environmentId}/volumes/${volumeName}/usage`)
		) as Promise<VolumeUsageDto>;
	}

	async getVolumeUsageCounts(environmentId: string, config?: APIRequestConfig): Promise<VolumeUsageCounts> {
		const res = await this.api.get(`/environments/${environmentId}/volumes/counts`, config);
		return res.data.data;
	}

	async getVolumeSizes(environmentId: string, config?: APIRequestConfig): Promise<VolumeSizeInfo[]> {
		const res = await this.api.get(`/environments/${environmentId}/volumes/sizes`, config);
		return res.data.data;
	}

	async createVolume(options: VolumeCreate, environmentId: string): Promise<Volume> {
		return this.handleResponse(this.api.post(`/environments/${environmentId}/volumes`, options));
	}

	async renameVolume(sourceName: string, options: VolumeRenameRequest, environmentId: string): Promise<Volume> {
		return this.handleResponse(
			this.api.post(`/environments/${environmentId}/volumes/${encodeURIComponent(sourceName)}/rename`, options)
		) as Promise<Volume>;
	}

	async deleteVolume(volumeName: string, environmentId: string): Promise<any> {
		return this.handleResponse(this.api.delete(`/environments/${environmentId}/volumes/${volumeName}`));
	}
}

export const volumeService = new VolumeService();
