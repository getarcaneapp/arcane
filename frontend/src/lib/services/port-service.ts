import BaseAPIService from './api-service';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import type { SearchPaginationSortRequest, Paginated } from '#lib/types/shared.js';
import type { PortMappingDto } from '#lib/types/docker.js';
import { transformPaginationParams } from '#lib/utils/tables.js';

export type PortsPaginatedResponse = Paginated<PortMappingDto>;

class PortService extends BaseAPIService {
	private async resolveEnvironmentId(environmentId?: string): Promise<string> {
		return environmentId ?? (await environmentStore.getCurrentEnvironmentId());
	}

	async getPorts(options?: SearchPaginationSortRequest): Promise<PortsPaginatedResponse> {
		const envId = await this.resolveEnvironmentId();
		return this.getPortsForEnvironment(envId, options);
	}

	async getPortsForEnvironment(environmentId: string, options?: SearchPaginationSortRequest): Promise<PortsPaginatedResponse> {
		const params = transformPaginationParams(options);
		const res = await this.api.get(`/environments/${environmentId}/ports`, { params });
		return res.data;
	}
}

export const portService = new PortService();
