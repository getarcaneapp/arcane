import type {
	GitRepositoryCreate,
	GitRepositoryUpdate,
	GitRepository,
	GitRepositoryTestResponse,
	BranchesResponse,
	BrowseResponse
} from '#lib/types/automation.js';
import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared.js';
import { transformPaginationParams } from '#lib/utils/tables.js';

import BaseAPIService from './api-service';

class GitRepositoryService extends BaseAPIService {
	async getRepositories(options?: SearchPaginationSortRequest): Promise<Paginated<GitRepository>> {
		const params = transformPaginationParams(options);
		const res = await this.api.get('/customize/git/repositories', { params });
		return res.data;
	}

	async getRepository(id: string): Promise<GitRepository> {
		return this.handleResponse(this.api.get(`/customize/git/repositories/${id}`));
	}

	async createRepository(repository: GitRepositoryCreate): Promise<GitRepository> {
		return this.handleResponse(this.api.post(`/customize/git/repositories`, repository));
	}

	async updateRepository(id: string, repository: GitRepositoryUpdate): Promise<GitRepository> {
		return this.handleResponse(this.api.patch(`/customize/git/repositories/${id}`, repository));
	}

	async deleteRepository(id: string): Promise<void> {
		return this.handleResponse(this.api.delete(`/customize/git/repositories/${id}`));
	}

	async testRepository(id: string, branch?: string): Promise<GitRepositoryTestResponse> {
		const params = branch ? { action: 'test', branch } : { action: 'test' };
		return this.handleResponse(this.api.post(`/customize/git/repositories/${id}`, {}, { params }));
	}

	async getBranches(id: string): Promise<BranchesResponse> {
		return this.handleResponse(this.api.get(`/customize/git/repositories/${id}/branches`));
	}

	async browseFiles(id: string, branch: string, path?: string): Promise<BrowseResponse> {
		const params = { branch, ...(path && { path }) };
		return this.handleResponse(this.api.get(`/customize/git/repositories/${id}/files`, { params }));
	}
}

export const gitRepositoryService = new GitRepositoryService();
