import BaseAPIService from './api-service';
import type { ApiKey, ApiKeyCreated, CreateApiKey, UpdateApiKey } from '$lib/types/auth';
import type { Paginated, SearchPaginationSortRequest } from '$lib/types/shared';
import { transformPaginationParams } from '$lib/utils/tables';

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

	async listMine(): Promise<ApiKey[]> {
		return this.handleResponse(this.api.get('/auth/me/api-keys')) as Promise<ApiKey[]>;
	}

	async createMine(apiKey: CreateApiKey): Promise<ApiKeyCreated> {
		return this.handleResponse(this.api.post('/auth/me/api-keys', apiKey)) as Promise<ApiKeyCreated>;
	}

	async deleteMine(id: string): Promise<void> {
		return this.handleResponse(this.api.delete(`/auth/me/api-keys/${id}`)) as Promise<void>;
	}
}

export const apiKeyService = new ApiKeyAPIService();
