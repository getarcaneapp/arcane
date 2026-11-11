import type { MessageResponse } from '#lib/types/base.js';
import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
import type { VolumeCreate, VolumesPaginatedResponse } from '#lib/types/ui/volume.js';
import type { Volume, VolumeUsageResponse, VolumeUsageCounts, VolumeRename, VolumeSizeInfo } from '#lib/types/volume.js';
import { transformPaginationParams } from '#lib/utils/tables.js';

import BaseAPIService, { type APIRequestConfig } from './api-service';

class VolumeService extends BaseAPIService {
	async getVolumesForEnvironment(
		environmentId: string,
		options?: SearchPaginationSortRequest,
		config?: APIRequestConfig
	): Promise<VolumesPaginatedResponse> {
		const params = transformPaginationParams(options);
		const res = await this.api.get(`/agents/${environmentId}/volumes`, { ...config, params });
		return res.data;
	}

	async getVolumeForEnvironment(environmentId: string, volumeName: string): Promise<Volume> {
		return this.handleResponse(this.api.get(`/agents/${environmentId}/volumes/${volumeName}`)) as Promise<Volume>;
	}

	async getVolumeUsage(volumeName: string, environmentId: string): Promise<VolumeUsageResponse> {
		return this.handleResponse(
			this.api.get(`/agents/${environmentId}/volumes/${volumeName}/usage`)
		) as Promise<VolumeUsageResponse>;
	}

	async getVolumeUsageCounts(environmentId: string, config?: APIRequestConfig): Promise<VolumeUsageCounts> {
		const res = await this.api.get(`/agents/${environmentId}/volumes/counts`, config);
		return res.data.data;
	}

	async getVolumeSizes(environmentId: string, config?: APIRequestConfig): Promise<VolumeSizeInfo[]> {
		const res = await this.api.get(`/agents/${environmentId}/volumes/sizes`, config);
		return res.data.data;
	}

	async createVolume(options: VolumeCreate, environmentId: string): Promise<Volume> {
		return this.handleResponse(this.api.post(`/agents/${environmentId}/volumes`, options));
	}

	async renameVolume(sourceName: string, options: VolumeRename, environmentId: string): Promise<Volume> {
		return this.handleResponse(
			this.api.post(`/agents/${environmentId}/volumes/${encodeURIComponent(sourceName)}/rename`, options)
		) as Promise<Volume>;
	}

	async deleteVolume(volumeName: string, environmentId: string): Promise<MessageResponse> {
		return this.handleResponse(this.api.delete(`/agents/${environmentId}/volumes/${volumeName}`));
	}
}

export const volumeService = new VolumeService();
