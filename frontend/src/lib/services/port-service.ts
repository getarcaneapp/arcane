import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
import type { PortsPaginatedResponse } from '#lib/types/ui/port.js';
import { transformPaginationParams } from '#lib/utils/tables.js';

import BaseAPIService, { type APIRequestConfig } from './api-service';

class PortService extends BaseAPIService {
	async getPortsForEnvironment(
		environmentId: string,
		options?: SearchPaginationSortRequest,
		config?: APIRequestConfig
	): Promise<PortsPaginatedResponse> {
		const params = transformPaginationParams(options);
		const res = await this.api.get(`/agents/${environmentId}/ports`, { ...config, params });
		return res.data;
	}
}

export const portService = new PortService();
