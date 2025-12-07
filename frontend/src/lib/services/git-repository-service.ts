import BaseAPIService from './api-service';
import type {
	GitRepositoryCreateDto,
	GitRepositoryUpdateDto,
	GitRepository,
	GitRepositoryTestResponse
} from '$lib/types/gitops.type';
import type { Paginated, SearchPaginationSortRequest } from '$lib/types/pagination.type';
import { transformPaginationParams } from '$lib/utils/params.util';

export default class GitRepositoryService extends BaseAPIService {
	async getRepositories(options?: SearchPaginationSortRequest): Promise<Paginated<GitRepository>> {
		const params = transformPaginationParams(options);
		const res = await this.api.get('/git-repositories', { params });
		return res.data;
	}

	async getRepository(id: string): Promise<GitRepository> {
		return this.handleResponse(this.api.get(`/git-repositories/${id}`));
	}

	async createRepository(repository: GitRepositoryCreateDto): Promise<GitRepository> {
		return this.handleResponse(this.api.post(`/git-repositories`, repository));
	}

	async updateRepository(id: string, repository: GitRepositoryUpdateDto): Promise<GitRepository> {
		return this.handleResponse(this.api.put(`/git-repositories/${id}`, repository));
	}

	async deleteRepository(id: string): Promise<void> {
		return this.handleResponse(this.api.delete(`/git-repositories/${id}`));
	}

	async testRepository(id: string, branch?: string): Promise<GitRepositoryTestResponse> {
		const params = branch ? { branch } : {};
		return this.handleResponse(this.api.post(`/git-repositories/${id}/test`, {}, { params }));
	}
}

export const gitRepositoryService = new GitRepositoryService();
