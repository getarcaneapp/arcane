import BaseAPIService from './api-service';
import type {
	ContainerRegistryCreateDto,
	ContainerRegistryPullUsageResponse,
	ContainerRegistryUpdateDto,
	RegistryRepository,
	RegistryTag
} from '#lib/types/docker.js';
import type { ContainerRegistry } from '#lib/types/docker.js';
import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared.js';
import { transformPaginationParams } from '#lib/utils/tables.js';

class ContainerRegistryService extends BaseAPIService {
	async getRegistries(options?: SearchPaginationSortRequest): Promise<Paginated<ContainerRegistry>> {
		const params = transformPaginationParams(options);
		const res = await this.api.get('/container-registries', { params });
		return res.data;
	}

	async getRegistry(id: string): Promise<ContainerRegistry> {
		return this.handleResponse(this.api.get(`/container-registries/${id}`));
	}

	async getPullUsage(): Promise<ContainerRegistryPullUsageResponse> {
		return this.handleResponse(this.api.get('/container-registries/pull-usage'));
	}

	async createRegistry(registry: ContainerRegistryCreateDto): Promise<ContainerRegistry> {
		return this.handleResponse(this.api.post(`/container-registries`, registry));
	}

	async updateRegistry(id: string, registry: ContainerRegistryUpdateDto): Promise<ContainerRegistry> {
		return this.handleResponse(this.api.put(`/container-registries/${id}`, registry));
	}

	async deleteRegistry(id: string): Promise<void> {
		return this.handleResponse(this.api.delete(`/container-registries/${id}`));
	}

	async testRegistry(id: string): Promise<unknown> {
		return this.handleResponse(this.api.post(`/container-registries/${id}/test`));
	}

	async getRepositories(id: string, options?: SearchPaginationSortRequest): Promise<Paginated<RegistryRepository>> {
		const params = transformPaginationParams(options);
		const res = await this.api.get(`/container-registries/${id}/repositories`, { params });
		const page: Paginated<Omit<RegistryRepository, 'id'>> = res.data;
		return { ...page, data: page.data.map((repository) => ({ ...repository, id: repository.name })) };
	}

	async getTags(id: string, repository: string, options?: SearchPaginationSortRequest): Promise<Paginated<RegistryTag>> {
		const params = { ...transformPaginationParams(options), repository };
		const res = await this.api.get(`/container-registries/${id}/tags`, { params });
		const page: Paginated<Omit<RegistryTag, 'id'>> = res.data;
		return { ...page, data: page.data.map((tag) => ({ ...tag, id: tag.name })) };
	}

	async deleteTag(id: string, repository: string, tag: string): Promise<{ digest: string }> {
		return this.handleResponse(this.api.delete(`/container-registries/${id}/tags`, { params: { repository, tag } }));
	}
}

export const containerRegistryService = new ContainerRegistryService();
