import type { MessageResponse, Paginated } from '#lib/types/base.js';
import type {
	ContainerRegistryPullUsageResponse,
	ContainerRegistry,
	ContainerRegistryRepository,
	ContainerRegistryRepositoryTag
} from '#lib/types/containerregistry.js';
import type {
	ContainerRegistryCreate,
	ContainerRegistryUpdate,
	ContainerRegistryRepositoryRow,
	ContainerRegistryRepositoryTagRow
} from '#lib/types/ui/containerregistry.js';
import type { SearchPaginationSortRequest } from '#lib/types/ui/pagination.js';
import { transformPaginationParams } from '#lib/utils/tables.js';

import BaseAPIService from './api-service';

class ContainerRegistryService extends BaseAPIService {
	async getRegistries(options?: SearchPaginationSortRequest): Promise<Paginated<ContainerRegistry>> {
		const params = transformPaginationParams(options);
		const res = await this.api.get('/registries', { params });
		return res.data;
	}

	async getRegistry(id: string): Promise<ContainerRegistry> {
		return this.handleResponse(this.api.get(`/registries/${id}`));
	}

	async getPullUsage(): Promise<ContainerRegistryPullUsageResponse> {
		return this.handleResponse(this.api.get('/registries/usage'));
	}

	async createRegistry(registry: ContainerRegistryCreate): Promise<ContainerRegistry> {
		return this.handleResponse(this.api.post(`/registries`, registry));
	}

	async updateRegistry(id: string, registry: ContainerRegistryUpdate): Promise<ContainerRegistry> {
		return this.handleResponse(this.api.patch(`/registries/${id}`, registry));
	}

	async deleteRegistry(id: string): Promise<void> {
		return this.handleResponse(this.api.delete(`/registries/${id}`));
	}

	async testRegistry(id: string): Promise<MessageResponse> {
		return this.handleResponse(this.api.post(`/registries/${id}`, undefined, { params: { action: 'test' } }));
	}

	async getRepositories(id: string, options?: SearchPaginationSortRequest): Promise<Paginated<ContainerRegistryRepositoryRow>> {
		const params = transformPaginationParams(options);
		const res = await this.api.get(`/registries/${id}/repositories`, { params });
		const page: Paginated<ContainerRegistryRepository> = res.data;
		return { ...page, data: page.data.map((repository) => ({ ...repository, id: repository.name })) };
	}

	async getTags(
		id: string,
		repository: string,
		options?: SearchPaginationSortRequest
	): Promise<Paginated<ContainerRegistryRepositoryTagRow>> {
		const params = { ...transformPaginationParams(options), repository };
		const res = await this.api.get(`/registries/${id}/tags`, { params });
		const page: Paginated<ContainerRegistryRepositoryTag> = res.data;
		return { ...page, data: page.data.map((tag) => ({ ...tag, id: tag.name })) };
	}

	async deleteTag(id: string, repository: string, tag: string): Promise<{ digest: string }> {
		return this.handleResponse(this.api.delete(`/registries/${id}/tags`, { params: { repository, tag } }));
	}
}

export const containerRegistryService = new ContainerRegistryService();
