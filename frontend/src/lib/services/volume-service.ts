import BaseAPIService from './api-service';
import { environmentStore } from '$lib/stores/environment.store.svelte';
import type {
	VolumeSummaryDto,
	VolumeDetailDto,
	VolumeUsageDto,
	VolumeUsageCounts,
	VolumeCreateRequest,
	VolumeSizeInfo
} from '$lib/types/volume.type';
import type { SearchPaginationSortRequest, Paginated } from '$lib/types/pagination.type';
import { transformPaginationParams } from '$lib/utils/params.util';

export type VolumesPaginatedResponse = Paginated<VolumeSummaryDto, VolumeUsageCounts>;

export class VolumeService extends BaseAPIService {
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

	async getVolumeUsage(volumeName: string): Promise<VolumeUsageDto> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.get(`/environments/${envId}/volumes/${volumeName}/usage`)) as Promise<VolumeUsageDto>;
	}

	async getVolumeUsageCounts(): Promise<VolumeUsageCounts> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const res = await this.api.get(`/environments/${envId}/volumes/counts`);
		return res.data.data;
	}

	async getVolumeSizes(): Promise<VolumeSizeInfo[]> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const res = await this.api.get(`/environments/${envId}/volumes/sizes`);
		return res.data.data;
	}

	async createVolume(options: VolumeCreateRequest): Promise<any> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.post(`/environments/${envId}/volumes`, options));
	}

	async deleteVolume(volumeName: string): Promise<any> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.delete(`/environments/${envId}/volumes/${volumeName}`));
	}
}

export const volumeService = new VolumeService();
