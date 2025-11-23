import BaseAPIService from './api-service';
import type { GitOpsRepositoryCreateDto, GitOpsRepositoryUpdateDto } from '$lib/types/gitops.type';
import type { GitOpsRepository } from '$lib/types/gitops.type';
import type { Paginated, SearchPaginationSortRequest } from '$lib/types/pagination.type';
import { transformPaginationParams } from '$lib/utils/params.util';

export default class GitOpsRepositoryService extends BaseAPIService {
	async getRepositories(options?: SearchPaginationSortRequest): Promise<Paginated<GitOpsRepository>> {
		const params = transformPaginationParams(options);
		const res = await this.api.get('/gitops', { params });
		return res.data;
	}

	async getRepository(id: string): Promise<GitOpsRepository> {
		return this.handleResponse(this.api.get(`/gitops/${id}`));
	}

	async createRepository(repository: GitOpsRepositoryCreateDto): Promise<GitOpsRepository> {
		return this.handleResponse(this.api.post(`/gitops`, repository));
	}

	async updateRepository(id: string, repository: GitOpsRepositoryUpdateDto): Promise<GitOpsRepository> {
		return this.handleResponse(this.api.put(`/gitops/${id}`, repository));
	}

	async deleteRepository(id: string): Promise<void> {
		return this.handleResponse(this.api.delete(`/gitops/${id}`));
	}

	async testRepository(id: string): Promise<any> {
		return this.handleResponse(this.api.post(`/gitops/${id}/test`));
	}

	async syncRepositoryNow(id: string): Promise<any> {
		return this.handleResponse(this.api.post(`/gitops/${id}/sync-now`));
	}
}

export const gitopsRepositoryService = new GitOpsRepositoryService();
