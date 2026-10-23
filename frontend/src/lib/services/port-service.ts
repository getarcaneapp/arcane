import type { PortMappingDto } from '#lib/types/docker.js';
import type { SearchPaginationSortRequest, Paginated } from '#lib/types/shared.js';
import { transformPaginationParams } from '#lib/utils/tables.js';

import BaseAPIService, { type APIRequestConfig } from './api-service';

export type PortsPaginatedResponse = Paginated<PortMappingDto>;

class PortService extends BaseAPIService {
	async getPortsForEnvironment(
		environmentId: string,
		options?: SearchPaginationSortRequest,
		config?: APIRequestConfig
	): Promise<PortsPaginatedResponse> {
		const params = transformPaginationParams(options);
		const res = await this.api.get(`/environments/${environmentId}/ports`, { ...config, params });
		return res.data;
	}
}

export const portService = new PortService();
