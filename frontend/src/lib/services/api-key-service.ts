import type { ApiKey, ApiKeyCreate, ApiKeyCreated, ApiKeyUpdate, PersonalApiKeyCreate } from '#lib/types/auth.js';
import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared.js';
import { transformPaginationParams } from '#lib/utils/tables.js';

import BaseAPIService from './api-service';

class ApiKeyAPIService extends BaseAPIService {
	async getApiKeys(options?: SearchPaginationSortRequest): Promise<Paginated<ApiKey>> {
		const params = transformPaginationParams(options);
		const res = await this.api.get('/apikeys', { params });
		return res.data;
	}

	async get(id: string): Promise<ApiKey> {
		return this.handleResponse(this.api.get(`/apikeys/${id}`)) as Promise<ApiKey>;
	}

	async create(apiKey: ApiKeyCreate): Promise<ApiKeyCreated> {
		return this.handleResponse(this.api.post('/apikeys', apiKey)) as Promise<ApiKeyCreated>;
	}

	async update(id: string, apiKey: ApiKeyUpdate): Promise<ApiKey> {
		return this.handleResponse(this.api.patch(`/apikeys/${id}`, apiKey)) as Promise<ApiKey>;
	}

	async delete(id: string): Promise<void> {
		return this.handleResponse(this.api.delete(`/apikeys/${id}`)) as Promise<void>;
	}

	async listMine(): Promise<ApiKey[]> {
		return this.handleResponse(this.api.get('/auth/me/apikeys')) as Promise<ApiKey[]>;
	}

	async createMine(apiKey: PersonalApiKeyCreate): Promise<ApiKeyCreated> {
		return this.handleResponse(this.api.post('/auth/me/apikeys', apiKey)) as Promise<ApiKeyCreated>;
	}

	async deleteMine(id: string): Promise<void> {
		return this.handleResponse(this.api.delete(`/auth/me/apikeys/${id}`)) as Promise<void>;
	}
}

export const apiKeyService = new ApiKeyAPIService();
