import BaseAPIService from './api-service';
import { environmentStore } from '$lib/stores/environment.store.svelte';
import type { SecretSummaryDto, SecretWithContentDto, SecretCreateRequest, SecretUpdateRequest } from '$lib/types/secret.type';
import type { SearchPaginationSortRequest, Paginated } from '$lib/types/pagination.type';
import { transformPaginationParams } from '$lib/utils/params.util';

export type SecretsPaginatedResponse = Paginated<SecretSummaryDto>;

export class SecretService extends BaseAPIService {
	async getSecrets(options?: SearchPaginationSortRequest): Promise<SecretsPaginatedResponse> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		const params = transformPaginationParams(options);
		const res = await this.api.get(`/environments/${envId}/secrets`, { params });
		return res.data;
	}

	async getSecret(secretId: string): Promise<SecretSummaryDto> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.get(`/environments/${envId}/secrets/${secretId}`)) as Promise<SecretSummaryDto>;
	}

	async getSecretContent(secretId: string): Promise<SecretWithContentDto> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(
			this.api.get(`/environments/${envId}/secrets/${secretId}/content`)
		) as Promise<SecretWithContentDto>;
	}

	async createSecret(options: SecretCreateRequest): Promise<SecretSummaryDto> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.post(`/environments/${envId}/secrets`, options)) as Promise<SecretSummaryDto>;
	}

	async updateSecret(secretId: string, options: SecretUpdateRequest): Promise<SecretSummaryDto> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		return this.handleResponse(this.api.put(`/environments/${envId}/secrets/${secretId}`, options)) as Promise<SecretSummaryDto>;
	}

	async deleteSecret(secretId: string): Promise<void> {
		const envId = await environmentStore.getCurrentEnvironmentId();
		await this.handleResponse(this.api.delete(`/environments/${envId}/secrets/${secretId}`));
	}
}

export const secretService = new SecretService();
