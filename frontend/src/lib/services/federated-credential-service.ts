import BaseAPIService from './api-service';
import type { CreateFederatedCredential, FederatedCredential, UpdateFederatedCredential } from '$lib/types/auth';
import type { Paginated, SearchPaginationSortRequest } from '$lib/types/shared';
import { transformPaginationParams } from '$lib/utils/tables';

class FederatedCredentialAPIService extends BaseAPIService {
	async list(options?: SearchPaginationSortRequest): Promise<Paginated<FederatedCredential>> {
		const params = transformPaginationParams(options);
		const res = await this.api.get('/federated-credentials', { params });
		return res.data;
	}

	async get(id: string): Promise<FederatedCredential> {
		return this.handleResponse(this.api.get(`/federated-credentials/${id}`)) as Promise<FederatedCredential>;
	}

	async create(credential: CreateFederatedCredential): Promise<FederatedCredential> {
		return this.handleResponse(this.api.post('/federated-credentials', credential)) as Promise<FederatedCredential>;
	}

	async update(id: string, credential: UpdateFederatedCredential): Promise<FederatedCredential> {
		return this.handleResponse(this.api.put(`/federated-credentials/${id}`, credential)) as Promise<FederatedCredential>;
	}

	async delete(id: string): Promise<void> {
		return this.handleResponse(this.api.delete(`/federated-credentials/${id}`)) as Promise<void>;
	}
}

export const federatedCredentialService = new FederatedCredentialAPIService();
