import BaseAPIService from './api-service';
import type { ApiKey, ApiKeyCreated, CreateApiKey, UpdateApiKey } from '$lib/types/api-key.type';
import type { Paginated, SearchPaginationSortRequest } from '$lib/types/pagination.type';
import { transformPaginationParams } from '$lib/utils/params.util';

export default class ApiKeyAPIService extends BaseAPIService {
	async getApiKeys(options?: SearchPaginationSortRequest): Promise<Paginated<ApiKey>> {
		const params = transformPaginationParams(options);
		const res = await this.api.get('/api-keys', { params });
		return res.data;
	}

	async get(id: string): Promise<ApiKey> {
		return this.handleResponse(this.api.get(`/api-keys/${id}`)) as Promise<ApiKey>;
	}

	async create(apiKey: CreateApiKey): Promise<ApiKeyCreated> {
		return this.handleResponse(this.api.post('/api-keys', apiKey)) as Promise<ApiKeyCreated>;
	}

	async update(id: string, apiKey: UpdateApiKey): Promise<ApiKey> {
		return this.handleResponse(this.api.put(`/api-keys/${id}`, apiKey)) as Promise<ApiKey>;
	}

	async delete(id: string): Promise<void> {
		return this.handleResponse(this.api.delete(`/api-keys/${id}`)) as Promise<void>;
	}
}

export const apiKeyService = new ApiKeyAPIService();
